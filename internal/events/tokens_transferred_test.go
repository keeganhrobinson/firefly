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
	"fmt"
	"testing"

	"github.com/hyperledger/firefly/mocks/databasemocks"
	"github.com/hyperledger/firefly/mocks/tokenmocks"
	"github.com/hyperledger/firefly/mocks/txcommonmocks"
	"github.com/hyperledger/firefly/pkg/blockchain"
	"github.com/hyperledger/firefly/pkg/fftypes"
	"github.com/hyperledger/firefly/pkg/tokens"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func newTransfer() *tokens.TokenTransfer {
	return &tokens.TokenTransfer{
		PoolProtocolID: "F1",
		TokenTransfer: fftypes.TokenTransfer{
			Type:       fftypes.TokenTransferTypeTransfer,
			TokenIndex: "0",
			Connector:  "erc1155",
			Key:        "0x12345",
			From:       "0x1",
			To:         "0x2",
			ProtocolID: "123",
			URI:        "firefly://token/1",
			Amount:     *fftypes.NewFFBigInt(1),
			TX: fftypes.TransactionRef{
				ID:   fftypes.NewUUID(),
				Type: fftypes.TransactionTypeTokenTransfer,
			},
		},
		Event: blockchain.Event{
			BlockchainTXID: "0xffffeeee",
			Name:           "Transfer",
			ProtocolID:     "0000/0000/0000",
			Info:           fftypes.JSONObject{"some": "info"},
		},
	}
}

func TestTokensTransferredSucceedWithRetries(t *testing.T) {
	em, cancel := newTestEventManagerWithMetrics(t)
	defer cancel()

	mdi := em.database.(*databasemocks.Plugin)
	mti := &tokenmocks.Plugin{}
	mth := em.txHelper.(*txcommonmocks.Helper)

	transfer := newTransfer()
	transfer.TX = fftypes.TransactionRef{}
	pool := &fftypes.TokenPool{
		Namespace: "ns1",
	}

	mdi.On("GetTokenTransferByProtocolID", em.ctx, "erc1155", "123").Return(nil, fmt.Errorf("pop")).Once()
	mdi.On("GetTokenTransferByProtocolID", em.ctx, "erc1155", "123").Return(nil, nil).Times(4)
	mdi.On("GetTokenPoolByProtocolID", em.ctx, "erc1155", "F1").Return(nil, fmt.Errorf("pop")).Once()
	mdi.On("GetTokenPoolByProtocolID", em.ctx, "erc1155", "F1").Return(pool, nil).Times(3)
	mth.On("InsertBlockchainEvent", em.ctx, mock.MatchedBy(func(e *fftypes.BlockchainEvent) bool {
		return e.Namespace == pool.Namespace && e.Name == transfer.Event.Name
	})).Return(nil).Times(3)
	mdi.On("InsertEvent", em.ctx, mock.MatchedBy(func(ev *fftypes.Event) bool {
		return ev.Type == fftypes.EventTypeBlockchainEventReceived && ev.Namespace == pool.Namespace
	})).Return(nil).Times(3)
	mdi.On("UpsertTokenTransfer", em.ctx, &transfer.TokenTransfer).Return(fmt.Errorf("pop")).Once()
	mdi.On("UpsertTokenTransfer", em.ctx, &transfer.TokenTransfer).Return(nil).Times(2)
	mdi.On("UpdateTokenBalances", em.ctx, &transfer.TokenTransfer).Return(fmt.Errorf("pop")).Once()
	mdi.On("UpdateTokenBalances", em.ctx, &transfer.TokenTransfer).Return(nil).Once()
	mdi.On("InsertEvent", em.ctx, mock.MatchedBy(func(ev *fftypes.Event) bool {
		return ev.Type == fftypes.EventTypeTransferConfirmed && ev.Reference == transfer.LocalID && ev.Namespace == pool.Namespace
	})).Return(nil).Once()

	err := em.TokensTransferred(mti, transfer)
	assert.NoError(t, err)

	mdi.AssertExpectations(t)
	mti.AssertExpectations(t)
	mth.AssertExpectations(t)
}

func TestTokensTransferredIgnoreExisting(t *testing.T) {
	em, cancel := newTestEventManager(t)
	defer cancel()

	mdi := em.database.(*databasemocks.Plugin)
	mti := &tokenmocks.Plugin{}

	transfer := newTransfer()

	mdi.On("GetTokenTransferByProtocolID", em.ctx, "erc1155", "123").Return(&fftypes.TokenTransfer{}, nil)

	err := em.TokensTransferred(mti, transfer)
	assert.NoError(t, err)

	mdi.AssertExpectations(t)
	mti.AssertExpectations(t)
}

func TestPersistTransferOpFail(t *testing.T) {
	em, cancel := newTestEventManager(t)
	defer cancel()

	mdi := em.database.(*databasemocks.Plugin)

	transfer := newTransfer()
	pool := &fftypes.TokenPool{
		Namespace: "ns1",
	}

	mdi.On("GetTokenTransferByProtocolID", em.ctx, "erc1155", "123").Return(nil, nil)
	mdi.On("GetTokenPoolByProtocolID", em.ctx, "erc1155", "F1").Return(pool, nil)
	mdi.On("GetOperations", em.ctx, mock.Anything).Return(nil, nil, fmt.Errorf("pop"))

	valid, err := em.persistTokenTransfer(em.ctx, transfer)
	assert.False(t, valid)
	assert.EqualError(t, err, "pop")

	mdi.AssertExpectations(t)
}

func TestPersistTransferBadOp(t *testing.T) {
	em, cancel := newTestEventManager(t)
	defer cancel()

	mdi := em.database.(*databasemocks.Plugin)
	mth := em.txHelper.(*txcommonmocks.Helper)

	transfer := newTransfer()
	pool := &fftypes.TokenPool{
		Namespace: "ns1",
	}
	ops := []*fftypes.Operation{{
		Input: fftypes.JSONObject{
			"localId": "bad",
		},
		Transaction: fftypes.NewUUID(),
	}}

	mdi.On("GetTokenTransferByProtocolID", em.ctx, "erc1155", "123").Return(nil, nil)
	mdi.On("GetTokenPoolByProtocolID", em.ctx, "erc1155", "F1").Return(pool, nil)
	mdi.On("GetOperations", em.ctx, mock.Anything).Return(ops, nil, nil)
	mth.On("PersistTransaction", mock.Anything, "ns1", transfer.TX.ID, fftypes.TransactionTypeTokenTransfer, "0xffffeeee").Return(false, fmt.Errorf("pop"))

	valid, err := em.persistTokenTransfer(em.ctx, transfer)
	assert.False(t, valid)
	assert.EqualError(t, err, "pop")

	mdi.AssertExpectations(t)
}

func TestPersistTransferTxFail(t *testing.T) {
	em, cancel := newTestEventManager(t)
	defer cancel()

	mdi := em.database.(*databasemocks.Plugin)
	mth := em.txHelper.(*txcommonmocks.Helper)

	transfer := newTransfer()
	pool := &fftypes.TokenPool{
		Namespace: "ns1",
	}
	localID := fftypes.NewUUID()
	ops := []*fftypes.Operation{{
		Input: fftypes.JSONObject{
			"localId": localID.String(),
		},
	}}

	mdi.On("GetTokenTransferByProtocolID", em.ctx, "erc1155", "123").Return(nil, nil)
	mdi.On("GetTokenPoolByProtocolID", em.ctx, "erc1155", "F1").Return(pool, nil)
	mdi.On("GetOperations", em.ctx, mock.Anything).Return(ops, nil, nil)
	mth.On("PersistTransaction", mock.Anything, "ns1", transfer.TX.ID, fftypes.TransactionTypeTokenTransfer, "0xffffeeee").Return(false, fmt.Errorf("pop"))

	valid, err := em.persistTokenTransfer(em.ctx, transfer)
	assert.False(t, valid)
	assert.EqualError(t, err, "pop")

	mdi.AssertExpectations(t)
}

func TestPersistTransferGetTransferFail(t *testing.T) {
	em, cancel := newTestEventManager(t)
	defer cancel()

	mdi := em.database.(*databasemocks.Plugin)
	mth := em.txHelper.(*txcommonmocks.Helper)

	transfer := newTransfer()
	pool := &fftypes.TokenPool{
		Namespace: "ns1",
	}
	localID := fftypes.NewUUID()
	ops := []*fftypes.Operation{{
		Input: fftypes.JSONObject{
			"localId": localID.String(),
		},
	}}

	mdi.On("GetTokenTransferByProtocolID", em.ctx, "erc1155", "123").Return(nil, nil)
	mdi.On("GetTokenPoolByProtocolID", em.ctx, "erc1155", "F1").Return(pool, nil)
	mdi.On("GetOperations", em.ctx, mock.Anything).Return(ops, nil, nil)
	mth.On("PersistTransaction", mock.Anything, "ns1", transfer.TX.ID, fftypes.TransactionTypeTokenTransfer, "0xffffeeee").Return(true, nil)
	mdi.On("GetTokenTransfer", em.ctx, localID).Return(nil, fmt.Errorf("pop"))

	valid, err := em.persistTokenTransfer(em.ctx, transfer)
	assert.False(t, valid)
	assert.EqualError(t, err, "pop")

	mdi.AssertExpectations(t)
}

func TestPersistTransferBlockchainEventFail(t *testing.T) {
	em, cancel := newTestEventManager(t)
	defer cancel()

	mdi := em.database.(*databasemocks.Plugin)
	mth := em.txHelper.(*txcommonmocks.Helper)

	transfer := newTransfer()
	pool := &fftypes.TokenPool{
		Namespace: "ns1",
	}
	localID := fftypes.NewUUID()
	ops := []*fftypes.Operation{{
		Input: fftypes.JSONObject{
			"localId": localID.String(),
		},
	}}

	mdi.On("GetTokenTransferByProtocolID", em.ctx, "erc1155", "123").Return(nil, nil)
	mdi.On("GetTokenPoolByProtocolID", em.ctx, "erc1155", "F1").Return(pool, nil)
	mdi.On("GetOperations", em.ctx, mock.Anything).Return(ops, nil, nil)
	mth.On("PersistTransaction", mock.Anything, "ns1", transfer.TX.ID, fftypes.TransactionTypeTokenTransfer, "0xffffeeee").Return(true, nil)
	mdi.On("GetTokenTransfer", em.ctx, localID).Return(nil, nil)
	mth.On("InsertBlockchainEvent", em.ctx, mock.MatchedBy(func(e *fftypes.BlockchainEvent) bool {
		return e.Namespace == pool.Namespace && e.Name == transfer.Event.Name
	})).Return(fmt.Errorf("pop"))

	valid, err := em.persistTokenTransfer(em.ctx, transfer)
	assert.False(t, valid)
	assert.EqualError(t, err, "pop")

	mdi.AssertExpectations(t)
}

func TestTokensTransferredWithTransactionRegenerateLocalID(t *testing.T) {
	em, cancel := newTestEventManager(t)
	defer cancel()

	mdi := em.database.(*databasemocks.Plugin)
	mti := &tokenmocks.Plugin{}
	mth := em.txHelper.(*txcommonmocks.Helper)

	transfer := newTransfer()
	pool := &fftypes.TokenPool{
		Namespace: "ns1",
	}
	localID := fftypes.NewUUID()
	operations := []*fftypes.Operation{{
		Input: fftypes.JSONObject{
			"localId": localID.String(),
		},
	}}

	mdi.On("GetTokenTransferByProtocolID", em.ctx, "erc1155", "123").Return(nil, nil)
	mdi.On("GetTokenPoolByProtocolID", em.ctx, "erc1155", "F1").Return(pool, nil)
	mdi.On("GetOperations", em.ctx, mock.Anything).Return(operations, nil, nil)
	mth.On("PersistTransaction", mock.Anything, "ns1", transfer.TX.ID, fftypes.TransactionTypeTokenTransfer, "0xffffeeee").Return(true, nil)
	mdi.On("GetTokenTransfer", em.ctx, localID).Return(&fftypes.TokenTransfer{}, nil)
	mth.On("InsertBlockchainEvent", em.ctx, mock.MatchedBy(func(e *fftypes.BlockchainEvent) bool {
		return e.Namespace == pool.Namespace && e.Name == transfer.Event.Name
	})).Return(nil)
	mdi.On("InsertEvent", em.ctx, mock.MatchedBy(func(ev *fftypes.Event) bool {
		return ev.Type == fftypes.EventTypeBlockchainEventReceived && ev.Namespace == pool.Namespace
	})).Return(nil)
	mdi.On("UpsertTokenTransfer", em.ctx, &transfer.TokenTransfer).Return(nil)
	mdi.On("UpdateTokenBalances", em.ctx, &transfer.TokenTransfer).Return(nil)

	valid, err := em.persistTokenTransfer(em.ctx, transfer)
	assert.True(t, valid)
	assert.NoError(t, err)

	assert.NotEqual(t, *localID, *transfer.LocalID)

	mdi.AssertExpectations(t)
	mti.AssertExpectations(t)
}

func TestTokensTransferredBadPool(t *testing.T) {
	em, cancel := newTestEventManager(t)
	defer cancel()

	mdi := em.database.(*databasemocks.Plugin)
	mti := &tokenmocks.Plugin{}

	transfer := newTransfer()

	mdi.On("GetTokenTransferByProtocolID", em.ctx, "erc1155", "123").Return(nil, nil)
	mdi.On("GetTokenPoolByProtocolID", em.ctx, "erc1155", "F1").Return(nil, nil)

	err := em.TokensTransferred(mti, transfer)
	assert.NoError(t, err)

	mdi.AssertExpectations(t)
	mti.AssertExpectations(t)
}

func TestTokensTransferredWithMessageReceived(t *testing.T) {
	em, cancel := newTestEventManager(t)
	defer cancel()

	mdi := em.database.(*databasemocks.Plugin)
	mti := &tokenmocks.Plugin{}
	mth := em.txHelper.(*txcommonmocks.Helper)

	uri := "firefly://token/1"
	info := fftypes.JSONObject{"some": "info"}
	transfer := &tokens.TokenTransfer{
		PoolProtocolID: "F1",
		TokenTransfer: fftypes.TokenTransfer{
			Type:       fftypes.TokenTransferTypeTransfer,
			TokenIndex: "0",
			URI:        uri,
			Connector:  "erc1155",
			Key:        "0x12345",
			From:       "0x1",
			To:         "0x2",
			ProtocolID: "123",
			Message:    fftypes.NewUUID(),
			Amount:     *fftypes.NewFFBigInt(1),
		},
		Event: blockchain.Event{
			BlockchainTXID: "0xffffeeee",
			ProtocolID:     "0000/0000/0000",
			Info:           info,
		},
	}
	pool := &fftypes.TokenPool{
		Namespace: "ns1",
	}
	message := &fftypes.Message{
		BatchID: fftypes.NewUUID(),
	}

	mdi.On("GetTokenTransferByProtocolID", em.ctx, "erc1155", "123").Return(nil, nil).Times(2)
	mdi.On("GetTokenPoolByProtocolID", em.ctx, "erc1155", "F1").Return(pool, nil).Times(2)
	mth.On("InsertBlockchainEvent", em.ctx, mock.MatchedBy(func(e *fftypes.BlockchainEvent) bool {
		return e.Namespace == pool.Namespace && e.Name == transfer.Event.Name
	})).Return(nil).Times(2)
	mdi.On("InsertEvent", em.ctx, mock.MatchedBy(func(ev *fftypes.Event) bool {
		return ev.Type == fftypes.EventTypeBlockchainEventReceived && ev.Namespace == pool.Namespace
	})).Return(nil).Times(2)
	mdi.On("UpsertTokenTransfer", em.ctx, &transfer.TokenTransfer).Return(nil).Times(2)
	mdi.On("UpdateTokenBalances", em.ctx, &transfer.TokenTransfer).Return(nil).Times(2)
	mdi.On("GetMessageByID", em.ctx, transfer.Message).Return(nil, fmt.Errorf("pop")).Once()
	mdi.On("GetMessageByID", em.ctx, transfer.Message).Return(message, nil).Once()
	mdi.On("InsertEvent", em.ctx, mock.MatchedBy(func(ev *fftypes.Event) bool {
		return ev.Type == fftypes.EventTypeTransferConfirmed && ev.Reference == transfer.LocalID && ev.Namespace == pool.Namespace
	})).Return(nil).Once()

	err := em.TokensTransferred(mti, transfer)
	assert.NoError(t, err)

	mdi.AssertExpectations(t)
	mti.AssertExpectations(t)
	mth.AssertExpectations(t)
}

func TestTokensTransferredWithMessageSend(t *testing.T) {
	em, cancel := newTestEventManager(t)
	defer cancel()

	mdi := em.database.(*databasemocks.Plugin)
	mti := &tokenmocks.Plugin{}
	mth := em.txHelper.(*txcommonmocks.Helper)

	uri := "firefly://token/1"
	info := fftypes.JSONObject{"some": "info"}
	transfer := &tokens.TokenTransfer{
		PoolProtocolID: "F1",
		TokenTransfer: fftypes.TokenTransfer{
			Type:       fftypes.TokenTransferTypeTransfer,
			TokenIndex: "0",
			URI:        uri,
			Connector:  "erc1155",
			Key:        "0x12345",
			From:       "0x1",
			To:         "0x2",
			ProtocolID: "123",
			Message:    fftypes.NewUUID(),
			Amount:     *fftypes.NewFFBigInt(1),
		},
		Event: blockchain.Event{
			BlockchainTXID: "0xffffeeee",
			ProtocolID:     "0000/0000/0000",
			Info:           info,
		},
	}
	pool := &fftypes.TokenPool{
		Namespace: "ns1",
	}
	message := &fftypes.Message{
		BatchID: fftypes.NewUUID(),
		State:   fftypes.MessageStateStaged,
	}

	mdi.On("GetTokenTransferByProtocolID", em.ctx, "erc1155", "123").Return(nil, nil).Times(2)
	mdi.On("GetTokenPoolByProtocolID", em.ctx, "erc1155", "F1").Return(pool, nil).Times(2)
	mth.On("InsertBlockchainEvent", em.ctx, mock.MatchedBy(func(e *fftypes.BlockchainEvent) bool {
		return e.Namespace == pool.Namespace && e.Name == transfer.Event.Name
	})).Return(nil).Times(2)
	mdi.On("InsertEvent", em.ctx, mock.MatchedBy(func(ev *fftypes.Event) bool {
		return ev.Type == fftypes.EventTypeBlockchainEventReceived && ev.Namespace == pool.Namespace
	})).Return(nil).Times(2)
	mdi.On("UpsertTokenTransfer", em.ctx, &transfer.TokenTransfer).Return(nil).Times(2)
	mdi.On("UpdateTokenBalances", em.ctx, &transfer.TokenTransfer).Return(nil).Times(2)
	mdi.On("GetMessageByID", em.ctx, mock.Anything).Return(message, nil).Times(2)
	mdi.On("ReplaceMessage", em.ctx, mock.MatchedBy(func(msg *fftypes.Message) bool {
		return msg.State == fftypes.MessageStateReady
	})).Return(fmt.Errorf("pop"))
	mdi.On("InsertEvent", em.ctx, mock.MatchedBy(func(ev *fftypes.Event) bool {
		return ev.Type == fftypes.EventTypeTransferConfirmed && ev.Reference == transfer.LocalID && ev.Namespace == pool.Namespace
	})).Return(nil).Once()

	err := em.TokensTransferred(mti, transfer)
	assert.NoError(t, err)

	mdi.AssertExpectations(t)
	mti.AssertExpectations(t)
	mth.AssertExpectations(t)
}
