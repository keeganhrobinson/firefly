// Copyright © 2021 Kaleido, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in comdiliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or imdilied.
// See the License for the specific language governing permissions and
// limitations under the License.

package batch

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/hyperledger/firefly/internal/config"
	"github.com/hyperledger/firefly/internal/log"
	"github.com/hyperledger/firefly/internal/retry"
	"github.com/hyperledger/firefly/internal/txcommon"
	"github.com/hyperledger/firefly/mocks/databasemocks"
	"github.com/hyperledger/firefly/mocks/datamocks"
	"github.com/hyperledger/firefly/mocks/sysmessagingmocks"
	"github.com/hyperledger/firefly/mocks/txcommonmocks"
	"github.com/hyperledger/firefly/pkg/fftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func newTestBatchProcessor(t *testing.T, dispatch DispatchHandler) (func(), *databasemocks.Plugin, *batchProcessor) {
	bm, cancel := newTestBatchManager(t)
	mdi := bm.database.(*databasemocks.Plugin)
	mni := bm.ni.(*sysmessagingmocks.LocalNodeInfo)
	mdm := bm.data.(*datamocks.Manager)
	txHelper := txcommon.NewTransactionHelper(mdi, mdm)
	mni.On("GetNodeUUID", mock.Anything).Return(fftypes.NewUUID()).Maybe()
	bp := newBatchProcessor(bm, &batchProcessorConf{
		namespace: "ns1",
		txType:    fftypes.TransactionTypeBatchPin,
		signer:    fftypes.SignerRef{Author: "did:firefly:org/abcd", Key: "0x12345"},
		dispatch:  dispatch,
		DispatcherOptions: DispatcherOptions{
			BatchMaxSize:   10,
			BatchMaxBytes:  1024 * 1024,
			BatchTimeout:   100 * time.Millisecond,
			DisposeTimeout: 200 * time.Millisecond,
		},
	}, &retry.Retry{
		InitialDelay: 1 * time.Microsecond,
		MaximumDelay: 1 * time.Microsecond,
	}, txHelper)
	bp.txHelper = &txcommonmocks.Helper{}
	return cancel, mdi, bp
}

func mockRunAsGroupPassthrough(mdi *databasemocks.Plugin) {
	rag := mdi.On("RunAsGroup", mock.Anything, mock.Anything)
	rag.RunFn = func(a mock.Arguments) {
		fn := a[1].(func(context.Context) error)
		rag.ReturnArguments = mock.Arguments{fn(a[0].(context.Context))}
	}
}

func TestUnfilledBatch(t *testing.T) {
	log.SetLevel("debug")
	config.Reset()

	dispatched := make(chan *DispatchState)
	cancel, mdi, bp := newTestBatchProcessor(t, func(c context.Context, state *DispatchState) error {
		dispatched <- state
		return nil
	})
	defer cancel()

	mockRunAsGroupPassthrough(mdi)
	mdi.On("UpdateMessages", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mdi.On("UpsertBatch", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mth := bp.txHelper.(*txcommonmocks.Helper)
	mth.On("SubmitNewTransaction", mock.Anything, "ns1", fftypes.TransactionTypeBatchPin).Return(fftypes.NewUUID(), nil)

	mdm := bp.data.(*datamocks.Manager)
	mdm.On("UpdateMessageIfCached", mock.Anything, mock.Anything).Return()

	// Dispatch the work
	go func() {
		for i := 0; i < 5; i++ {
			msgid := fftypes.NewUUID()
			bp.newWork <- &batchWork{
				msg: &fftypes.Message{Header: fftypes.MessageHeader{ID: msgid}, Sequence: int64(1000 + i)},
			}
		}
	}()

	// Wait for the confirmations, and the dispatch
	batch := <-dispatched

	// Check we got all the messages in a single batch
	assert.Equal(t, 5, len(batch.Messages))

	bp.cancelCtx()
	<-bp.done

	mdm.AssertExpectations(t)
	mdi.AssertExpectations(t)
	mth.AssertExpectations(t)
}

func TestBatchSizeOverflow(t *testing.T) {
	log.SetLevel("debug")
	config.Reset()

	dispatched := make(chan *DispatchState)
	cancel, mdi, bp := newTestBatchProcessor(t, func(c context.Context, state *DispatchState) error {
		dispatched <- state
		return nil
	})
	defer cancel()
	bp.conf.BatchMaxBytes = batchSizeEstimateBase + (&fftypes.Message{}).EstimateSize(false) + 100
	mockRunAsGroupPassthrough(mdi)
	mdi.On("UpdateMessages", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mdi.On("UpsertBatch", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	mth := bp.txHelper.(*txcommonmocks.Helper)
	mth.On("SubmitNewTransaction", mock.Anything, "ns1", fftypes.TransactionTypeBatchPin).Return(fftypes.NewUUID(), nil)

	mdm := bp.data.(*datamocks.Manager)
	mdm.On("UpdateMessageIfCached", mock.Anything, mock.Anything).Return()

	// Dispatch the work
	msgIDs := []*fftypes.UUID{fftypes.NewUUID(), fftypes.NewUUID()}
	go func() {
		for i := 0; i < 2; i++ {
			bp.newWork <- &batchWork{
				msg: &fftypes.Message{Header: fftypes.MessageHeader{ID: msgIDs[i]}, Sequence: int64(1000 + i)},
			}
		}
	}()

	// Wait for the confirmations, and the dispatch
	batch1 := <-dispatched
	batch2 := <-dispatched

	// Check we got all messages across two batches
	assert.Equal(t, 1, len(batch1.Messages))
	assert.Equal(t, msgIDs[0], batch1.Messages[0].Header.ID)
	assert.Equal(t, 1, len(batch2.Messages))
	assert.Equal(t, msgIDs[1], batch2.Messages[0].Header.ID)

	bp.cancelCtx()
	<-bp.done

	mdi.AssertExpectations(t)
	mth.AssertExpectations(t)
	mdm.AssertExpectations(t)
}

func TestCloseToUnblockDispatch(t *testing.T) {
	cancel, _, bp := newTestBatchProcessor(t, func(c context.Context, state *DispatchState) error {
		return fmt.Errorf("pop")
	})
	defer cancel()
	bp.cancelCtx()
	bp.dispatchBatch(&DispatchState{})
	<-bp.done
}

func TestCloseToUnblockUpsertBatch(t *testing.T) {

	cancel, mdi, bp := newTestBatchProcessor(t, func(c context.Context, state *DispatchState) error {
		return nil
	})
	defer cancel()
	bp.retry.MaximumDelay = 1 * time.Microsecond
	bp.conf.BatchMaxSize = 1
	bp.conf.BatchTimeout = 100 * time.Second
	mockRunAsGroupPassthrough(mdi)
	waitForCall := make(chan bool)
	mth := bp.txHelper.(*txcommonmocks.Helper)
	mth.On("SubmitNewTransaction", mock.Anything, "ns1", fftypes.TransactionTypeBatchPin).
		Run(func(a mock.Arguments) {
			waitForCall <- true
			<-waitForCall
		}).
		Return(nil, fmt.Errorf("pop"))

	// Generate the work
	msgid := fftypes.NewUUID()
	go func() {
		bp.newWork <- &batchWork{
			msg: &fftypes.Message{Header: fftypes.MessageHeader{ID: msgid}, Sequence: int64(1000)},
		}
	}()

	// Ensure the mock has been run
	<-waitForCall
	close(waitForCall)

	// Close to unblock
	bp.cancelCtx()
	<-bp.done
}

func TestCalcPinsFail(t *testing.T) {
	cancel, _, bp := newTestBatchProcessor(t, func(c context.Context, state *DispatchState) error {
		return nil
	})
	defer cancel()
	bp.cancelCtx()
	mdi := bp.database.(*databasemocks.Plugin)
	mdi.On("UpsertNonceNext", mock.Anything, mock.Anything).Return(fmt.Errorf("pop"))
	mockRunAsGroupPassthrough(mdi)

	gid := fftypes.NewRandB32()
	err := bp.sealBatch(&DispatchState{
		Persisted: fftypes.BatchPersisted{
			BatchHeader: fftypes.BatchHeader{
				Group: gid,
			},
		},
		Messages: []*fftypes.Message{
			{Header: fftypes.MessageHeader{
				Group:  gid,
				Topics: fftypes.FFStringArray{"topic1"},
			}},
		},
	})
	assert.Regexp(t, "FF10158", err)

	<-bp.done

	mdi.AssertExpectations(t)
}

func TestAddWorkInSort(t *testing.T) {
	cancel, _, bp := newTestBatchProcessor(t, func(c context.Context, state *DispatchState) error {
		return nil
	})
	defer cancel()
	bp.assemblyQueue = []*batchWork{
		{msg: &fftypes.Message{Sequence: 200}},
		{msg: &fftypes.Message{Sequence: 201}},
		{msg: &fftypes.Message{Sequence: 202}},
		{msg: &fftypes.Message{Sequence: 204}},
	}
	_, _ = bp.addWork(&batchWork{
		msg: &fftypes.Message{Sequence: 203},
	})
	assert.Equal(t, []*batchWork{
		{msg: &fftypes.Message{Sequence: 200}},
		{msg: &fftypes.Message{Sequence: 201}},
		{msg: &fftypes.Message{Sequence: 202}},
		{msg: &fftypes.Message{Sequence: 203}},
		{msg: &fftypes.Message{Sequence: 204}},
	}, bp.assemblyQueue)
}

func TestStartQuiesceNonBlocking(t *testing.T) {
	cancel, _, bp := newTestBatchProcessor(t, func(c context.Context, state *DispatchState) error {
		return nil
	})
	defer cancel()
	bp.startQuiesce()
	bp.startQuiesce() // we're just checking this doesn't hang
}

func TestMarkMessageDispatchedUnpinnedOK(t *testing.T) {
	log.SetLevel("debug")
	config.Reset()

	dispatched := make(chan *DispatchState)
	cancel, mdi, bp := newTestBatchProcessor(t, func(c context.Context, state *DispatchState) error {
		dispatched <- state
		return nil
	})
	defer cancel()
	bp.conf.txType = fftypes.TransactionTypeUnpinned

	mockRunAsGroupPassthrough(mdi)
	mdi.On("UpdateMessages", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mdi.On("UpsertBatch", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mdi.On("InsertEvent", mock.Anything, mock.Anything).Return(fmt.Errorf("pop")).Once()
	mdi.On("InsertEvent", mock.Anything, mock.Anything).Return(nil)

	mth := bp.txHelper.(*txcommonmocks.Helper)
	mth.On("SubmitNewTransaction", mock.Anything, "ns1", fftypes.TransactionTypeUnpinned).Return(fftypes.NewUUID(), nil)

	mdm := bp.data.(*datamocks.Manager)
	mdm.On("UpdateMessageIfCached", mock.Anything, mock.Anything).Return()

	// Dispatch the work
	go func() {
		for i := 0; i < 5; i++ {
			msgid := fftypes.NewUUID()
			bp.newWork <- &batchWork{
				msg: &fftypes.Message{Header: fftypes.MessageHeader{ID: msgid, Topics: fftypes.FFStringArray{"topic1"}}, Sequence: int64(1000 + i)},
			}
		}
	}()

	// Wait for the confirmations, and the dispatch
	batch := <-dispatched

	// Check we got all the messages in a single batch
	assert.Equal(t, 5, len(batch.Messages))

	bp.cancelCtx()
	<-bp.done

	mdi.AssertExpectations(t)
	mdm.AssertExpectations(t)
	mth.AssertExpectations(t)
}

func TestMaskContextsDuplicate(t *testing.T) {
	log.SetLevel("debug")
	config.Reset()

	dispatched := make(chan *DispatchState)
	cancel, mdi, bp := newTestBatchProcessor(t, func(c context.Context, state *DispatchState) error {
		dispatched <- state
		return nil
	})
	defer cancel()

	mdi.On("UpsertNonceNext", mock.Anything, mock.Anything).Return(nil).Once()
	mdi.On("UpdateMessage", mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

	messages := []*fftypes.Message{
		{
			Header: fftypes.MessageHeader{
				ID:     fftypes.NewUUID(),
				Type:   fftypes.MessageTypePrivate,
				Group:  fftypes.NewRandB32(),
				Topics: fftypes.FFStringArray{"topic1"},
			},
		},
	}

	_, err := bp.maskContexts(bp.ctx, messages)
	assert.NoError(t, err)

	// 2nd time no DB ops
	_, err = bp.maskContexts(bp.ctx, messages)
	assert.NoError(t, err)

	bp.cancelCtx()
	<-bp.done

	mdi.AssertExpectations(t)
}

func TestMaskContextsUpdataMessageFail(t *testing.T) {
	log.SetLevel("debug")
	config.Reset()

	dispatched := make(chan *DispatchState)
	cancel, mdi, bp := newTestBatchProcessor(t, func(c context.Context, state *DispatchState) error {
		dispatched <- state
		return nil
	})
	defer cancel()

	mdi.On("UpsertNonceNext", mock.Anything, mock.Anything).Return(nil).Once()
	mdi.On("UpdateMessage", mock.Anything, mock.Anything, mock.Anything).Return(fmt.Errorf("pop")).Once()

	messages := []*fftypes.Message{
		{
			Header: fftypes.MessageHeader{
				ID:     fftypes.NewUUID(),
				Type:   fftypes.MessageTypePrivate,
				Group:  fftypes.NewRandB32(),
				Topics: fftypes.FFStringArray{"topic1"},
			},
		},
	}

	_, err := bp.maskContexts(bp.ctx, messages)
	assert.Regexp(t, "pop", err)

	bp.cancelCtx()
	<-bp.done

	mdi.AssertExpectations(t)
}

func TestBigBatchEstimate(t *testing.T) {
	log.SetLevel("debug")
	config.Reset()

	bd := []byte(`{
		"id": "37ba893b-fcfa-4cf9-8ce8-34cd8bc9bc72",
		"type": "broadcast",
		"namespace": "default",
		"node": "248ba775-f595-40a6-a989-c2f2faae2dea",
		"author": "did:firefly:org/org_0",
		"key": "0x7e3bb2198959d3a1c3ede9db1587560320ce8998",
		"Group": null,
		"created": "2022-03-18T14:57:33.228374398Z",
		"hash": "7c620c12207ec153afea75d958de3edf601beced2570c798ebc246c2c44a5f66",
		"payload": {
		  "tx": {
			"type": "batch_pin",
			"id": "8d3f06b8-adb5-4745-a536-a9e262fd2e9f"
		  },
		  "messages": [
			{
			  "header": {
				"id": "2b393190-28e7-4b86-8af6-00906e94989b",
				"type": "broadcast",
				"txtype": "batch_pin",
				"author": "did:firefly:org/org_0",
				"key": "0x7e3bb2198959d3a1c3ede9db1587560320ce8998",
				"created": "2022-03-18T14:57:32.209734225Z",
				"namespace": "default",
				"topics": [
				  "default"
				],
				"tag": "perf_02e01e12-b918-4982-8407-2f9a08d673f3_740",
				"datahash": "b5b0c398450707b885f5973248ffa9a542f4c2f54860eba6c2d7aee48d0f9109"
			  },
			  "hash": "5fc430f1c8134c6c32c4e34ef65984843bb77bb19e73c862d464669537d96dbd",
			  "data": [
				{
				  "id": "147743b4-bd23-4da1-bd21-90c4ad9f1650",
				  "hash": "8ed265110f60711f79de1bc87b476e00bd8f8be436cdda3cf27fbf886d5e6ce6"
				}
			  ]
			}
		  ],
		  "data": [
			{
			  "id": "147743b4-bd23-4da1-bd21-90c4ad9f1650",
			  "validator": "json",
			  "namespace": "default",
			  "hash": "8ed265110f60711f79de1bc87b476e00bd8f8be436cdda3cf27fbf886d5e6ce6",
			  "created": "2022-03-18T14:57:32.209705277Z",
			  "value": {
				"broadcastID": "740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740740"
			  }
			}
		  ]
		}
	  }`)
	var batch fftypes.Batch
	err := json.Unmarshal(bd, &batch)
	assert.NoError(t, err)

	sizeEstimate := batchSizeEstimateBase
	for i, m := range batch.Payload.Messages {
		dataJSONSize := 0
		bw := &batchWork{
			msg: m,
		}
		for _, dr := range m.Data {
			for _, d := range batch.Payload.Data {
				if d.ID.Equals(dr.ID) {
					bw.data = append(bw.data, d)
					break
				}
			}
			bd, err := json.Marshal(&bw.data)
			assert.NoError(t, err)
			dataJSONSize += len(bd)
		}
		md, err := json.Marshal(&bw.msg)
		assert.NoError(t, err)
		msgJSONSize := len(md)
		t.Logf("Msg=%.3d/%s Estimate=%d JSON - Msg=%d Data=%d Total=%d", i, m.Header.ID, bw.estimateSize(), msgJSONSize, dataJSONSize, msgJSONSize+dataJSONSize)
		sizeEstimate += bw.estimateSize()
	}

	assert.Greater(t, sizeEstimate, int64(len(bd)))
}
