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

package apiserver

import (
	"net/http/httptest"
	"testing"

	"github.com/hyperledger/firefly/internal/networkmap"
	"github.com/hyperledger/firefly/mocks/networkmapmocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestGetDIDDocByDID(t *testing.T) {
	o, r := newTestAPIServer()
	nmn := &networkmapmocks.Manager{}
	o.On("NetworkMap").Return(nmn)
	req := httptest.NewRequest("GET", "/api/v1/network/diddocs/did:firefly:org/org_1", nil)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	res := httptest.NewRecorder()

	nmn.On("GetDIDDocForIndentityByDID", mock.Anything, "did:firefly:org/org_1").
		Return(&networkmap.DIDDocument{}, nil)
	r.ServeHTTP(res, req)

	assert.Equal(t, 200, res.Result().StatusCode)
}
