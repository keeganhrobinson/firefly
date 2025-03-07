// Copyright © 2022 Kaleido, Inc.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package events

import (
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"fmt"
	"strings"
	"time"

	"github.com/hyperledger/firefly/internal/config"
	"github.com/hyperledger/firefly/internal/data"
	"github.com/hyperledger/firefly/internal/definitions"
	"github.com/hyperledger/firefly/internal/identity"
	"github.com/hyperledger/firefly/internal/log"
	"github.com/hyperledger/firefly/internal/metrics"
	"github.com/hyperledger/firefly/internal/retry"
	"github.com/hyperledger/firefly/pkg/blockchain"
	"github.com/hyperledger/firefly/pkg/database"
	"github.com/hyperledger/firefly/pkg/fftypes"
	"github.com/karlseguin/ccache"
)

const (
	aggregatorOffsetName = "ff_aggregator"
)

type aggregator struct {
	ctx           context.Context
	database      database.Plugin
	definitions   definitions.DefinitionHandlers
	identity      identity.Manager
	data          data.Manager
	eventPoller   *eventPoller
	verifierType  fftypes.VerifierType
	rewindBatches chan fftypes.UUID
	queuedRewinds chan fftypes.UUID
	retry         *retry.Retry
	metrics       metrics.Manager
	batchCache    *ccache.Cache
	batchCacheTTL time.Duration
}

type batchCacheEntry struct {
	batch    *fftypes.BatchPersisted
	manifest *fftypes.BatchManifest
}

func newAggregator(ctx context.Context, di database.Plugin, bi blockchain.Plugin, sh definitions.DefinitionHandlers, im identity.Manager, dm data.Manager, en *eventNotifier, mm metrics.Manager) *aggregator {
	batchSize := config.GetInt(config.EventAggregatorBatchSize)
	ag := &aggregator{
		ctx:           log.WithLogField(ctx, "role", "aggregator"),
		database:      di,
		definitions:   sh,
		identity:      im,
		data:          dm,
		verifierType:  bi.VerifierType(),
		rewindBatches: make(chan fftypes.UUID, 1), // hops to queuedRewinds with a shouldertab on the event poller
		queuedRewinds: make(chan fftypes.UUID, batchSize),
		metrics:       mm,
		batchCacheTTL: config.GetDuration(config.BatchCacheTTL),
	}
	ag.batchCache = ccache.New(
		// We use a LRU cache with a size-aware max
		ccache.Configure().
			MaxSize(config.GetByteSize(config.BatchCacheSize)),
	)
	firstEvent := fftypes.SubOptsFirstEvent(config.GetString(config.EventAggregatorFirstEvent))
	ag.eventPoller = newEventPoller(ctx, di, en, &eventPollerConf{
		eventBatchSize:             batchSize,
		eventBatchTimeout:          config.GetDuration(config.EventAggregatorBatchTimeout),
		eventPollTimeout:           config.GetDuration(config.EventAggregatorPollTimeout),
		startupOffsetRetryAttempts: config.GetInt(config.OrchestratorStartupAttempts),
		retry: retry.Retry{
			InitialDelay: config.GetDuration(config.EventAggregatorRetryInitDelay),
			MaximumDelay: config.GetDuration(config.EventAggregatorRetryMaxDelay),
			Factor:       config.GetFloat64(config.EventAggregatorRetryFactor),
		},
		firstEvent:       &firstEvent,
		namespace:        fftypes.SystemNamespace,
		offsetType:       fftypes.OffsetTypeAggregator,
		offsetName:       aggregatorOffsetName,
		newEventsHandler: ag.processPinsEventsHandler,
		getItems:         ag.getPins,
		queryFactory:     database.PinQueryFactory,
		addCriteria: func(af database.AndFilter) database.AndFilter {
			return af.Condition(af.Builder().Eq("dispatched", false))
		},
		maybeRewind: ag.rewindOffchainBatches,
	})
	ag.retry = &ag.eventPoller.conf.retry
	return ag
}

func (ag *aggregator) start() {
	go ag.batchRewindListener()
	ag.eventPoller.start()
}

func (ag *aggregator) batchRewindListener() {
	for {
		select {
		case uuid := <-ag.rewindBatches:
			ag.queuedRewinds <- uuid
			ag.eventPoller.shoulderTap()
		case <-ag.ctx.Done():
			return
		}
	}
}

func (ag *aggregator) rewindOffchainBatches() (rewind bool, offset int64) {
	l := log.L(ag.ctx)
	var batchIDs []driver.Value
	draining := true
	for draining {
		select {
		case batchID := <-ag.queuedRewinds:
			batchIDs = append(batchIDs, batchID)
			l.Debugf("Rewinding for batch %s", &batchID)
		default:
			draining = false
		}
	}
	// Retry idefinitely for database errors (until the context closes)
	_ = ag.retry.Do(ag.ctx, "check for off-chain batch deliveries", func(attempt int) (retry bool, err error) {
		if len(batchIDs) > 0 {
			fb := database.PinQueryFactory.NewFilter(ag.ctx)
			filter := fb.And(
				fb.In("batch", batchIDs),
				fb.Eq("dispatched", false),
			).Sort("sequence").Limit(1) // only need the one oldest sequence
			sequences, _, err := ag.database.GetPins(ag.ctx, filter)
			if err != nil {
				return true, err
			}
			if len(sequences) > 0 {
				rewind = true
				offset = sequences[0].Sequence - 1 // offset is set to the last event we saw (so one behind the next pin we want)
			}
		}
		return false, nil
	})
	return rewind, offset
}

func (ag *aggregator) processWithBatchState(callback func(ctx context.Context, state *batchState) error) error {
	state := newBatchState(ag)

	err := ag.database.RunAsGroup(ag.ctx, func(ctx context.Context) (err error) {
		if err := callback(ctx, state); err != nil {
			return err
		}
		if len(state.PreFinalize) == 0 {
			return state.RunFinalize(ctx)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if len(state.PreFinalize) == 0 {
		return err
	}

	if err := state.RunPreFinalize(ag.ctx); err != nil {
		return err
	}
	return ag.database.RunAsGroup(ag.ctx, func(ctx context.Context) error {
		return state.RunFinalize(ctx)
	})
}

func (ag *aggregator) processPinsEventsHandler(items []fftypes.LocallySequenced) (repoll bool, err error) {
	pins := make([]*fftypes.Pin, len(items))
	for i, item := range items {
		pins[i] = item.(*fftypes.Pin)
	}

	return false, ag.processWithBatchState(func(ctx context.Context, state *batchState) error {
		return ag.processPins(ctx, pins, state)
	})
}

func (ag *aggregator) getPins(ctx context.Context, filter database.Filter, offset int64) ([]fftypes.LocallySequenced, error) {
	log.L(ctx).Tracef("Reading page of pins > %d (first pin would be %d)", offset, offset+1)
	pins, _, err := ag.database.GetPins(ctx, filter)
	ls := make([]fftypes.LocallySequenced, len(pins))
	for i, p := range pins {
		ls[i] = p
	}
	return ls, err
}

func (ag *aggregator) extractBatchMessagePin(manifest *fftypes.BatchManifest, requiredIndex int64) (totalBatchPins int64, msgEntry *fftypes.MessageManifestEntry, msgBaseIndex int64) {
	for _, batchMsg := range manifest.Messages {
		batchMsgBaseIdx := totalBatchPins
		for i := 0; i < batchMsg.Topics; i++ {
			if totalBatchPins == requiredIndex {
				msgEntry = batchMsg
				msgBaseIndex = batchMsgBaseIdx
			}
			totalBatchPins++
		}
	}
	return totalBatchPins, msgEntry, msgBaseIndex
}

func (ag *aggregator) migrateManifest(ctx context.Context, persistedBatch *fftypes.BatchPersisted) *fftypes.BatchManifest {
	// In version v0.13.x and earlier, we stored the full batch
	var fullPayload fftypes.BatchPayload
	err := persistedBatch.Manifest.Unmarshal(ctx, &fullPayload)
	if err != nil {
		log.L(ctx).Errorf("Invalid migration persisted batch: %s", err)
		return nil
	}
	if len(fullPayload.Messages) == 0 {
		log.L(ctx).Errorf("Invalid migration persisted batch: no payload")
		return nil
	}

	return persistedBatch.GenManifest(fullPayload.Messages, fullPayload.Data)
}

func (ag *aggregator) extractManifest(ctx context.Context, batch *fftypes.BatchPersisted) *fftypes.BatchManifest {

	var manifest fftypes.BatchManifest
	err := batch.Manifest.Unmarshal(ctx, &manifest)
	if err != nil {
		log.L(ctx).Errorf("Invalid manifest: %s", err)
		return nil
	}
	switch manifest.Version {
	case fftypes.ManifestVersionUnset:
		return ag.migrateManifest(ctx, batch)
	case fftypes.ManifestVersion1:
		return &manifest
	default:
		log.L(ctx).Errorf("Invalid manifest version: %d", manifest.Version)
		return nil
	}
}

func (ag *aggregator) getBatchCacheKey(id *fftypes.UUID, hash *fftypes.Bytes32) string {
	return fmt.Sprintf("%s/%s", id, hash)
}

func (ag *aggregator) GetBatchForPin(ctx context.Context, pin *fftypes.Pin) (*fftypes.BatchPersisted, *fftypes.BatchManifest, error) {
	cacheKey := ag.getBatchCacheKey(pin.Batch, pin.BatchHash)
	cached := ag.batchCache.Get(cacheKey)
	if cached != nil {
		cached.Extend(ag.batchCacheTTL)
		bce := cached.Value().(*batchCacheEntry)
		log.L(ag.ctx).Debugf("Batch cache hit %s", cacheKey)
		return bce.batch, bce.manifest, nil
	}
	batch, err := ag.database.GetBatchByID(ctx, pin.Batch)
	if err != nil {
		return nil, nil, err
	}
	if batch == nil {
		return nil, nil, nil
	}
	if !batch.Hash.Equals(pin.BatchHash) {
		log.L(ctx).Errorf("Batch %s hash does not match the pin. OffChain=%s OnChain=%s", pin.Batch, batch.Hash, pin.Hash)
		return nil, nil, nil
	}
	manifest := ag.extractManifest(ctx, batch)
	if manifest == nil {
		log.L(ctx).Errorf("Batch %s manifest could not be extracted - pin %s is parked", pin.Batch, pin.Hash)
		return nil, nil, nil
	}
	ag.cacheBatch(cacheKey, batch, manifest)
	return batch, manifest, nil
}

func (ag *aggregator) cacheBatch(cacheKey string, batch *fftypes.BatchPersisted, manifest *fftypes.BatchManifest) {
	bce := &batchCacheEntry{
		batch:    batch,
		manifest: manifest,
	}
	ag.batchCache.Set(cacheKey, bce, ag.batchCacheTTL)
	log.L(ag.ctx).Debugf("Cached batch %s", cacheKey)
}

func (ag *aggregator) processPins(ctx context.Context, pins []*fftypes.Pin, state *batchState) (err error) {
	l := log.L(ctx)

	// Keep a batch cache for this list of pins
	var batch *fftypes.BatchPersisted
	var manifest *fftypes.BatchManifest
	// As messages can have multiple topics, we need to avoid processing the message twice in the same poll loop.
	// We must check all the contexts in the message, and mark them dispatched together.
	dupMsgCheck := make(map[fftypes.UUID]bool)
	for _, pin := range pins {

		if batch == nil || !batch.ID.Equals(pin.Batch) {
			batch, manifest, err = ag.GetBatchForPin(ctx, pin)
			if err != nil {
				return err
			}
			if batch == nil {
				l.Debugf("Pin %.10d batch unavailable: batch=%s pinIndex=%d hash=%s masked=%t", pin.Sequence, pin.Batch, pin.Index, pin.Hash, pin.Masked)
				continue
			}
		}

		// Extract the message from the batch - where the index is of a topic within a message
		batchPinCount, msgEntry, msgBaseIndex := ag.extractBatchMessagePin(manifest, pin.Index)
		if msgEntry == nil {
			l.Errorf("Pin %.10d outside of range: batch=%s pinCount=%d pinIndex=%d hash=%s masked=%t", pin.Sequence, pin.Batch, batchPinCount, pin.Index, pin.Hash, pin.Masked)
			continue
		}

		l.Debugf("Aggregating pin %.10d batch=%s msg=%s pinIndex=%d msgBaseIndex=%d hash=%s masked=%t", pin.Sequence, pin.Batch, msgEntry.ID, pin.Index, msgBaseIndex, pin.Hash, pin.Masked)
		if dupMsgCheck[*msgEntry.ID] {
			continue
		}
		dupMsgCheck[*msgEntry.ID] = true

		// Attempt to process the message (only returns errors for database persistence issues)
		err := ag.processMessage(ctx, manifest, pin, msgBaseIndex, msgEntry, state)
		if err != nil {
			return err
		}
	}

	ag.eventPoller.commitOffset(pins[len(pins)-1].Sequence)
	return nil
}

func (ag *aggregator) checkOnchainConsistency(ctx context.Context, msg *fftypes.Message, pin *fftypes.Pin) (valid bool, err error) {
	l := log.L(ctx)

	verifierRef := &fftypes.VerifierRef{
		Type:  ag.verifierType,
		Value: pin.Signer,
	}

	if msg.Header.Key == "" || msg.Header.Key != pin.Signer {
		l.Errorf("Invalid message '%s'. Key '%s' does not match the signer of the pin: %s", msg.Header.ID, msg.Header.Key, pin.Signer)
		return false, nil // This is not retryable. skip this message
	}

	// Verify that we can resolve the signing key back to the identity that is claimed in the batch.
	resolvedAuthor, err := ag.identity.FindIdentityForVerifier(ctx, []fftypes.IdentityType{
		fftypes.IdentityTypeOrg,
		fftypes.IdentityTypeCustom,
	}, msg.Header.Namespace, verifierRef)
	if err != nil {
		return false, err
	}
	if resolvedAuthor == nil {
		if msg.Header.Type == fftypes.MessageTypeDefinition &&
			(msg.Header.Tag == fftypes.SystemTagIdentityClaim || msg.Header.Tag == fftypes.DeprecatedSystemTagDefineNode || msg.Header.Tag == fftypes.DeprecatedSystemTagDefineOrganization) {
			// We defer detailed checking of this identity to the system handler
			return true, nil
		} else if msg.Header.Type != fftypes.MessageTypePrivate {
			// Only private messages, or root org broadcasts can have an unregistered key
			l.Errorf("Invalid message '%s'. Author '%s' cound not be resolved: %s", msg.Header.ID, msg.Header.Author, err)
			return false, nil // This is not retryable. skip this batch
		}
	} else if msg.Header.Author == "" || resolvedAuthor.DID != msg.Header.Author {
		l.Errorf("Invalid message '%s'. Author '%s' does not match identity registered to %s: %s (%s)", msg.Header.ID, msg.Header.Author, verifierRef.Value, resolvedAuthor.DID, resolvedAuthor.ID)
		return false, nil // This is not retryable. skip this batch

	}

	return true, nil
}

func (ag *aggregator) processMessage(ctx context.Context, manifest *fftypes.BatchManifest, pin *fftypes.Pin, msgBaseIndex int64, msgEntry *fftypes.MessageManifestEntry, state *batchState) (err error) {
	l := log.L(ctx)

	var cro data.CacheReadOption
	if pin.Masked {
		cro = data.CRORequirePins
	} else {
		cro = data.CRORequirePublicBlobRefs
	}
	msg, data, dataAvailable, err := ag.data.GetMessageWithDataCached(ctx, msgEntry.ID, cro)
	if err != nil {
		return err
	}
	if !dataAvailable {
		l.Errorf("Message '%s' in batch '%s' is missing data", msgEntry.ID, manifest.ID)
		return nil
	}

	// Check if it's ready to be processed
	unmaskedContexts := make([]*fftypes.Bytes32, 0, len(msg.Header.Topics))
	nextPins := make([]*nextPinState, 0, len(msg.Header.Topics))
	if pin.Masked {
		// Private messages have one or more masked "pin" hashes that allow us to work
		// out if it's the next message in the sequence, given the previous messages
		if msg.Header.Group == nil || len(msg.Pins) == 0 || len(msg.Header.Topics) != len(msg.Pins) {
			l.Errorf("Message '%s' in batch '%s' has invalid pin data pins=%v topics=%v", msg.Header.ID, manifest.ID, msg.Pins, msg.Header.Topics)
			return nil
		}
		for i, pinStr := range msg.Pins {
			var msgContext fftypes.Bytes32
			pinSplit := strings.Split(pinStr, ":")
			nonceStr := ""
			if len(pinSplit) > 1 {
				// We introduced a "HASH:NONCE" syntax into the pin strings, to aid debug, but the inclusion of the
				// nonce after the hash is not necessary.
				nonceStr = pinSplit[1]
			}
			err := msgContext.UnmarshalText([]byte(pinSplit[0]))
			if err != nil {
				l.Errorf("Message '%s' in batch '%s' has invalid pin at index %d: '%s'", msg.Header.ID, manifest.ID, i, pinStr)
				return nil
			}
			nextPin, err := state.CheckMaskedContextReady(ctx, msg, msg.Header.Topics[i], pin.Sequence, &msgContext, nonceStr)
			if err != nil || nextPin == nil {
				return err
			}
			nextPins = append(nextPins, nextPin)
		}
	} else {
		for i, topic := range msg.Header.Topics {
			h := sha256.New()
			h.Write([]byte(topic))
			msgContext := fftypes.HashResult(h)
			unmaskedContexts = append(unmaskedContexts, msgContext)
			ready, err := state.CheckUnmaskedContextReady(ctx, msgContext, msg, msg.Header.Topics[i], pin.Sequence)
			if err != nil || !ready {
				return err
			}
		}

	}

	dispatched := false
	var newState fftypes.MessageState
	if dataAvailable {
		l.Debugf("Attempt dispatch msg=%s broadcastContexts=%v privatePins=%v", msg.Header.ID, unmaskedContexts, msg.Pins)
		newState, dispatched, err = ag.attemptMessageDispatch(ctx, msg, data, manifest.TX.ID, state, pin)
		if err != nil {
			return err
		}
	}

	// Mark all message pins dispatched true/false
	// - dispatched=true: we need to write them dispatched in the DB at the end of the batch, and increment all nextPins
	// - dispatched=false: we need to prevent dispatch of any subsequent messages on the same topic in the batch
	if dispatched {
		for _, np := range nextPins {
			np.IncrementNextPin(ctx)
		}
		state.MarkMessageDispatched(ctx, manifest.ID, msg, msgBaseIndex, newState)
	} else {
		for _, unmaskedContext := range unmaskedContexts {
			state.SetContextBlockedBy(ctx, *unmaskedContext, pin.Sequence)
		}
	}

	return nil
}

func (ag *aggregator) attemptMessageDispatch(ctx context.Context, msg *fftypes.Message, data fftypes.DataArray, tx *fftypes.UUID, state *batchState, pin *fftypes.Pin) (newState fftypes.MessageState, valid bool, err error) {

	// Check the pin signer is valid for the message
	if valid, err := ag.checkOnchainConsistency(ctx, msg, pin); err != nil || !valid {
		return "", false, err
	}

	// Verify we have all the blobs for the data
	if resolved, err := ag.resolveBlobs(ctx, data); err != nil || !resolved {
		return "", false, err
	}

	// For transfers, verify the transfer has come through
	if msg.Header.Type == fftypes.MessageTypeTransferBroadcast || msg.Header.Type == fftypes.MessageTypeTransferPrivate {
		fb := database.TokenTransferQueryFactory.NewFilter(ctx)
		filter := fb.And(
			fb.Eq("message", msg.Header.ID),
		)
		if transfers, _, err := ag.database.GetTokenTransfers(ctx, filter); err != nil || len(transfers) == 0 {
			log.L(ctx).Debugf("Transfer for message %s not yet available", msg.Header.ID)
			return "", false, err
		} else if !msg.Hash.Equals(transfers[0].MessageHash) {
			log.L(ctx).Errorf("Message hash %s does not match hash recorded in transfer: %s", msg.Hash, transfers[0].MessageHash)
			return "", false, nil
		}
	}

	// Validate the message data
	valid = true
	var customCorrelator *fftypes.UUID
	switch {
	case msg.Header.Type == fftypes.MessageTypeDefinition:
		// We handle definition events in-line on the aggregator, as it would be confusing for apps to be
		// dispatched subsequent events before we have processed the definition events they depend on.
		handlerResult, err := ag.definitions.HandleDefinitionBroadcast(ctx, state, msg, data, tx)
		log.L(ctx).Infof("Result of definition broadcast '%s' [%s]: %s", msg.Header.Tag, msg.Header.ID, handlerResult.Action)
		if handlerResult.Action == definitions.ActionRetry {
			return "", false, err
		}
		if handlerResult.Action == definitions.ActionWait {
			return "", false, nil
		}
		customCorrelator = handlerResult.CustomCorrelator
		valid = handlerResult.Action == definitions.ActionConfirm

	case msg.Header.Type == fftypes.MessageTypeGroupInit:
		// Already handled as part of resolving the context - do nothing.

	case len(msg.Data) > 0:
		valid, err = ag.data.ValidateAll(ctx, data)
		if err != nil {
			return "", false, err
		}
	}

	newState = fftypes.MessageStateConfirmed
	eventType := fftypes.EventTypeMessageConfirmed
	if valid {
		state.pendingConfirms[*msg.Header.ID] = msg
	} else {
		newState = fftypes.MessageStateRejected
		eventType = fftypes.EventTypeMessageRejected
	}

	state.AddFinalize(func(ctx context.Context) error {
		// Generate the appropriate event - one per topic (events cover a single topic)
		for _, topic := range msg.Header.Topics {
			event := fftypes.NewEvent(eventType, msg.Header.Namespace, msg.Header.ID, tx, topic)
			event.Correlator = msg.Header.CID
			if customCorrelator != nil {
				// Definition handlers can set a custom event correlator (such as a token pool ID)
				event.Correlator = customCorrelator
			}
			if err = ag.database.InsertEvent(ctx, event); err != nil {
				return err
			}
		}
		return nil
	})
	if ag.metrics.IsMetricsEnabled() {
		ag.metrics.MessageConfirmed(msg, eventType)
	}

	return newState, true, nil
}

// resolveBlobs ensures that the blobs for all the attachments in the data array, have been received into the
// local data exchange blob store. Either because of a private transfer, or by downloading them from the shared storage
func (ag *aggregator) resolveBlobs(ctx context.Context, data fftypes.DataArray) (resolved bool, err error) {
	l := log.L(ctx)

	for _, d := range data {
		if d.Blob == nil || d.Blob.Hash == nil {
			continue
		}

		// See if we already have the data
		blob, err := ag.database.GetBlobMatchingHash(ctx, d.Blob.Hash)
		if err != nil {
			return false, err
		}
		if blob != nil {
			l.Debugf("Blob '%s' found in local DX with ref '%s'", blob.Hash, blob.PayloadRef)
			continue
		}

		// If we've reached here, the data isn't available yet.
		// This isn't an error, we just need to wait for it to arrive.
		l.Debugf("Blob '%s' not available for data %s", d.Blob.Hash, d.ID)
		return false, nil

	}

	return true, nil

}

func (ag *aggregator) rewindForBlobArrival(ctx context.Context, blobHash *fftypes.Bytes32) error {

	batchIDs := make(map[fftypes.UUID]bool)

	// We need to work out what pins potentially are unblocked by the arrival of this data

	// Find any data associated with this blob
	var data []*fftypes.DataRef
	filter := database.DataQueryFactory.NewFilter(ctx).Eq("blob.hash", blobHash)
	data, _, err := ag.database.GetDataRefs(ctx, filter)
	if err != nil {
		return err
	}

	// Find the messages assocated with that data
	var messages []*fftypes.Message
	for _, data := range data {
		fb := database.MessageQueryFactory.NewFilter(ctx)
		filter := fb.And(fb.Eq("confirmed", nil))
		messages, _, err = ag.database.GetMessagesForData(ctx, data.ID, filter)
		if err != nil {
			return err
		}
	}

	// Find the unique batch IDs for all the messages
	for _, msg := range messages {
		if msg.BatchID != nil {
			batchIDs[*msg.BatchID] = true
		}
	}

	// Initiate rewinds for all the batchIDs that are potentially completed by the arrival of this data
	for bid := range batchIDs {
		var batchID = bid // cannot use the address of the loop var
		log.L(ag.ctx).Infof("Batch '%s' contains reference to received blob %s", &bid, blobHash)
		ag.rewindBatches <- batchID
	}
	return nil
}
