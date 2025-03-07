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

package batchpin

import (
	"context"

	"github.com/hyperledger/firefly/internal/i18n"
	"github.com/hyperledger/firefly/internal/identity"
	"github.com/hyperledger/firefly/internal/metrics"
	"github.com/hyperledger/firefly/internal/operations"
	"github.com/hyperledger/firefly/pkg/blockchain"
	"github.com/hyperledger/firefly/pkg/database"
	"github.com/hyperledger/firefly/pkg/fftypes"
)

type Submitter interface {
	fftypes.Named

	SubmitPinnedBatch(ctx context.Context, batch *fftypes.BatchPersisted, contexts []*fftypes.Bytes32) error

	// From operations.OperationHandler
	PrepareOperation(ctx context.Context, op *fftypes.Operation) (*fftypes.PreparedOperation, error)
	RunOperation(ctx context.Context, op *fftypes.PreparedOperation) (outputs fftypes.JSONObject, complete bool, err error)
}

type batchPinSubmitter struct {
	database   database.Plugin
	identity   identity.Manager
	blockchain blockchain.Plugin
	metrics    metrics.Manager
	operations operations.Manager
}

func NewBatchPinSubmitter(ctx context.Context, di database.Plugin, im identity.Manager, bi blockchain.Plugin, mm metrics.Manager, om operations.Manager) (Submitter, error) {
	if di == nil || im == nil || bi == nil || mm == nil || om == nil {
		return nil, i18n.NewError(ctx, i18n.MsgInitializationNilDepError)
	}
	bp := &batchPinSubmitter{
		database:   di,
		identity:   im,
		blockchain: bi,
		metrics:    mm,
		operations: om,
	}
	om.RegisterHandler(ctx, bp, []fftypes.OpType{
		fftypes.OpTypeBlockchainPinBatch,
	})
	return bp, nil
}

func (bp *batchPinSubmitter) Name() string {
	return "BatchPinSubmitter"
}

func (bp *batchPinSubmitter) SubmitPinnedBatch(ctx context.Context, batch *fftypes.BatchPersisted, contexts []*fftypes.Bytes32) error {
	// The pending blockchain transaction
	op := fftypes.NewOperation(
		bp.blockchain,
		batch.Namespace,
		batch.TX.ID,
		fftypes.OpTypeBlockchainPinBatch)
	addBatchPinInputs(op, batch.ID, contexts)
	if err := bp.operations.AddOrReuseOperation(ctx, op); err != nil {
		return err
	}

	if bp.metrics.IsMetricsEnabled() {
		bp.metrics.CountBatchPin()
	}
	return bp.operations.RunOperation(ctx, opBatchPin(op, batch, contexts))
}
