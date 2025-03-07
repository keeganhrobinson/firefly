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

package privatemessaging

import (
	"context"

	"github.com/hyperledger/firefly/internal/batch"
	"github.com/hyperledger/firefly/internal/batchpin"
	"github.com/hyperledger/firefly/internal/config"
	"github.com/hyperledger/firefly/internal/data"
	"github.com/hyperledger/firefly/internal/i18n"
	"github.com/hyperledger/firefly/internal/identity"
	"github.com/hyperledger/firefly/internal/log"
	"github.com/hyperledger/firefly/internal/metrics"
	"github.com/hyperledger/firefly/internal/operations"
	"github.com/hyperledger/firefly/internal/retry"
	"github.com/hyperledger/firefly/internal/syncasync"
	"github.com/hyperledger/firefly/internal/sysmessaging"
	"github.com/hyperledger/firefly/pkg/blockchain"
	"github.com/hyperledger/firefly/pkg/database"
	"github.com/hyperledger/firefly/pkg/dataexchange"
	"github.com/hyperledger/firefly/pkg/fftypes"
	"github.com/karlseguin/ccache"
)

const pinnedPrivateDispatcherName = "pinned_private"
const unpinnedPrivateDispatcherName = "unpinned_private"

type Manager interface {
	fftypes.Named
	GroupManager

	Start() error
	NewMessage(ns string, msg *fftypes.MessageInOut) sysmessaging.MessageSender
	SendMessage(ctx context.Context, ns string, in *fftypes.MessageInOut, waitConfirm bool) (out *fftypes.Message, err error)
	RequestReply(ctx context.Context, ns string, request *fftypes.MessageInOut) (reply *fftypes.MessageInOut, err error)

	// From operations.OperationHandler
	PrepareOperation(ctx context.Context, op *fftypes.Operation) (*fftypes.PreparedOperation, error)
	RunOperation(ctx context.Context, op *fftypes.PreparedOperation) (outputs fftypes.JSONObject, complete bool, err error)
}

type privateMessaging struct {
	groupManager

	ctx                   context.Context
	database              database.Plugin
	identity              identity.Manager
	exchange              dataexchange.Plugin
	blockchain            blockchain.Plugin
	batch                 batch.Manager
	data                  data.Manager
	syncasync             syncasync.Bridge
	batchpin              batchpin.Submitter
	retry                 retry.Retry
	localNodeName         string
	localNodeID           *fftypes.UUID // lookup and cached on first use, as might not be registered at startup
	opCorrelationRetries  int
	maxBatchPayloadLength int64
	metrics               metrics.Manager
	operations            operations.Manager
	orgFirstNodes         map[fftypes.UUID]*fftypes.Identity
}

func NewPrivateMessaging(ctx context.Context, di database.Plugin, im identity.Manager, dx dataexchange.Plugin, bi blockchain.Plugin, ba batch.Manager, dm data.Manager, sa syncasync.Bridge, bp batchpin.Submitter, mm metrics.Manager, om operations.Manager) (Manager, error) {
	if di == nil || im == nil || dx == nil || bi == nil || ba == nil || dm == nil || mm == nil || om == nil {
		return nil, i18n.NewError(ctx, i18n.MsgInitializationNilDepError)
	}

	pm := &privateMessaging{
		ctx:           ctx,
		database:      di,
		identity:      im,
		exchange:      dx,
		blockchain:    bi,
		batch:         ba,
		data:          dm,
		syncasync:     sa,
		batchpin:      bp,
		localNodeName: config.GetString(config.NodeName),
		groupManager: groupManager{
			database:      di,
			data:          dm,
			groupCacheTTL: config.GetDuration(config.GroupCacheTTL),
		},
		retry: retry.Retry{
			InitialDelay: config.GetDuration(config.PrivateMessagingRetryInitDelay),
			MaximumDelay: config.GetDuration(config.PrivateMessagingRetryMaxDelay),
			Factor:       config.GetFloat64(config.PrivateMessagingRetryFactor),
		},
		opCorrelationRetries:  config.GetInt(config.PrivateMessagingOpCorrelationRetries),
		maxBatchPayloadLength: config.GetByteSize(config.PrivateMessagingBatchPayloadLimit),
		metrics:               mm,
		operations:            om,
		orgFirstNodes:         make(map[fftypes.UUID]*fftypes.Identity),
	}
	pm.groupManager.groupCache = ccache.New(
		// We use a LRU cache with a size-aware max
		ccache.Configure().
			MaxSize(config.GetByteSize(config.GroupCacheSize)),
	)

	bo := batch.DispatcherOptions{
		BatchType:      fftypes.BatchTypePrivate,
		BatchMaxSize:   config.GetUint(config.PrivateMessagingBatchSize),
		BatchMaxBytes:  pm.maxBatchPayloadLength,
		BatchTimeout:   config.GetDuration(config.PrivateMessagingBatchTimeout),
		DisposeTimeout: config.GetDuration(config.PrivateMessagingBatchAgentTimeout),
	}

	ba.RegisterDispatcher(pinnedPrivateDispatcherName,
		fftypes.TransactionTypeBatchPin,
		[]fftypes.MessageType{
			fftypes.MessageTypeGroupInit,
			fftypes.MessageTypePrivate,
			fftypes.MessageTypeTransferPrivate,
		},
		pm.dispatchPinnedBatch, bo)

	ba.RegisterDispatcher(unpinnedPrivateDispatcherName,
		fftypes.TransactionTypeUnpinned,
		[]fftypes.MessageType{
			fftypes.MessageTypePrivate,
		},
		pm.dispatchUnpinnedBatch, bo)

	om.RegisterHandler(ctx, pm, []fftypes.OpType{
		fftypes.OpTypeDataExchangeSendBlob,
		fftypes.OpTypeDataExchangeSendBatch,
	})

	return pm, nil
}

func (pm *privateMessaging) Name() string {
	return "PrivateMessaging"
}

func (pm *privateMessaging) Start() error {
	return pm.exchange.Start()
}

func (pm *privateMessaging) dispatchPinnedBatch(ctx context.Context, state *batch.DispatchState) error {
	err := pm.dispatchBatchCommon(ctx, state)
	if err != nil {
		return err
	}

	log.L(ctx).Infof("Pinning private batch %s with author=%s key=%s group=%s", state.Persisted.ID, state.Persisted.Author, state.Persisted.Key, state.Persisted.Group)
	return pm.batchpin.SubmitPinnedBatch(ctx, &state.Persisted, state.Pins)
}

func (pm *privateMessaging) dispatchUnpinnedBatch(ctx context.Context, state *batch.DispatchState) error {
	return pm.dispatchBatchCommon(ctx, state)
}

func (pm *privateMessaging) dispatchBatchCommon(ctx context.Context, state *batch.DispatchState) error {
	batch := state.Persisted.GenInflight(state.Messages, state.Data)
	tw := &fftypes.TransportWrapper{
		Batch: batch,
	}

	// Retrieve the group
	group, nodes, err := pm.groupManager.getGroupNodes(ctx, batch.Group, false /* fail if not found */)
	if err != nil {
		return err
	}

	if batch.Payload.TX.Type == fftypes.TransactionTypeUnpinned {
		// In the case of an un-pinned message we cannot be sure the group has been broadcast via the blockchain.
		// So we have to take the hit of sending it along with every message.
		tw.Group = group
	}

	return pm.sendData(ctx, tw, nodes)
}

func (pm *privateMessaging) transferBlobs(ctx context.Context, data fftypes.DataArray, txid *fftypes.UUID, node *fftypes.Identity) error {
	// Send all the blobs associated with this batch
	for _, d := range data {
		if d.Blob != nil {
			if d.Blob.Hash == nil {
				return i18n.NewError(ctx, i18n.MsgDataMissingBlobHash, d.ID)
			}

			blob, err := pm.database.GetBlobMatchingHash(ctx, d.Blob.Hash)
			if err != nil {
				return err
			}
			if blob == nil {
				return i18n.NewError(ctx, i18n.MsgBlobNotFound, d.Blob)
			}

			op := fftypes.NewOperation(
				pm.exchange,
				d.Namespace,
				txid,
				fftypes.OpTypeDataExchangeSendBlob)
			addTransferBlobInputs(op, node.ID, blob.Hash)
			log.L(ctx).Debugf("Transferring blob %s for data %s in operation %s", d.Blob.Hash, d.ID, op.ID)
			if err = pm.operations.AddOrReuseOperation(ctx, op); err != nil {
				return err
			}

			if err = pm.operations.RunOperation(ctx, opSendBlob(op, node, blob)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (pm *privateMessaging) sendData(ctx context.Context, tw *fftypes.TransportWrapper, nodes []*fftypes.Identity) (err error) {
	l := log.L(ctx)
	batch := tw.Batch

	// Lookup the local org
	localOrg, err := pm.identity.GetNodeOwnerOrg(ctx)
	if err != nil {
		return err
	}

	// Write it to the dataexchange for each member
	for i, node := range nodes {

		if node.Parent.Equals(localOrg.ID) {
			l.Debugf("Skipping send of batch for local node %s:%s for group=%s node=%s (%d/%d)", batch.Namespace, batch.ID, batch.Group, node.ID, i+1, len(nodes))
			continue
		}

		l.Debugf("Sending batch %s:%s to group=%s node=%s (%d/%d)", batch.Namespace, batch.ID, batch.Group, node.ID, i+1, len(nodes))

		// Initiate transfer of any blobs first
		if err = pm.transferBlobs(ctx, batch.Payload.Data, batch.Payload.TX.ID, node); err != nil {
			return err
		}

		op := fftypes.NewOperation(
			pm.exchange,
			batch.Namespace,
			batch.Payload.TX.ID,
			fftypes.OpTypeDataExchangeSendBatch)
		var groupHash *fftypes.Bytes32
		if tw.Group != nil {
			groupHash = tw.Group.Hash
		}
		addBatchSendInputs(op, node.ID, groupHash, batch.ID)
		if err = pm.operations.AddOrReuseOperation(ctx, op); err != nil {
			return err
		}
		if err = pm.operations.RunOperation(ctx, opSendBatch(op, node, tw)); err != nil {
			return err
		}
	}

	return nil
}
