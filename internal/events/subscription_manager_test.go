// Copyright © 2021 Kaleido, Inc.
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
	"fmt"
	"testing"

	"github.com/hyperledger/firefly/internal/config"
	"github.com/hyperledger/firefly/internal/txcommon"
	"github.com/hyperledger/firefly/mocks/databasemocks"
	"github.com/hyperledger/firefly/mocks/datamocks"
	"github.com/hyperledger/firefly/mocks/definitionsmocks"
	"github.com/hyperledger/firefly/mocks/eventsmocks"
	"github.com/hyperledger/firefly/pkg/events"
	"github.com/hyperledger/firefly/pkg/fftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func newTestSubManager(t *testing.T, mei *eventsmocks.PluginAll) (*subscriptionManager, func()) {
	config.Reset()
	config.Set(config.EventTransportsEnabled, []string{})

	mdi := &databasemocks.Plugin{}
	mdm := &datamocks.Manager{}
	msh := &definitionsmocks.DefinitionHandlers{}
	txHelper := txcommon.NewTransactionHelper(mdi, mdm)

	ctx, cancel := context.WithCancel(context.Background())
	mei.On("Name").Return("ut")
	mei.On("Capabilities").Return(&events.Capabilities{}).Maybe()
	mei.On("InitPrefix", mock.Anything).Return()
	mei.On("Init", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mdi.On("GetEvents", mock.Anything, mock.Anything, mock.Anything).Return([]*fftypes.Event{}, nil, nil).Maybe()
	mdi.On("GetOffset", mock.Anything, mock.Anything, mock.Anything).Return(&fftypes.Offset{RowID: 3333333, Current: 0}, nil).Maybe()
	sm, err := newSubscriptionManager(ctx, mdi, mdm, newEventNotifier(ctx, "ut"), msh, txHelper)
	assert.NoError(t, err)
	sm.transports = map[string]events.Plugin{
		"ut": mei,
	}
	return sm, cancel
}

func TestRegisterDurableSubscriptions(t *testing.T) {

	sub1 := fftypes.NewUUID()
	sub2 := fftypes.NewUUID()

	// Set some existing ones to be cleaned out
	testED1, cancel1 := newTestEventDispatcher(&subscription{definition: &fftypes.Subscription{SubscriptionRef: fftypes.SubscriptionRef{ID: sub1}}})
	testED1.start()
	defer cancel1()

	mei := testED1.transport.(*eventsmocks.PluginAll)
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()

	mdi := sm.database.(*databasemocks.Plugin)
	mdi.On("GetSubscriptions", mock.Anything, mock.Anything).Return([]*fftypes.Subscription{
		{SubscriptionRef: fftypes.SubscriptionRef{
			ID: sub1,
		}, Transport: "ut"},
		{SubscriptionRef: fftypes.SubscriptionRef{
			ID: sub2,
		}, Transport: "ut"},
	}, nil, nil)
	mei.On("ValidateOptions", mock.Anything).Return(nil)
	err := sm.start()
	assert.NoError(t, err)

	sm.connections["conn1"] = &connection{
		ei:        mei,
		id:        "conn1",
		transport: "ut",
		dispatchers: map[fftypes.UUID]*eventDispatcher{
			*sub1: testED1,
		},
	}
	be := &boundCallbacks{sm: sm, ei: mei}

	be.RegisterConnection("conn1", func(sr fftypes.SubscriptionRef) bool {
		return *sr.ID == *sub2
	})
	be.RegisterConnection("conn2", func(sr fftypes.SubscriptionRef) bool {
		return *sr.ID == *sub1
	})

	assert.Equal(t, 1, len(sm.connections["conn1"].dispatchers))
	assert.Equal(t, *sub2, *sm.connections["conn1"].dispatchers[*sub2].subscription.definition.ID)
	assert.Equal(t, 1, len(sm.connections["conn2"].dispatchers))
	assert.Equal(t, *sub1, *sm.connections["conn2"].dispatchers[*sub1].subscription.definition.ID)

	// Close with active conns
	sm.close()
	assert.Nil(t, sm.connections["conn1"])
	assert.Nil(t, sm.connections["conn2"])
}

func TestRegisterEphemeralSubscriptions(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mdi := sm.database.(*databasemocks.Plugin)

	mdi.On("GetSubscriptions", mock.Anything, mock.Anything).Return([]*fftypes.Subscription{}, nil, nil)
	mei.On("ValidateOptions", mock.Anything).Return(nil)

	err := sm.start()
	assert.NoError(t, err)
	be := &boundCallbacks{sm: sm, ei: mei}

	// check with filter
	err = be.EphemeralSubscription("conn1", "ns1", &fftypes.SubscriptionFilter{Message: fftypes.MessageFilter{Author: "flapflip"}}, &fftypes.SubscriptionOptions{})
	assert.NoError(t, err)

	assert.Equal(t, 1, len(sm.connections["conn1"].dispatchers))
	for _, d := range sm.connections["conn1"].dispatchers {
		assert.True(t, d.subscription.definition.Ephemeral)
	}

	be.ConnnectionClosed("conn1")
	assert.Nil(t, sm.connections["conn1"])
	// Check we swallow dup closes without errors
	be.ConnnectionClosed("conn1")
	assert.Nil(t, sm.connections["conn1"])
}

func TestRegisterEphemeralSubscriptionsFail(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mdi := sm.database.(*databasemocks.Plugin)

	mdi.On("GetSubscriptions", mock.Anything, mock.Anything).Return([]*fftypes.Subscription{}, nil, nil)
	mei.On("ValidateOptions", mock.Anything).Return(nil)
	err := sm.start()
	assert.NoError(t, err)
	be := &boundCallbacks{sm: sm, ei: mei}

	err = be.EphemeralSubscription("conn1", "ns1", &fftypes.SubscriptionFilter{
		Message: fftypes.MessageFilter{
			Author: "[[[[[ !wrong",
		},
	}, &fftypes.SubscriptionOptions{})
	assert.Regexp(t, "FF10171", err)
	assert.Empty(t, sm.connections["conn1"].dispatchers)

}

func TestSubManagerBadPlugin(t *testing.T) {
	mdi := &databasemocks.Plugin{}
	mdm := &datamocks.Manager{}
	txHelper := txcommon.NewTransactionHelper(mdi, mdm)
	config.Reset()
	config.Set(config.EventTransportsEnabled, []string{"!unknown!"})
	_, err := newSubscriptionManager(context.Background(), mdi, mdm, newEventNotifier(context.Background(), "ut"), nil, txHelper)
	assert.Regexp(t, "FF10172", err)
}

func TestSubManagerTransportInitError(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	mei.On("Name").Return("ut")
	mei.On("InitPrefix", mock.Anything).Return()
	mei.On("Init", mock.Anything, mock.Anything, mock.Anything).Return(fmt.Errorf("pop"))

	sm, cancel := newTestSubManager(t, mei)
	defer cancel()

	err := sm.initTransports()
	assert.EqualError(t, err, "pop")
}

func TestStartSubRestoreFail(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mdi := sm.database.(*databasemocks.Plugin)

	mdi.On("GetSubscriptions", mock.Anything, mock.Anything).Return(nil, nil, fmt.Errorf("pop"))
	err := sm.start()
	assert.EqualError(t, err, "pop")
}

func TestStartSubRestoreOkSubsFail(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mdi := sm.database.(*databasemocks.Plugin)

	mdi.On("GetSubscriptions", mock.Anything, mock.Anything).Return([]*fftypes.Subscription{
		{SubscriptionRef: fftypes.SubscriptionRef{
			ID: fftypes.NewUUID(),
		},
			Filter: fftypes.SubscriptionFilter{
				Events: "[[[[[[not a regex",
			}},
	}, nil, nil)
	err := sm.start()
	assert.NoError(t, err) // swallowed and startup continues
}

func TestStartSubRestoreOkSubsOK(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mdi := sm.database.(*databasemocks.Plugin)

	mdi.On("GetSubscriptions", mock.Anything, mock.Anything).Return([]*fftypes.Subscription{
		{SubscriptionRef: fftypes.SubscriptionRef{
			ID: fftypes.NewUUID(),
		},
			Filter: fftypes.SubscriptionFilter{
				Topic:  ".*",
				Events: ".*",
				Message: fftypes.MessageFilter{
					Tag:    ".*",
					Group:  ".*",
					Author: ".*",
				},
				Transaction: fftypes.TransactionFilter{
					Type: ".*",
				},
				BlockchainEvent: fftypes.BlockchainEventFilter{
					Name: ".*",
				},
			}},
	}, nil, nil)
	err := sm.start()
	assert.NoError(t, err) // swallowed and startup continues
}

func TestCreateSubscriptionBadTransport(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	_, err := sm.parseSubscriptionDef(sm.ctx, &fftypes.Subscription{})
	assert.Regexp(t, "FF1017", err)
}

func TestCreateSubscriptionBadTransportOptions(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	sub := &fftypes.Subscription{
		Transport: "ut",
		Options:   fftypes.SubscriptionOptions{},
	}
	sub.Options.TransportOptions()["myoption"] = "badvalue"
	mei.On("ValidateOptions", mock.MatchedBy(func(opts *fftypes.SubscriptionOptions) bool {
		return opts.TransportOptions()["myoption"] == "badvalue"
	})).Return(fmt.Errorf("pop"))
	_, err := sm.parseSubscriptionDef(sm.ctx, sub)
	assert.Regexp(t, "pop", err)
}

func TestCreateSubscriptionBadEventilter(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mei.On("ValidateOptions", mock.Anything).Return(nil)
	_, err := sm.parseSubscriptionDef(sm.ctx, &fftypes.Subscription{
		Filter: fftypes.SubscriptionFilter{
			Events: "[[[[! badness",
		},
		Transport: "ut",
	})
	assert.Regexp(t, "FF10171.*events", err)
}

func TestCreateSubscriptionBadTopicFilter(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mei.On("ValidateOptions", mock.Anything).Return(nil)
	_, err := sm.parseSubscriptionDef(sm.ctx, &fftypes.Subscription{
		Filter: fftypes.SubscriptionFilter{
			Topic: "[[[[! badness",
		},
		Transport: "ut",
	})
	assert.Regexp(t, "FF10171.*topic", err)
}

func TestCreateSubscriptionBadGroupFilter(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mei.On("ValidateOptions", mock.Anything).Return(nil)
	_, err := sm.parseSubscriptionDef(sm.ctx, &fftypes.Subscription{
		Filter: fftypes.SubscriptionFilter{
			Message: fftypes.MessageFilter{
				Group: "[[[[! badness",
			},
		},
		Transport: "ut",
	})
	assert.Regexp(t, "FF10171.*group", err)
}

func TestCreateSubscriptionBadAuthorFilter(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mei.On("ValidateOptions", mock.Anything).Return(nil)
	_, err := sm.parseSubscriptionDef(sm.ctx, &fftypes.Subscription{
		Filter: fftypes.SubscriptionFilter{
			Message: fftypes.MessageFilter{
				Author: "[[[[! badness",
			},
		},
		Transport: "ut",
	})
	assert.Regexp(t, "FF10171.*author", err)
}

func TestCreateSubscriptionBadTxTypeFilter(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mei.On("ValidateOptions", mock.Anything).Return(nil)
	_, err := sm.parseSubscriptionDef(sm.ctx, &fftypes.Subscription{
		Filter: fftypes.SubscriptionFilter{
			Transaction: fftypes.TransactionFilter{
				Type: "[[[[! badness",
			},
		},
		Transport: "ut",
	})
	assert.Regexp(t, "FF10171.*type", err)
}

func TestCreateSubscriptionBadBlockchainEventNameFilter(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mei.On("ValidateOptions", mock.Anything).Return(nil)
	_, err := sm.parseSubscriptionDef(sm.ctx, &fftypes.Subscription{
		Filter: fftypes.SubscriptionFilter{
			BlockchainEvent: fftypes.BlockchainEventFilter{
				Name: "[[[[! badness",
			},
		},
		Transport: "ut",
	})
	assert.Regexp(t, "FF10171.*name", err)
}

func TestCreateSubscriptionBadDeprecatedGroupFilter(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mei.On("ValidateOptions", mock.Anything).Return(nil)
	_, err := sm.parseSubscriptionDef(sm.ctx, &fftypes.Subscription{
		Filter: fftypes.SubscriptionFilter{
			DeprecatedGroup: "[[[[! badness",
		},
		Transport: "ut",
	})
	assert.Regexp(t, "FF10171.*group", err)
}

func TestCreateSubscriptionBadDeprecatedTagFilter(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mei.On("ValidateOptions", mock.Anything).Return(nil)
	_, err := sm.parseSubscriptionDef(sm.ctx, &fftypes.Subscription{
		Filter: fftypes.SubscriptionFilter{
			DeprecatedTag: "[[[[! badness",
		},
		Transport: "ut",
	})
	assert.Regexp(t, "FF10171.*tag", err)
}

func TestCreateSubscriptionBadMessageTagFilter(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mei.On("ValidateOptions", mock.Anything).Return(nil)
	_, err := sm.parseSubscriptionDef(sm.ctx, &fftypes.Subscription{
		Filter: fftypes.SubscriptionFilter{
			Message: fftypes.MessageFilter{
				Tag: "[[[[! badness",
			},
		},
		Transport: "ut",
	})
	assert.Regexp(t, "FF10171.*message.tag", err)
}

func TestCreateSubscriptionBadDeprecatedAuthorFilter(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mei.On("ValidateOptions", mock.Anything).Return(nil)
	_, err := sm.parseSubscriptionDef(sm.ctx, &fftypes.Subscription{
		Filter: fftypes.SubscriptionFilter{
			DeprecatedAuthor: "[[[[! badness",
		},
		Transport: "ut",
	})
	assert.Regexp(t, "FF10171.*author", err)
}

func TestCreateSubscriptionBadDeprecatedTopicsFilter(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mei.On("ValidateOptions", mock.Anything).Return(nil)
	_, err := sm.parseSubscriptionDef(sm.ctx, &fftypes.Subscription{
		Filter: fftypes.SubscriptionFilter{
			DeprecatedTopics: "[[[[! badness",
		},
		Transport: "ut",
	})
	assert.Regexp(t, "FF10171.*topics", err)
}

func TestCreateSubscriptionBadBlockchainEventListenerFilter(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mei.On("ValidateOptions", mock.Anything).Return(nil)
	_, err := sm.parseSubscriptionDef(sm.ctx, &fftypes.Subscription{
		Filter: fftypes.SubscriptionFilter{
			BlockchainEvent: fftypes.BlockchainEventFilter{
				Listener: "[[[[! badness",
			},
		},
		Transport: "ut",
	})
	assert.Regexp(t, "FF10171.*listener", err)
}

func TestCreateSubscriptionSuccessMessageFilter(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mei.On("ValidateOptions", mock.Anything).Return(nil)
	_, err := sm.parseSubscriptionDef(sm.ctx, &fftypes.Subscription{
		Filter: fftypes.SubscriptionFilter{
			Message: fftypes.MessageFilter{
				Author: "flapflip",
			},
		},
		Transport: "ut",
	})
	assert.NoError(t, err)
}

func TestCreateSubscriptionSuccessTxFilter(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mei.On("ValidateOptions", mock.Anything).Return(nil)
	_, err := sm.parseSubscriptionDef(sm.ctx, &fftypes.Subscription{
		Filter: fftypes.SubscriptionFilter{
			Transaction: fftypes.TransactionFilter{
				Type: "flapflip",
			},
		},
		Transport: "ut",
	})
	assert.NoError(t, err)
}

func TestCreateSubscriptionSuccessBlockchainEvent(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mei.On("ValidateOptions", mock.Anything).Return(nil)
	_, err := sm.parseSubscriptionDef(sm.ctx, &fftypes.Subscription{
		Filter: fftypes.SubscriptionFilter{
			BlockchainEvent: fftypes.BlockchainEventFilter{
				Name: "flapflip",
			},
		},
		Transport: "ut",
	})
	assert.NoError(t, err)
}

func TestCreateSubscriptionWithDeprecatedFilters(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mei.On("ValidateOptions", mock.Anything).Return(nil)
	_, err := sm.parseSubscriptionDef(sm.ctx, &fftypes.Subscription{
		Filter: fftypes.SubscriptionFilter{
			Topic:            "flop",
			DeprecatedTopics: "test",
			DeprecatedTag:    "flap",
			DeprecatedAuthor: "flip",
			DeprecatedGroup:  "flapflip",
		},
		Transport: "ut",
	})
	assert.NoError(t, err)

}

func TestDispatchDeliveryResponseOK(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mdi := sm.database.(*databasemocks.Plugin)
	mdi.On("GetSubscriptions", mock.Anything, mock.Anything).Return([]*fftypes.Subscription{}, nil, nil)
	mei.On("ValidateOptions", mock.Anything).Return(nil)
	err := sm.start()
	assert.NoError(t, err)
	be := &boundCallbacks{sm: sm, ei: mei}

	err = be.EphemeralSubscription("conn1", "ns1", &fftypes.SubscriptionFilter{}, &fftypes.SubscriptionOptions{})
	assert.NoError(t, err)

	assert.Equal(t, 1, len(sm.connections["conn1"].dispatchers))
	var subID *fftypes.UUID
	for _, d := range sm.connections["conn1"].dispatchers {
		assert.True(t, d.subscription.definition.Ephemeral)
		subID = d.subscription.definition.ID
	}

	be.DeliveryResponse("conn1", &fftypes.EventDeliveryResponse{
		ID: fftypes.NewUUID(), // Won't be in-flight, but that's fine
		Subscription: fftypes.SubscriptionRef{
			ID: subID,
		},
	})
	mdi.AssertExpectations(t)
}

func TestDispatchDeliveryResponseInvalidSubscription(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mdi := sm.database.(*databasemocks.Plugin)
	mdi.On("GetSubscriptions", mock.Anything, mock.Anything).Return([]*fftypes.Subscription{}, nil, nil)
	err := sm.start()
	assert.NoError(t, err)
	be := &boundCallbacks{sm: sm, ei: mei}

	be.DeliveryResponse("conn1", &fftypes.EventDeliveryResponse{
		ID: fftypes.NewUUID(),
		Subscription: fftypes.SubscriptionRef{
			ID: fftypes.NewUUID(),
		},
	})
	mdi.AssertExpectations(t)
}

func TestConnIDSafetyChecking(t *testing.T) {
	mei1 := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei1)
	defer cancel()
	mei2 := &eventsmocks.PluginAll{}
	mei2.On("Name").Return("ut2")
	be2 := &boundCallbacks{sm: sm, ei: mei2}

	sm.connections["conn1"] = &connection{
		ei:          mei1,
		id:          "conn1",
		transport:   "ut",
		dispatchers: map[fftypes.UUID]*eventDispatcher{},
	}

	err := be2.RegisterConnection("conn1", func(sr fftypes.SubscriptionRef) bool { return true })
	assert.Regexp(t, "FF10190", err)

	err = be2.EphemeralSubscription("conn1", "ns1", &fftypes.SubscriptionFilter{}, &fftypes.SubscriptionOptions{})
	assert.Regexp(t, "FF10190", err)

	be2.DeliveryResponse("conn1", &fftypes.EventDeliveryResponse{})

	be2.ConnnectionClosed("conn1")

	assert.NotNil(t, sm.connections["conn1"])

}

func TestNewDurableSubscriptionBadSub(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mdi := sm.database.(*databasemocks.Plugin)

	subID := fftypes.NewUUID()
	mdi.On("GetSubscriptionByID", mock.Anything, subID).Return(&fftypes.Subscription{
		Filter: fftypes.SubscriptionFilter{
			Events: "![[[[badness",
		},
	}, nil)
	sm.newOrUpdatedDurableSubscription(subID)

	assert.Empty(t, sm.durableSubs)
}

func TestNewDurableSubscriptionUnknownTransport(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mdi := sm.database.(*databasemocks.Plugin)

	sm.connections["conn1"] = &connection{
		ei:        mei,
		id:        "conn1",
		transport: "ut",
		matcher: func(sr fftypes.SubscriptionRef) bool {
			return sr.Namespace == "ns1" && sr.Name == "sub1"
		},
		dispatchers: map[fftypes.UUID]*eventDispatcher{},
	}

	subID := fftypes.NewUUID()
	mdi.On("GetSubscriptionByID", mock.Anything, subID).Return(&fftypes.Subscription{
		SubscriptionRef: fftypes.SubscriptionRef{
			ID:        subID,
			Namespace: "ns1",
			Name:      "sub1",
		},
		Transport: "unknown",
	}, nil)
	sm.newOrUpdatedDurableSubscription(subID)

	assert.Empty(t, sm.connections["conn1"].dispatchers)
	assert.Empty(t, sm.durableSubs)
}

func TestNewDurableSubscriptionOK(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mdi := sm.database.(*databasemocks.Plugin)
	mei.On("ValidateOptions", mock.Anything).Return(nil)

	sm.connections["conn1"] = &connection{
		ei:        mei,
		id:        "conn1",
		transport: "ut",
		matcher: func(sr fftypes.SubscriptionRef) bool {
			return sr.Namespace == "ns1" && sr.Name == "sub1"
		},
		dispatchers: map[fftypes.UUID]*eventDispatcher{},
	}

	subID := fftypes.NewUUID()
	mdi.On("GetSubscriptionByID", mock.Anything, subID).Return(&fftypes.Subscription{
		SubscriptionRef: fftypes.SubscriptionRef{
			ID:        subID,
			Namespace: "ns1",
			Name:      "sub1",
		},
		Transport: "ut",
	}, nil)
	sm.newOrUpdatedDurableSubscription(subID)

	assert.NotEmpty(t, sm.connections["conn1"].dispatchers)
	assert.NotEmpty(t, sm.durableSubs)
}

func TestUpdatedDurableSubscriptionNoOp(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mdi := sm.database.(*databasemocks.Plugin)
	mei.On("ValidateOptions", mock.Anything).Return(nil)

	subID := fftypes.NewUUID()
	sub := &fftypes.Subscription{
		SubscriptionRef: fftypes.SubscriptionRef{
			ID:        subID,
			Namespace: "ns1",
			Name:      "sub1",
		},
		Transport: "ut",
	}
	s := &subscription{
		definition: sub,
	}
	sm.durableSubs[*subID] = s

	ed, cancelEd := newTestEventDispatcher(s)
	defer cancelEd()
	sm.connections["conn1"] = &connection{
		ei:        mei,
		id:        "conn1",
		transport: "ut",
		matcher: func(sr fftypes.SubscriptionRef) bool {
			return sr.Namespace == "ns1" && sr.Name == "sub1"
		},
		dispatchers: map[fftypes.UUID]*eventDispatcher{
			*subID: ed,
		},
	}

	mdi.On("GetSubscriptionByID", mock.Anything, subID).Return(sub, nil)
	sm.newOrUpdatedDurableSubscription(subID)

	assert.Equal(t, ed, sm.connections["conn1"].dispatchers[*subID])
	assert.Equal(t, s, sm.durableSubs[*subID])
}

func TestUpdatedDurableSubscriptionOK(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mdi := sm.database.(*databasemocks.Plugin)
	mei.On("ValidateOptions", mock.Anything).Return(nil)

	subID := fftypes.NewUUID()
	sub := &fftypes.Subscription{
		SubscriptionRef: fftypes.SubscriptionRef{
			ID:        subID,
			Namespace: "ns1",
			Name:      "sub1",
		},
		Transport: "ut",
	}
	sub2 := *sub
	sub2.Updated = fftypes.Now()
	s := &subscription{
		definition: sub,
	}
	sm.durableSubs[*subID] = s

	ed, cancelEd := newTestEventDispatcher(s)
	cancelEd()
	close(ed.closed)
	sm.connections["conn1"] = &connection{
		ei:        mei,
		id:        "conn1",
		transport: "ut",
		matcher: func(sr fftypes.SubscriptionRef) bool {
			return sr.Namespace == "ns1" && sr.Name == "sub1"
		},
		dispatchers: map[fftypes.UUID]*eventDispatcher{
			*subID: ed,
		},
	}

	mdi.On("GetSubscriptionByID", mock.Anything, subID).Return(&sub2, nil)
	sm.newOrUpdatedDurableSubscription(subID)

	assert.NotEqual(t, ed, sm.connections["conn1"].dispatchers[*subID])
	assert.NotEqual(t, s, sm.durableSubs[*subID])
	assert.NotEmpty(t, sm.connections["conn1"].dispatchers)
	assert.NotEmpty(t, sm.durableSubs)
}

func TestMatchedSubscriptionWithLockUnknownTransport(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()

	conn := &connection{
		matcher: func(sr fftypes.SubscriptionRef) bool { return true },
	}
	sm.matchSubToConnLocked(conn, &subscription{definition: &fftypes.Subscription{Transport: "Wrong!"}})
	assert.Nil(t, conn.dispatchers)
}

func TestMatchedSubscriptionWithBadMatcherRegisteredt(t *testing.T) {
	mei := &eventsmocks.PluginAll{}
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()

	conn := &connection{}
	sm.matchSubToConnLocked(conn, &subscription{definition: &fftypes.Subscription{Transport: "Wrong!"}})
	assert.Nil(t, conn.dispatchers)
}

func TestDeleteDurableSubscriptionOk(t *testing.T) {
	subID := fftypes.NewUUID()
	subDef := &fftypes.Subscription{
		SubscriptionRef: fftypes.SubscriptionRef{
			ID:        subID,
			Namespace: "ns1",
			Name:      "sub1",
		},
		Transport: "websockets",
	}
	sub := &subscription{
		definition: subDef,
	}
	testED1, _ := newTestEventDispatcher(sub)

	mei := testED1.transport.(*eventsmocks.PluginAll)
	sm, cancel := newTestSubManager(t, mei)
	defer cancel()
	mdi := sm.database.(*databasemocks.Plugin)

	sm.durableSubs[*subID] = sub
	ed, _ := newTestEventDispatcher(sub)
	ed.database = mdi
	ed.start()
	sm.connections["conn1"] = &connection{
		ei:        mei,
		id:        "conn1",
		transport: "ut",
		matcher: func(sr fftypes.SubscriptionRef) bool {
			return sr.Namespace == "ns1" && sr.Name == "sub1"
		},
		dispatchers: map[fftypes.UUID]*eventDispatcher{
			*subID: ed,
		},
	}

	mdi.On("GetSubscriptionByID", mock.Anything, subID).Return(subDef, nil)
	mdi.On("DeleteOffset", mock.Anything, fftypes.FFEnum("subscription"), subID.String()).Return(fmt.Errorf("this error is logged and swallowed"))
	sm.deletedDurableSubscription(subID)

	assert.Empty(t, sm.connections["conn1"].dispatchers)
	assert.Empty(t, sm.durableSubs)
	<-ed.closed
}
