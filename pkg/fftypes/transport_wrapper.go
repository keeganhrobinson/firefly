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

package fftypes

type TransportPayloadType = FFEnum

var (
	TransportPayloadTypeMessage = ffEnum("transportpayload", "message")
	TransportPayloadTypeBatch   = ffEnum("transportpayload", "batch")
)

// TransportWrapper wraps paylaods over data exchange transfers, for easy deserialization at target
type TransportWrapper struct {
	Group *Group `json:"group,omitempty"`
	Batch *Batch `json:"batch,omitempty"`
}

type TransportStatusUpdate struct {
	Error    string     `json:"error,omitempty"`
	Manifest string     `json:"manifest,omitempty"`
	Info     JSONObject `json:"info,omitempty"`
	Hash     string     `json:"hash,omitempty"`
}
