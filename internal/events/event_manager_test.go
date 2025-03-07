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
	"github.com/hyperledger/firefly/internal/events/system"
	"github.com/hyperledger/firefly/internal/txcommon"
	"github.com/hyperledger/firefly/mocks/assetmocks"
	"github.com/hyperledger/firefly/mocks/blockchainmocks"
	"github.com/hyperledger/firefly/mocks/broadcastmocks"
	"github.com/hyperledger/firefly/mocks/databasemocks"
	"github.com/hyperledger/firefly/mocks/datamocks"
	"github.com/hyperledger/firefly/mocks/definitionsmocks"
	"github.com/hyperledger/firefly/mocks/eventsmocks"
	"github.com/hyperledger/firefly/mocks/identitymanagermocks"
	"github.com/hyperledger/firefly/mocks/metricsmocks"
	"github.com/hyperledger/firefly/mocks/privatemessagingmocks"
	"github.com/hyperledger/firefly/mocks/shareddownloadmocks"
	"github.com/hyperledger/firefly/mocks/sharedstoragemocks"
	"github.com/hyperledger/firefly/mocks/sysmessagingmocks"
	"github.com/hyperledger/firefly/mocks/txcommonmocks"
	"github.com/hyperledger/firefly/pkg/fftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

var testNodeID = fftypes.NewUUID()

func newTestEventManager(t *testing.T) (*eventManager, func()) {
	return newTestEventManagerCommon(t, false)
}

func newTestEventManagerWithMetrics(t *testing.T) (*eventManager, func()) {
	return newTestEventManagerCommon(t, true)
}

func newTestEventManagerCommon(t *testing.T, metrics bool) (*eventManager, func()) {
	config.Reset()
	ctx, cancel := context.WithCancel(context.Background())
	mdi := &databasemocks.Plugin{}
	mbi := &blockchainmocks.Plugin{}
	mim := &identitymanagermocks.Manager{}
	mpi := &sharedstoragemocks.Plugin{}
	met := &eventsmocks.Plugin{}
	mdm := &datamocks.Manager{}
	msh := &definitionsmocks.DefinitionHandlers{}
	mbm := &broadcastmocks.Manager{}
	mpm := &privatemessagingmocks.Manager{}
	mam := &assetmocks.Manager{}
	mni := &sysmessagingmocks.LocalNodeInfo{}
	mdd := &shareddownloadmocks.Manager{}
	mmi := &metricsmocks.Manager{}
	txHelper := txcommon.NewTransactionHelper(mdi, mdm)
	mmi.On("IsMetricsEnabled").Return(metrics)
	if metrics {
		mmi.On("TransferConfirmed", mock.Anything)
	}
	mni.On("GetNodeUUID", mock.Anything).Return(testNodeID).Maybe()
	met.On("Name").Return("ut").Maybe()
	mbi.On("VerifierType").Return(fftypes.VerifierTypeEthAddress).Maybe()
	emi, err := NewEventManager(ctx, mni, mpi, mdi, mbi, mim, msh, mdm, mbm, mpm, mam, mdd, mmi, txHelper)
	em := emi.(*eventManager)
	em.txHelper = &txcommonmocks.Helper{}
	rag := mdi.On("RunAsGroup", em.ctx, mock.Anything).Maybe()
	rag.RunFn = func(a mock.Arguments) {
		rag.ReturnArguments = mock.Arguments{a[1].(func(context.Context) error)(a[0].(context.Context))}
	}
	assert.NoError(t, err)
	return em, cancel
}

func TestStartStop(t *testing.T) {
	em, cancel := newTestEventManager(t)
	mdi := em.database.(*databasemocks.Plugin)
	mdi.On("GetOffset", mock.Anything, fftypes.OffsetTypeAggregator, aggregatorOffsetName).Return(&fftypes.Offset{
		Type:    fftypes.OffsetTypeAggregator,
		Name:    aggregatorOffsetName,
		Current: 12345,
		RowID:   333333,
	}, nil)
	mdi.On("GetPins", mock.Anything, mock.Anything, mock.Anything).Return([]*fftypes.Pin{}, nil, nil)
	mdi.On("GetSubscriptions", mock.Anything, mock.Anything, mock.Anything).Return([]*fftypes.Subscription{}, nil, nil)
	assert.NoError(t, em.Start())
	em.NewEvents() <- 12345
	em.NewPins() <- 12345
	assert.Equal(t, chan<- *fftypes.ChangeEvent(em.subManager.cel.changeEvents), em.ChangeEvents())
	cancel()
	em.WaitStop()
}

func TestStartStopBadDependencies(t *testing.T) {
	_, err := NewEventManager(context.Background(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	assert.Regexp(t, "FF10128", err)

}

func TestStartStopBadTransports(t *testing.T) {
	config.Set(config.EventTransportsEnabled, []string{"wrongun"})
	defer config.Reset()
	mdi := &databasemocks.Plugin{}
	mbi := &blockchainmocks.Plugin{}
	mim := &identitymanagermocks.Manager{}
	mpi := &sharedstoragemocks.Plugin{}
	mdm := &datamocks.Manager{}
	msh := &definitionsmocks.DefinitionHandlers{}
	mbm := &broadcastmocks.Manager{}
	mpm := &privatemessagingmocks.Manager{}
	mni := &sysmessagingmocks.LocalNodeInfo{}
	mam := &assetmocks.Manager{}
	msd := &shareddownloadmocks.Manager{}
	mm := &metricsmocks.Manager{}
	txHelper := txcommon.NewTransactionHelper(mdi, mdm)
	mbi.On("VerifierType").Return(fftypes.VerifierTypeEthAddress)
	_, err := NewEventManager(context.Background(), mni, mpi, mdi, mbi, mim, msh, mdm, mbm, mpm, mam, msd, mm, txHelper)
	assert.Regexp(t, "FF10172", err)
}

func TestEmitSubscriptionEventsNoops(t *testing.T) {
	em, cancel := newTestEventManager(t)
	mdi := em.database.(*databasemocks.Plugin)
	mdi.On("GetOffset", mock.Anything, fftypes.OffsetTypeAggregator, aggregatorOffsetName).Return(&fftypes.Offset{
		Type:    fftypes.OffsetTypeAggregator,
		Name:    aggregatorOffsetName,
		Current: 12345,
		RowID:   333333,
	}, nil)
	mdi.On("GetPins", mock.Anything, mock.Anything, mock.Anything).Return([]*fftypes.Pin{}, nil, nil)
	mdi.On("GetSubscriptions", mock.Anything, mock.Anything, mock.Anything).Return([]*fftypes.Subscription{}, nil, nil)

	getSubCallReady := make(chan bool, 1)
	getSubCalled := make(chan bool)
	getSub := mdi.On("GetSubscriptionByID", mock.Anything, mock.Anything).Return(nil, nil)
	getSub.RunFn = func(a mock.Arguments) {
		<-getSubCallReady
		getSubCalled <- true
	}

	delOffsetCalled := make(chan bool)
	delOffsetMock := mdi.On("DeleteOffset", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	delOffsetMock.RunFn = func(a mock.Arguments) {
		delOffsetCalled <- true
	}

	assert.NoError(t, em.Start())
	defer cancel()

	// Wait until the gets occur for these events, which will return nil
	getSubCallReady <- true
	em.NewSubscriptions() <- fftypes.NewUUID()
	em.SubscriptionUpdates() <- fftypes.NewUUID()
	<-getSubCalled

	em.DeletedSubscriptions() <- fftypes.NewUUID()
	close(getSubCallReady)
	<-delOffsetCalled
}

func TestCreateDurableSubscriptionBadSub(t *testing.T) {
	em, cancel := newTestEventManager(t)
	defer cancel()
	err := em.CreateUpdateDurableSubscription(em.ctx, &fftypes.Subscription{}, false)
	assert.Regexp(t, "FF10189", err)
}

func TestCreateDurableSubscriptionDupName(t *testing.T) {
	em, cancel := newTestEventManager(t)
	defer cancel()
	mdi := em.database.(*databasemocks.Plugin)
	sub := &fftypes.Subscription{
		SubscriptionRef: fftypes.SubscriptionRef{
			ID:        fftypes.NewUUID(),
			Namespace: "ns1",
			Name:      "sub1",
		},
	}
	mdi.On("GetSubscriptionByName", mock.Anything, "ns1", "sub1").Return(sub, nil)
	err := em.CreateUpdateDurableSubscription(em.ctx, sub, true)
	assert.Regexp(t, "FF10193", err)
}

func TestCreateDurableSubscriptionDefaultSubCannotParse(t *testing.T) {
	em, cancel := newTestEventManager(t)
	defer cancel()
	mdi := em.database.(*databasemocks.Plugin)
	sub := &fftypes.Subscription{
		SubscriptionRef: fftypes.SubscriptionRef{
			ID:        fftypes.NewUUID(),
			Namespace: "ns1",
			Name:      "sub1",
		},
		Filter: fftypes.SubscriptionFilter{
			Events: "![[[[[",
		},
	}
	mdi.On("GetSubscriptionByName", mock.Anything, "ns1", "sub1").Return(nil, nil)
	err := em.CreateUpdateDurableSubscription(em.ctx, sub, true)
	assert.Regexp(t, "FF10171", err)
}

func TestCreateDurableSubscriptionBadFirstEvent(t *testing.T) {
	em, cancel := newTestEventManager(t)
	defer cancel()
	mdi := em.database.(*databasemocks.Plugin)
	wrongFirstEvent := fftypes.SubOptsFirstEvent("lobster")
	sub := &fftypes.Subscription{
		SubscriptionRef: fftypes.SubscriptionRef{
			ID:        fftypes.NewUUID(),
			Namespace: "ns1",
			Name:      "sub1",
		},
		Options: fftypes.SubscriptionOptions{
			SubscriptionCoreOptions: fftypes.SubscriptionCoreOptions{
				FirstEvent: &wrongFirstEvent,
			},
		},
	}
	mdi.On("GetSubscriptionByName", mock.Anything, "ns1", "sub1").Return(nil, nil)
	err := em.CreateUpdateDurableSubscription(em.ctx, sub, true)
	assert.Regexp(t, "FF10191", err)
}

func TestCreateDurableSubscriptionNegativeFirstEvent(t *testing.T) {
	em, cancel := newTestEventManager(t)
	defer cancel()
	wrongFirstEvent := fftypes.SubOptsFirstEvent("-12345")
	mdi := em.database.(*databasemocks.Plugin)
	sub := &fftypes.Subscription{
		SubscriptionRef: fftypes.SubscriptionRef{
			ID:        fftypes.NewUUID(),
			Namespace: "ns1",
			Name:      "sub1",
		},
		Options: fftypes.SubscriptionOptions{
			SubscriptionCoreOptions: fftypes.SubscriptionCoreOptions{
				FirstEvent: &wrongFirstEvent,
			},
		},
	}
	mdi.On("GetSubscriptionByName", mock.Anything, "ns1", "sub1").Return(nil, nil)
	err := em.CreateUpdateDurableSubscription(em.ctx, sub, true)
	assert.Regexp(t, "FF10192", err)
}

func TestCreateDurableSubscriptionGetHighestSequenceFailure(t *testing.T) {
	em, cancel := newTestEventManager(t)
	defer cancel()
	mdi := em.database.(*databasemocks.Plugin)
	sub := &fftypes.Subscription{
		SubscriptionRef: fftypes.SubscriptionRef{
			ID:        fftypes.NewUUID(),
			Namespace: "ns1",
			Name:      "sub1",
		},
	}
	mdi.On("GetSubscriptionByName", mock.Anything, "ns1", "sub1").Return(nil, nil)
	mdi.On("GetEvents", mock.Anything, mock.Anything, mock.Anything).Return(nil, nil, fmt.Errorf("pop"))
	err := em.CreateUpdateDurableSubscription(em.ctx, sub, true)
	assert.EqualError(t, err, "pop")
}

func TestCreateDurableSubscriptionOk(t *testing.T) {
	em, cancel := newTestEventManager(t)
	defer cancel()
	mdi := em.database.(*databasemocks.Plugin)
	sub := &fftypes.Subscription{
		SubscriptionRef: fftypes.SubscriptionRef{
			ID:        fftypes.NewUUID(),
			Namespace: "ns1",
			Name:      "sub1",
		},
	}
	mdi.On("GetSubscriptionByName", mock.Anything, "ns1", "sub1").Return(nil, nil)
	mdi.On("GetEvents", mock.Anything, mock.Anything).Return([]*fftypes.Event{
		{Sequence: 12345},
	}, nil, nil)
	mdi.On("UpsertSubscription", mock.Anything, mock.Anything, false).Return(nil)
	err := em.CreateUpdateDurableSubscription(em.ctx, sub, true)
	assert.NoError(t, err)
	// Check genreated fields
	assert.NotNil(t, sub.ID)
	assert.Equal(t, "websockets", sub.Transport)
	assert.Equal(t, "12345", string(*sub.Options.FirstEvent))
}

func TestUpdateDurableSubscriptionOk(t *testing.T) {
	em, cancel := newTestEventManager(t)
	defer cancel()
	mdi := em.database.(*databasemocks.Plugin)
	sub := &fftypes.Subscription{
		SubscriptionRef: fftypes.SubscriptionRef{
			ID:        fftypes.NewUUID(),
			Namespace: "ns1",
			Name:      "sub1",
		},
	}
	var firstEvent fftypes.SubOptsFirstEvent = "12345"
	mdi.On("GetSubscriptionByName", mock.Anything, "ns1", "sub1").Return(&fftypes.Subscription{
		SubscriptionRef: fftypes.SubscriptionRef{
			ID: fftypes.NewUUID(),
		},
		Options: fftypes.SubscriptionOptions{
			SubscriptionCoreOptions: fftypes.SubscriptionCoreOptions{
				FirstEvent: &firstEvent,
			},
		},
	}, nil) // return non-matching existing
	mdi.On("UpsertSubscription", mock.Anything, mock.Anything, true).Return(nil)
	err := em.CreateUpdateDurableSubscription(em.ctx, sub, false)
	assert.NoError(t, err)
	// Check genreated fields
	assert.NotNil(t, sub.ID)
	assert.Equal(t, "websockets", sub.Transport)
	assert.Equal(t, "12345", string(*sub.Options.FirstEvent))
}

func TestUpdateDurableSubscriptionNoOp(t *testing.T) {
	em, cancel := newTestEventManager(t)
	defer cancel()
	mdi := em.database.(*databasemocks.Plugin)
	no := false
	sub := &fftypes.Subscription{
		SubscriptionRef: fftypes.SubscriptionRef{
			ID:        fftypes.NewUUID(),
			Namespace: "ns1",
			Name:      "sub1",
		},
		Transport: "websockets",
		Options: fftypes.SubscriptionOptions{
			SubscriptionCoreOptions: fftypes.SubscriptionCoreOptions{
				WithData: &no,
			},
		},
	}
	var subExisting = *sub
	subExisting.Created = fftypes.Now()
	subExisting.Updated = fftypes.Now()
	subExisting.ID = fftypes.NewUUID()
	mdi.On("GetSubscriptionByName", mock.Anything, "ns1", "sub1").Return(&subExisting, nil) // return non-matching existing
	err := em.CreateUpdateDurableSubscription(em.ctx, sub, false)
	assert.NoError(t, err)
}

func TestCreateDeleteDurableSubscriptionOk(t *testing.T) {
	em, cancel := newTestEventManager(t)
	defer cancel()
	mdi := em.database.(*databasemocks.Plugin)
	subId := fftypes.NewUUID()
	sub := &fftypes.Subscription{SubscriptionRef: fftypes.SubscriptionRef{ID: subId, Namespace: "ns1"}}
	mdi.On("GetSubscriptionByID", mock.Anything, subId).Return(sub, nil)
	mdi.On("DeleteSubscriptionByID", mock.Anything, subId).Return(nil)
	err := em.DeleteDurableSubscription(em.ctx, sub)
	assert.NoError(t, err)
}

func TestAddInternalListener(t *testing.T) {
	em, cancel := newTestEventManager(t)
	ie := &system.Events{}
	cbs := &eventsmocks.Callbacks{}

	cbs.On("RegisterConnection", mock.Anything, mock.Anything).Return(nil)
	cbs.On("EphemeralSubscription", mock.Anything, "ns1", mock.Anything, mock.Anything).Return(nil)

	conf := config.NewPluginConfig("ut.events")
	ie.InitPrefix(conf)
	ie.Init(em.ctx, conf, cbs)
	em.internalEvents = ie
	defer cancel()
	err := em.AddSystemEventListener("ns1", func(event *fftypes.EventDelivery) error { return nil })
	assert.NoError(t, err)

	cbs.AssertExpectations(t)
}
