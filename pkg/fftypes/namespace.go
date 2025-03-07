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

import (
	"context"
	"crypto/sha256"

	"github.com/hyperledger/firefly/internal/i18n"
)

// NamespaceType describes when the namespace was created from local configuration, or broadcast through the network
type NamespaceType = FFEnum

var (
	// NamespaceTypeLocal is a namespace that only exists because it was defined in the local configuration of the node
	NamespaceTypeLocal = ffEnum("namespacetype", "local")
	// NamespaceTypeBroadcast is a namespace that was broadcast through the network. Broadcast namespaces can overwrite a local namespace
	NamespaceTypeBroadcast = ffEnum("namespacetype", "broadcast")
	// NamespaceTypeSystem is a reserved namespace used by FireFly itself
	NamespaceTypeSystem = ffEnum("namespacetype", "system")
)

// Namespace is a isolate set of named resources, to allow multiple applications to co-exist in the same network, with the same named objects.
// Can be used for use case segregation, or multi-tenancy.
type Namespace struct {
	ID          *UUID         `json:"id"`
	Message     *UUID         `json:"message,omitempty"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Type        NamespaceType `json:"type" ffenum:"namespacetype"`
	Created     *FFTime       `json:"created"`
}

func (ns *Namespace) Validate(ctx context.Context, existing bool) (err error) {
	if err = ValidateFFNameField(ctx, ns.Name, "name"); err != nil {
		return err
	}
	if err = ValidateLength(ctx, ns.Description, "description", 4096); err != nil {
		return err
	}
	if existing {
		if ns.ID == nil {
			return i18n.NewError(ctx, i18n.MsgNilID)
		}
	}
	return nil
}

func typeNamespaceNameTopicHash(objType string, ns string, name string) string {
	// Topic generation function for ordering anything with a type, namespace and name.
	// Means all messages racing for this name will be consistently ordered by all parties.
	h := sha256.New()
	h.Write([]byte(objType))
	h.Write([]byte(ns))
	h.Write([]byte(name))
	return HashResult(h).String()
}

func (ns *Namespace) Topic() string {
	return typeNamespaceNameTopicHash("namespace", ns.Name, "")
}

func (ns *Namespace) SetBroadcastMessage(msgID *UUID) {
	ns.Message = msgID
}
