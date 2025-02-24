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

package txcommon

import (
	"context"
	"encoding/json"

	"github.com/hyperledger/firefly/internal/i18n"
	"github.com/hyperledger/firefly/pkg/fftypes"
)

func AddTokenPoolCreateInputs(op *fftypes.Operation, pool *fftypes.TokenPool) (err error) {
	var poolJSON []byte
	if poolJSON, err = json.Marshal(pool); err == nil {
		err = json.Unmarshal(poolJSON, &op.Input)
	}
	return err
}

func RetrieveTokenPoolCreateInputs(ctx context.Context, op *fftypes.Operation) (*fftypes.TokenPool, error) {
	var pool fftypes.TokenPool
	s := op.Input.String()
	if err := json.Unmarshal([]byte(s), &pool); err != nil {
		return nil, i18n.WrapError(ctx, err, i18n.MsgJSONObjectParseFailed, s)
	}
	return &pool, nil
}

func AddTokenPoolActivateInputs(op *fftypes.Operation, poolID *fftypes.UUID, blockchainInfo fftypes.JSONObject) {
	op.Input = fftypes.JSONObject{
		"id":   poolID.String(),
		"info": blockchainInfo,
	}
}

func RetrieveTokenPoolActivateInputs(ctx context.Context, op *fftypes.Operation) (*fftypes.UUID, fftypes.JSONObject, error) {
	id, err := fftypes.ParseUUID(ctx, op.Input.GetString("id"))
	info := op.Input.GetObject("info")
	return id, info, err
}

func AddTokenTransferInputs(op *fftypes.Operation, transfer *fftypes.TokenTransfer) (err error) {
	var transferJSON []byte
	if transferJSON, err = json.Marshal(transfer); err == nil {
		err = json.Unmarshal(transferJSON, &op.Input)
	}
	return err
}

func RetrieveTokenTransferInputs(ctx context.Context, op *fftypes.Operation) (*fftypes.TokenTransfer, error) {
	var transfer fftypes.TokenTransfer
	s := op.Input.String()
	if err := json.Unmarshal([]byte(s), &transfer); err != nil {
		return nil, i18n.WrapError(ctx, err, i18n.MsgJSONObjectParseFailed, s)
	}
	return &transfer, nil
}

func AddTokenApprovalInputs(op *fftypes.Operation, approval *fftypes.TokenApproval) (err error) {
	var j []byte
	if j, err = json.Marshal(approval); err == nil {
		err = json.Unmarshal(j, &op.Input)
	}
	return err
}

func RetrieveTokenApprovalInputs(ctx context.Context, op *fftypes.Operation) (approval *fftypes.TokenApproval, err error) {
	var approve fftypes.TokenApproval
	s := op.Input.String()
	if err = json.Unmarshal([]byte(s), &approve); err != nil {
		return nil, i18n.WrapError(ctx, err, i18n.MsgJSONObjectParseFailed, s)
	}
	return &approve, nil
}
