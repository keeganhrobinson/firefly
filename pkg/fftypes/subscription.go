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
	"database/sql/driver"
	"encoding/json"
	"net/url"

	"github.com/hyperledger/firefly/internal/i18n"
)

// SubscriptionFilter contains regular expressions to match against events. All must match for an event to be dispatched to a subscription
type SubscriptionFilter struct {
	Events           string                `json:"events,omitempty"`
	Message          MessageFilter         `json:"message,omitempty"`
	Transaction      TransactionFilter     `json:"transaction,omitempty"`
	BlockchainEvent  BlockchainEventFilter `json:"blockchainevent,omitempty"`
	Topic            string                `json:"topic,omitempty"`
	DeprecatedTopics string                `json:"topics,omitempty"`
	DeprecatedTag    string                `json:"tag,omitempty"`
	DeprecatedGroup  string                `json:"group,omitempty"`
	DeprecatedAuthor string                `json:"author,omitempty"`
}

func NewSubscriptionFilterFromQuery(query url.Values) SubscriptionFilter {
	return SubscriptionFilter{
		Events: query.Get("filter.events"),
		Message: MessageFilter{
			Group:  query.Get("filter.message.group"),
			Tag:    query.Get("filter.message.tag"),
			Author: query.Get("filter.message.author"),
		},
		BlockchainEvent: BlockchainEventFilter{
			Name:     query.Get("filter.blockchain.name"),
			Listener: query.Get("filter.blockchain.listener"),
		},
		Transaction: TransactionFilter{
			Type: query.Get("filter.transaction.type"),
		},
		Topic:            query.Get("filter.topic"),
		DeprecatedTag:    query.Get("filter.tag"),
		DeprecatedTopics: query.Get("filter.topics"),
		DeprecatedGroup:  query.Get("filter.group"),
		DeprecatedAuthor: query.Get("filter.author"),
	}
}

type MessageFilter struct {
	Tag    string `json:"tag,omitempty"`
	Group  string `json:"group,omitempty"`
	Author string `json:"author,omitempty"`
}

type TransactionFilter struct {
	Type string `json:"type,omitempty"`
}

type BlockchainEventFilter struct {
	Name     string `json:"name,omitempty"`
	Listener string `json:"listener,omitempty"`
}

// SubOptsFirstEvent picks the first event that should be dispatched on the subscription, and can be a string containing an exact sequence as well as one of the enum values
type SubOptsFirstEvent string

const (
	// SubOptsFirstEventOldest indicates all events should be dispatched to the subscription
	SubOptsFirstEventOldest SubOptsFirstEvent = "oldest"
	// SubOptsFirstEventNewest indicates only newly received events should be dispatched to the subscription
	SubOptsFirstEventNewest SubOptsFirstEvent = "newest"
)

// SubscriptionCoreOptions are the core options that apply across all transports
type SubscriptionCoreOptions struct {
	FirstEvent *SubOptsFirstEvent `json:"firstEvent,omitempty"`
	ReadAhead  *uint16            `json:"readAhead,omitempty"`
	WithData   *bool              `json:"withData,omitempty"`
}

// SubscriptionOptions cutomize the behavior of subscriptions
type SubscriptionOptions struct {
	SubscriptionCoreOptions

	// Ephemeral subscriptions only can add this option to enable change events
	ChangeEvents bool `json:"-"`

	// Extenisble by the specific transport - so we serialize/de-serialize via map
	additionalOptions JSONObject
}

// SubscriptionRef are the fields that can be used to refer to a subscription
type SubscriptionRef struct {
	ID        *UUID  `json:"id"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// Subscription is a binding between the stream of events within a namespace, and an event interface - such as an application listening on websockets
type Subscription struct {
	SubscriptionRef

	Transport string              `json:"transport"`
	Filter    SubscriptionFilter  `json:"filter"`
	Options   SubscriptionOptions `json:"options"`
	Ephemeral bool                `json:"ephemeral,omitempty"`
	Created   *FFTime             `json:"created"`
	Updated   *FFTime             `json:"updated"`
}

func (so *SubscriptionOptions) UnmarshalJSON(b []byte) error {
	so.additionalOptions = JSONObject{}
	err := json.Unmarshal(b, &so.additionalOptions)
	if err == nil {
		err = json.Unmarshal(b, &so.SubscriptionCoreOptions)
	}
	if err != nil {
		return err
	}
	delete(so.additionalOptions, "firstEvent")
	delete(so.additionalOptions, "readAhead")
	delete(so.additionalOptions, "withData")
	return nil
}

func (so SubscriptionOptions) MarshalJSON() ([]byte, error) {
	if so.additionalOptions == nil {
		so.additionalOptions = JSONObject{}
	}
	if so.WithData != nil {
		so.additionalOptions["withData"] = so.WithData
	}
	if so.FirstEvent != nil {
		so.additionalOptions["firstEvent"] = *so.FirstEvent
	}
	if so.ReadAhead != nil {
		so.additionalOptions["readAhead"] = float64(*so.ReadAhead)
	}
	return json.Marshal(&so.additionalOptions)
}

func (so *SubscriptionOptions) TransportOptions() JSONObject {
	if so.additionalOptions == nil {
		so.additionalOptions = JSONObject{}
	}
	return so.additionalOptions
}

// Scan implements sql.Scanner
func (so *SubscriptionOptions) Scan(src interface{}) error {
	switch src := src.(type) {
	case []byte:
		return so.UnmarshalJSON(src)
	case string:
		return so.UnmarshalJSON([]byte(src))
	default:
		return i18n.NewError(context.Background(), i18n.MsgScanFailed, src, so)
	}
}

// Value implements sql.Valuer
func (so SubscriptionOptions) Value() (driver.Value, error) {
	return so.MarshalJSON()
}

func (sf SubscriptionFilter) Value() (driver.Value, error) {
	return json.Marshal(&sf)
}

func (sf *SubscriptionFilter) Scan(src interface{}) error {
	switch src := src.(type) {
	case nil:
		return nil

	case []byte:
		return json.Unmarshal(src, &sf)

	case string:
		if src == "" {
			return nil
		}
		return json.Unmarshal([]byte(src), &sf)

	default:
		return i18n.NewError(context.Background(), i18n.MsgScanFailed, src, sf)
	}
}
