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

package ethereum

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/hyperledger/firefly/internal/config"
	"github.com/hyperledger/firefly/internal/log"
	"github.com/hyperledger/firefly/internal/restclient"
	"github.com/hyperledger/firefly/mocks/blockchainmocks"
	"github.com/hyperledger/firefly/mocks/metricsmocks"
	"github.com/hyperledger/firefly/mocks/wsmocks"
	"github.com/hyperledger/firefly/pkg/blockchain"
	"github.com/hyperledger/firefly/pkg/fftypes"
	"github.com/hyperledger/firefly/pkg/wsclient"
	"github.com/jarcoal/httpmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

var utConfPrefix = config.NewPluginConfig("eth_unit_tests")
var utEthconnectConf = utConfPrefix.SubPrefix(EthconnectConfigKey)
var utAddressResolverConf = utConfPrefix.SubPrefix(AddressResolverConfigKey)

func testFFIMethod() *fftypes.FFIMethod {
	return &fftypes.FFIMethod{
		Name: "sum",
		Params: []*fftypes.FFIParam{
			{
				Name:   "x",
				Schema: fftypes.JSONAnyPtr(`{"type": "integer", "details": {"type": "uint256"}}`),
			},
			{
				Name:   "y",
				Schema: fftypes.JSONAnyPtr(`{"type": "integer", "details": {"type": "uint256"}}`),
			},
		},
		Returns: []*fftypes.FFIParam{
			{
				Name:   "z",
				Schema: fftypes.JSONAnyPtr(`{"type": "integer", "details": {"type": "uint256"}}`),
			},
		},
	}
}

func resetConf() {
	config.Reset()
	e := &Ethereum{}
	e.InitPrefix(utConfPrefix)
}

func newTestEthereum() (*Ethereum, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	em := &blockchainmocks.Callbacks{}
	wsm := &wsmocks.WSClient{}
	mm := &metricsmocks.Manager{}
	mm.On("IsMetricsEnabled").Return(true)
	mm.On("BlockchainTransaction", mock.Anything, mock.Anything).Return(nil)
	mm.On("BlockchainQuery", mock.Anything, mock.Anything).Return(nil)
	e := &Ethereum{
		ctx:          ctx,
		client:       resty.New().SetBaseURL("http://localhost:12345"),
		instancePath: "/instances/0x12345",
		topic:        "topic1",
		prefixShort:  defaultPrefixShort,
		prefixLong:   defaultPrefixLong,
		callbacks:    em,
		wsconn:       wsm,
		metrics:      mm,
	}
	return e, func() {
		cancel()
		if e.closed != nil {
			// We've init'd, wait to close
			<-e.closed
		}
	}
}

func TestInitMissingURL(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	resetConf()
	err := e.Init(e.ctx, utConfPrefix, &blockchainmocks.Callbacks{}, &metricsmocks.Manager{})
	assert.Regexp(t, "FF10138.*url", err)
}

func TestInitBadAddressResolver(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	resetConf()
	utAddressResolverConf.Set(AddressResolverURLTemplate, "{{unclosed}")
	err := e.Init(e.ctx, utConfPrefix, &blockchainmocks.Callbacks{}, &metricsmocks.Manager{})
	assert.Regexp(t, "FF10337.*urlTemplate", err)
}

func TestInitMissingInstance(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	resetConf()
	utEthconnectConf.Set(restclient.HTTPConfigURL, "http://localhost:12345")
	utEthconnectConf.Set(EthconnectConfigTopic, "topic1")

	err := e.Init(e.ctx, utConfPrefix, &blockchainmocks.Callbacks{}, &metricsmocks.Manager{})
	assert.Regexp(t, "FF10138.*instance", err)
}

func TestInitMissingTopic(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	resetConf()
	utEthconnectConf.Set(restclient.HTTPConfigURL, "http://localhost:12345")
	utEthconnectConf.Set(EthconnectConfigInstancePath, "/instances/0x12345")

	err := e.Init(e.ctx, utConfPrefix, &blockchainmocks.Callbacks{}, &metricsmocks.Manager{})
	assert.Regexp(t, "FF10138.*topic", err)
}

func TestInitAllNewStreamsAndWSEvent(t *testing.T) {

	log.SetLevel("trace")
	e, cancel := newTestEthereum()
	defer cancel()

	toServer, fromServer, wsURL, done := wsclient.NewTestWSServer(nil)
	defer done()

	mockedClient := &http.Client{}
	httpmock.ActivateNonDefault(mockedClient)
	defer httpmock.DeactivateAndReset()

	u, _ := url.Parse(wsURL)
	u.Scheme = "http"
	httpURL := u.String()

	httpmock.RegisterResponder("GET", fmt.Sprintf("%s/eventstreams", httpURL),
		httpmock.NewJsonResponderOrPanic(200, []eventStream{}))
	httpmock.RegisterResponder("POST", fmt.Sprintf("%s/eventstreams", httpURL),
		httpmock.NewJsonResponderOrPanic(200, eventStream{ID: "es12345"}))
	httpmock.RegisterResponder("GET", fmt.Sprintf("%s/subscriptions", httpURL),
		httpmock.NewJsonResponderOrPanic(200, []subscription{}))
	httpmock.RegisterResponder("POST", fmt.Sprintf("%s/subscriptions", httpURL),
		func(req *http.Request) (*http.Response, error) {
			var body map[string]interface{}
			json.NewDecoder(req.Body).Decode(&body)
			assert.Equal(t, "es12345", body["stream"])
			return httpmock.NewJsonResponderOrPanic(200, subscription{ID: "sub12345"})(req)
		})

	resetConf()
	utEthconnectConf.Set(restclient.HTTPConfigURL, httpURL)
	utEthconnectConf.Set(restclient.HTTPCustomClient, mockedClient)
	utEthconnectConf.Set(EthconnectConfigInstancePath, "/instances/0x12345")
	utEthconnectConf.Set(EthconnectConfigTopic, "topic1")

	err := e.Init(e.ctx, utConfPrefix, &blockchainmocks.Callbacks{}, &metricsmocks.Manager{})
	assert.NoError(t, err)

	assert.Equal(t, "ethereum", e.Name())
	assert.Equal(t, fftypes.VerifierTypeEthAddress, e.VerifierType())
	assert.Equal(t, 4, httpmock.GetTotalCallCount())
	assert.Equal(t, "es12345", e.initInfo.stream.ID)
	assert.Equal(t, "sub12345", e.initInfo.sub.ID)
	assert.True(t, e.Capabilities().GlobalSequencer)

	err = e.Start()
	assert.NoError(t, err)

	startupMessage := <-toServer
	assert.Equal(t, `{"type":"listen","topic":"topic1"}`, startupMessage)
	startupMessage = <-toServer
	assert.Equal(t, `{"type":"listenreplies"}`, startupMessage)
	fromServer <- `[]` // empty batch, will be ignored, but acked
	reply := <-toServer
	assert.Equal(t, `{"topic":"topic1","type":"ack"}`, reply)

	// Bad data will be ignored
	fromServer <- `!json`
	fromServer <- `{"not": "a reply"}`
	fromServer <- `42`

}

func TestWSInitFail(t *testing.T) {

	e, cancel := newTestEthereum()
	defer cancel()

	resetConf()
	utEthconnectConf.Set(restclient.HTTPConfigURL, "!!!://")
	utEthconnectConf.Set(EthconnectConfigInstancePath, "/instances/0x12345")
	utEthconnectConf.Set(EthconnectConfigTopic, "topic1")

	err := e.Init(e.ctx, utConfPrefix, &blockchainmocks.Callbacks{}, &metricsmocks.Manager{})
	assert.Regexp(t, "FF10162", err)

}

func TestWSConnectFail(t *testing.T) {

	wsm := &wsmocks.WSClient{}
	e := &Ethereum{
		ctx:    context.Background(),
		wsconn: wsm,
	}
	wsm.On("Connect").Return(fmt.Errorf("pop"))

	err := e.Start()
	assert.EqualError(t, err, "pop")
}

func TestInitAllExistingStreams(t *testing.T) {

	e, cancel := newTestEthereum()
	defer cancel()

	mockedClient := &http.Client{}
	httpmock.ActivateNonDefault(mockedClient)
	defer httpmock.DeactivateAndReset()

	httpmock.RegisterResponder("GET", "http://localhost:12345/eventstreams",
		httpmock.NewJsonResponderOrPanic(200, []eventStream{{ID: "es12345", WebSocket: eventStreamWebsocket{Topic: "topic1"}}}))
	httpmock.RegisterResponder("GET", "http://localhost:12345/subscriptions",
		httpmock.NewJsonResponderOrPanic(200, []subscription{
			{ID: "sub12345", Stream: "es12345", Name: "BatchPin_30783132333435e3" /* this is the subname for our combo of instance path and BatchPin */},
		}))
	httpmock.RegisterResponder("PATCH", "http://localhost:12345/eventstreams/es12345",
		httpmock.NewJsonResponderOrPanic(200, &eventStream{ID: "es12345", WebSocket: eventStreamWebsocket{Topic: "topic1"}}))

	resetConf()
	utEthconnectConf.Set(restclient.HTTPConfigURL, "http://localhost:12345")
	utEthconnectConf.Set(restclient.HTTPCustomClient, mockedClient)
	utEthconnectConf.Set(EthconnectConfigInstancePath, "0x12345")
	utEthconnectConf.Set(EthconnectConfigTopic, "topic1")

	err := e.Init(e.ctx, utConfPrefix, &blockchainmocks.Callbacks{}, &metricsmocks.Manager{})

	assert.Equal(t, 3, httpmock.GetTotalCallCount())
	assert.Equal(t, "es12345", e.initInfo.stream.ID)
	assert.Equal(t, "sub12345", e.initInfo.sub.ID)

	assert.NoError(t, err)

}

func TestInitOldInstancePathContracts(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	resetConf()

	mockedClient := &http.Client{}
	httpmock.ActivateNonDefault(mockedClient)
	defer httpmock.DeactivateAndReset()

	httpmock.RegisterResponder("GET", "http://localhost:12345/eventstreams",
		httpmock.NewJsonResponderOrPanic(200, []eventStream{}))
	httpmock.RegisterResponder("POST", "http://localhost:12345/eventstreams",
		httpmock.NewJsonResponderOrPanic(200, eventStream{ID: "es12345"}))
	httpmock.RegisterResponder("GET", "http://localhost:12345/subscriptions",
		httpmock.NewJsonResponderOrPanic(200, []subscription{}))
	httpmock.RegisterResponder("POST", "http://localhost:12345/subscriptions",
		func(req *http.Request) (*http.Response, error) {
			var body map[string]interface{}
			json.NewDecoder(req.Body).Decode(&body)
			assert.Equal(t, "es12345", body["stream"])
			return httpmock.NewJsonResponderOrPanic(200, subscription{ID: "sub12345"})(req)
		})
	httpmock.RegisterResponder("GET", "http://localhost:12345/contracts/firefly",
		httpmock.NewJsonResponderOrPanic(200, map[string]string{
			"created":      "2022-02-08T22:10:10Z",
			"address":      "12345",
			"path":         "/contracts/firefly",
			"abi":          "fc49dec3-0660-4dc7-61af-65af4c3ac456",
			"openapi":      "/contracts/firefly?swagger",
			"registeredAs": "firefly",
		}),
	)

	resetConf()
	utEthconnectConf.Set(restclient.HTTPConfigURL, "http://localhost:12345")
	utEthconnectConf.Set(restclient.HTTPCustomClient, mockedClient)
	utEthconnectConf.Set(EthconnectConfigInstancePath, "/contracts/firefly")
	utEthconnectConf.Set(EthconnectConfigTopic, "topic1")

	err := e.Init(e.ctx, utConfPrefix, &blockchainmocks.Callbacks{}, &metricsmocks.Manager{})
	assert.NoError(t, err)
	assert.Equal(t, e.instancePath, "0x12345")
}

func TestInitOldInstancePathInstances(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	resetConf()

	mockedClient := &http.Client{}
	httpmock.ActivateNonDefault(mockedClient)
	defer httpmock.DeactivateAndReset()

	httpmock.RegisterResponder("GET", "http://localhost:12345/eventstreams",
		httpmock.NewJsonResponderOrPanic(200, []eventStream{}))
	httpmock.RegisterResponder("POST", "http://localhost:12345/eventstreams",
		httpmock.NewJsonResponderOrPanic(200, eventStream{ID: "es12345"}))
	httpmock.RegisterResponder("GET", "http://localhost:12345/subscriptions",
		httpmock.NewJsonResponderOrPanic(200, []subscription{}))
	httpmock.RegisterResponder("POST", "http://localhost:12345/subscriptions",
		func(req *http.Request) (*http.Response, error) {
			var body map[string]interface{}
			json.NewDecoder(req.Body).Decode(&body)
			assert.Equal(t, "es12345", body["stream"])
			return httpmock.NewJsonResponderOrPanic(200, subscription{ID: "sub12345"})(req)
		})

	resetConf()
	utEthconnectConf.Set(restclient.HTTPConfigURL, "http://localhost:12345")
	utEthconnectConf.Set(restclient.HTTPCustomClient, mockedClient)
	utEthconnectConf.Set(EthconnectConfigInstancePath, "/instances/0x12345")
	utEthconnectConf.Set(EthconnectConfigTopic, "topic1")

	err := e.Init(e.ctx, utConfPrefix, &blockchainmocks.Callbacks{}, &metricsmocks.Manager{})
	assert.NoError(t, err)
	assert.Equal(t, e.instancePath, "0x12345")
}

func TestInitOldInstancePathError(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	resetConf()

	mockedClient := &http.Client{}
	httpmock.ActivateNonDefault(mockedClient)
	defer httpmock.DeactivateAndReset()

	httpmock.RegisterResponder("GET", "http://localhost:12345/eventstreams",
		httpmock.NewJsonResponderOrPanic(200, []eventStream{}))
	httpmock.RegisterResponder("POST", "http://localhost:12345/eventstreams",
		httpmock.NewJsonResponderOrPanic(200, eventStream{ID: "es12345"}))
	httpmock.RegisterResponder("GET", "http://localhost:12345/subscriptions",
		httpmock.NewJsonResponderOrPanic(200, []subscription{}))
	httpmock.RegisterResponder("POST", "http://localhost:12345/subscriptions",
		func(req *http.Request) (*http.Response, error) {
			var body map[string]interface{}
			json.NewDecoder(req.Body).Decode(&body)
			assert.Equal(t, "es12345", body["stream"])
			return httpmock.NewJsonResponderOrPanic(200, subscription{ID: "sub12345"})(req)
		})
	httpmock.RegisterResponder("GET", "http://localhost:12345/contracts/firefly",
		httpmock.NewJsonResponderOrPanic(500, "pop"),
	)

	resetConf()
	utEthconnectConf.Set(restclient.HTTPConfigURL, "http://localhost:12345")
	utEthconnectConf.Set(restclient.HTTPCustomClient, mockedClient)
	utEthconnectConf.Set(EthconnectConfigInstancePath, "/contracts/firefly")
	utEthconnectConf.Set(EthconnectConfigTopic, "topic1")

	err := e.Init(e.ctx, utConfPrefix, &blockchainmocks.Callbacks{}, &metricsmocks.Manager{})
	assert.Regexp(t, "FF10111", err)
	assert.Regexp(t, "pop", err)
}

func TestStreamQueryError(t *testing.T) {

	e, cancel := newTestEthereum()
	defer cancel()

	mockedClient := &http.Client{}
	httpmock.ActivateNonDefault(mockedClient)
	defer httpmock.DeactivateAndReset()

	httpmock.RegisterResponder("GET", "http://localhost:12345/eventstreams",
		httpmock.NewStringResponder(500, `pop`))

	resetConf()
	utEthconnectConf.Set(restclient.HTTPConfigURL, "http://localhost:12345")
	utEthconnectConf.Set(restclient.HTTPConfigRetryEnabled, false)
	utEthconnectConf.Set(restclient.HTTPCustomClient, mockedClient)
	utEthconnectConf.Set(EthconnectConfigInstancePath, "/instances/0x12345")
	utEthconnectConf.Set(EthconnectConfigTopic, "topic1")

	err := e.Init(e.ctx, utConfPrefix, &blockchainmocks.Callbacks{}, &metricsmocks.Manager{})

	assert.Regexp(t, "FF10111", err)
	assert.Regexp(t, "pop", err)

}

func TestStreamCreateError(t *testing.T) {

	e, cancel := newTestEthereum()
	defer cancel()

	mockedClient := &http.Client{}
	httpmock.ActivateNonDefault(mockedClient)
	defer httpmock.DeactivateAndReset()

	httpmock.RegisterResponder("GET", "http://localhost:12345/eventstreams",
		httpmock.NewJsonResponderOrPanic(200, []eventStream{}))
	httpmock.RegisterResponder("POST", "http://localhost:12345/eventstreams",
		httpmock.NewStringResponder(500, `pop`))

	resetConf()
	utEthconnectConf.Set(restclient.HTTPConfigURL, "http://localhost:12345")
	utEthconnectConf.Set(restclient.HTTPConfigRetryEnabled, false)
	utEthconnectConf.Set(restclient.HTTPCustomClient, mockedClient)
	utEthconnectConf.Set(EthconnectConfigInstancePath, "/instances/0x12345")
	utEthconnectConf.Set(EthconnectConfigTopic, "topic1")

	err := e.Init(e.ctx, utConfPrefix, &blockchainmocks.Callbacks{}, &metricsmocks.Manager{})

	assert.Regexp(t, "FF10111", err)
	assert.Regexp(t, "pop", err)

}

func TestStreamUpdateError(t *testing.T) {

	e, cancel := newTestEthereum()
	defer cancel()

	mockedClient := &http.Client{}
	httpmock.ActivateNonDefault(mockedClient)
	defer httpmock.DeactivateAndReset()

	httpmock.RegisterResponder("GET", "http://localhost:12345/eventstreams",
		httpmock.NewJsonResponderOrPanic(200, []eventStream{{ID: "es12345", WebSocket: eventStreamWebsocket{Topic: "topic1"}}}))
	httpmock.RegisterResponder("PATCH", "http://localhost:12345/eventstreams/es12345",
		httpmock.NewStringResponder(500, `pop`))

	resetConf()
	utEthconnectConf.Set(restclient.HTTPConfigURL, "http://localhost:12345")
	utEthconnectConf.Set(restclient.HTTPConfigRetryEnabled, false)
	utEthconnectConf.Set(restclient.HTTPCustomClient, mockedClient)
	utEthconnectConf.Set(EthconnectConfigInstancePath, "/instances/0x12345")
	utEthconnectConf.Set(EthconnectConfigTopic, "topic1")

	err := e.Init(e.ctx, utConfPrefix, &blockchainmocks.Callbacks{}, &metricsmocks.Manager{})

	assert.Regexp(t, "FF10111", err)
	assert.Regexp(t, "pop", err)

}

func TestSubQueryError(t *testing.T) {

	e, cancel := newTestEthereum()
	defer cancel()

	mockedClient := &http.Client{}
	httpmock.ActivateNonDefault(mockedClient)
	defer httpmock.DeactivateAndReset()

	httpmock.RegisterResponder("GET", "http://localhost:12345/eventstreams",
		httpmock.NewJsonResponderOrPanic(200, []eventStream{}))
	httpmock.RegisterResponder("POST", "http://localhost:12345/eventstreams",
		httpmock.NewJsonResponderOrPanic(200, eventStream{ID: "es12345"}))
	httpmock.RegisterResponder("GET", "http://localhost:12345/subscriptions",
		httpmock.NewStringResponder(500, `pop`))

	resetConf()
	utEthconnectConf.Set(restclient.HTTPConfigURL, "http://localhost:12345")
	utEthconnectConf.Set(restclient.HTTPConfigRetryEnabled, false)
	utEthconnectConf.Set(restclient.HTTPCustomClient, mockedClient)
	utEthconnectConf.Set(EthconnectConfigInstancePath, "/instances/0x12345")
	utEthconnectConf.Set(EthconnectConfigTopic, "topic1")

	err := e.Init(e.ctx, utConfPrefix, &blockchainmocks.Callbacks{}, &metricsmocks.Manager{})

	assert.Regexp(t, "FF10111", err)
	assert.Regexp(t, "pop", err)

}

func TestSubQueryCreateError(t *testing.T) {

	e, cancel := newTestEthereum()
	defer cancel()

	mockedClient := &http.Client{}
	httpmock.ActivateNonDefault(mockedClient)
	defer httpmock.DeactivateAndReset()

	httpmock.RegisterResponder("GET", "http://localhost:12345/eventstreams",
		httpmock.NewJsonResponderOrPanic(200, []eventStream{}))
	httpmock.RegisterResponder("POST", "http://localhost:12345/eventstreams",
		httpmock.NewJsonResponderOrPanic(200, eventStream{ID: "es12345"}))
	httpmock.RegisterResponder("GET", "http://localhost:12345/subscriptions",
		httpmock.NewJsonResponderOrPanic(200, []subscription{}))
	httpmock.RegisterResponder("POST", "http://localhost:12345/subscriptions",
		httpmock.NewStringResponder(500, `pop`))

	resetConf()
	utEthconnectConf.Set(restclient.HTTPConfigURL, "http://localhost:12345")
	utEthconnectConf.Set(restclient.HTTPConfigRetryEnabled, false)
	utEthconnectConf.Set(restclient.HTTPCustomClient, mockedClient)
	utEthconnectConf.Set(EthconnectConfigInstancePath, "/instances/0x12345")
	utEthconnectConf.Set(EthconnectConfigTopic, "topic1")

	err := e.Init(e.ctx, utConfPrefix, &blockchainmocks.Callbacks{}, &metricsmocks.Manager{})

	assert.Regexp(t, "FF10111", err)
	assert.Regexp(t, "pop", err)

}

func TestSubmitBatchPinOK(t *testing.T) {

	e, cancel := newTestEthereum()
	defer cancel()
	httpmock.ActivateNonDefault(e.client.GetClient())
	defer httpmock.DeactivateAndReset()

	addr := ethHexFormatB32(fftypes.NewRandB32())
	batch := &blockchain.BatchPin{
		TransactionID:   fftypes.MustParseUUID("9ffc50ff-6bfe-4502-adc7-93aea54cc059"),
		BatchID:         fftypes.MustParseUUID("c5df767c-fe44-4e03-8eb5-1c5523097db5"),
		BatchHash:       fftypes.NewRandB32(),
		BatchPayloadRef: "Qmf412jQZiuVUtdgnB36FXFX7xg5V6KEbSJ4dpQuhkLyfD",
		Contexts: []*fftypes.Bytes32{
			fftypes.NewRandB32(),
			fftypes.NewRandB32(),
		},
	}

	httpmock.RegisterResponder("POST", `http://localhost:12345/`,
		func(req *http.Request) (*http.Response, error) {
			var body map[string]interface{}
			json.NewDecoder(req.Body).Decode(&body)
			params := body["params"].([]interface{})
			headers := body["headers"].(map[string]interface{})
			assert.Equal(t, "SendTransaction", headers["type"])
			assert.Equal(t, "0x9ffc50ff6bfe4502adc793aea54cc059c5df767cfe444e038eb51c5523097db5", params[1])
			assert.Equal(t, ethHexFormatB32(batch.BatchHash), params[2])
			assert.Equal(t, "Qmf412jQZiuVUtdgnB36FXFX7xg5V6KEbSJ4dpQuhkLyfD", params[3])
			return httpmock.NewJsonResponderOrPanic(200, "")(req)
		})

	err := e.SubmitBatchPin(context.Background(), nil, nil, addr, batch)

	assert.NoError(t, err)

}

func TestSubmitBatchEmptyPayloadRef(t *testing.T) {

	e, cancel := newTestEthereum()
	defer cancel()
	httpmock.ActivateNonDefault(e.client.GetClient())
	defer httpmock.DeactivateAndReset()

	addr := ethHexFormatB32(fftypes.NewRandB32())
	batch := &blockchain.BatchPin{
		TransactionID: fftypes.MustParseUUID("9ffc50ff-6bfe-4502-adc7-93aea54cc059"),
		BatchID:       fftypes.MustParseUUID("c5df767c-fe44-4e03-8eb5-1c5523097db5"),
		BatchHash:     fftypes.NewRandB32(),
		Contexts: []*fftypes.Bytes32{
			fftypes.NewRandB32(),
			fftypes.NewRandB32(),
		},
	}

	httpmock.RegisterResponder("POST", `http://localhost:12345/`,
		func(req *http.Request) (*http.Response, error) {
			var body map[string]interface{}
			json.NewDecoder(req.Body).Decode(&body)
			params := body["params"].([]interface{})
			headers := body["headers"].(map[string]interface{})
			assert.Equal(t, "SendTransaction", headers["type"])
			assert.Equal(t, "0x9ffc50ff6bfe4502adc793aea54cc059c5df767cfe444e038eb51c5523097db5", params[1])
			assert.Equal(t, ethHexFormatB32(batch.BatchHash), params[2])
			assert.Equal(t, "", params[3])
			return httpmock.NewJsonResponderOrPanic(200, "")(req)
		})

	err := e.SubmitBatchPin(context.Background(), nil, nil, addr, batch)

	assert.NoError(t, err)

}

func TestSubmitBatchPinFail(t *testing.T) {

	e, cancel := newTestEthereum()
	defer cancel()
	httpmock.ActivateNonDefault(e.client.GetClient())
	defer httpmock.DeactivateAndReset()

	addr := ethHexFormatB32(fftypes.NewRandB32())
	batch := &blockchain.BatchPin{
		TransactionID:   fftypes.NewUUID(),
		BatchID:         fftypes.NewUUID(),
		BatchHash:       fftypes.NewRandB32(),
		BatchPayloadRef: "Qmf412jQZiuVUtdgnB36FXFX7xg5V6KEbSJ4dpQuhkLyfD",
		Contexts: []*fftypes.Bytes32{
			fftypes.NewRandB32(),
			fftypes.NewRandB32(),
		},
	}

	httpmock.RegisterResponder("POST", `http://localhost:12345/`,
		httpmock.NewStringResponder(500, "pop"))

	err := e.SubmitBatchPin(context.Background(), nil, nil, addr, batch)

	assert.Regexp(t, "FF10111", err)
	assert.Regexp(t, "pop", err)

}

func TestVerifyEthAddress(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()

	_, err := e.NormalizeSigningKey(context.Background(), "0x12345")
	assert.Regexp(t, "FF10141", err)

	key, err := e.NormalizeSigningKey(context.Background(), "0x2a7c9D5248681CE6c393117E641aD037F5C079F6")
	assert.NoError(t, err)
	assert.Equal(t, "0x2a7c9d5248681ce6c393117e641ad037f5c079f6", key)

}

func TestHandleMessageBatchPinOK(t *testing.T) {
	data := fftypes.JSONAnyPtr(`
[
  {
		"address": "0x1C197604587F046FD40684A8f21f4609FB811A7b",
		"blockNumber": "38011",
		"transactionIndex": "0x0",
		"transactionHash": "0xc26df2bf1a733e9249372d61eb11bd8662d26c8129df76890b1beb2f6fa72628",
		"data": {
			"author": "0X91D2B4381A4CD5C7C0F27565A7D4B829844C8635",
			"namespace": "ns1",
			"uuids": "0xe19af8b390604051812d7597d19adfb9847d3bfd074249efb65d3fed15f5b0a6",
			"batchHash": "0xd71eb138d74c229a388eb0e1abc03f4c7cbb21d4fc4b839fbf0ec73e4263f6be",
			"payloadRef": "Qmf412jQZiuVUtdgnB36FXFX7xg5V6KEbSJ4dpQuhkLyfD",
			"contexts": [
				"0x68e4da79f805bca5b912bcda9c63d03e6e867108dabb9b944109aea541ef522a",
				"0x19b82093de5ce92a01e333048e877e2374354bf846dd034864ef6ffbd6438771"
			]
    },
		"subId": "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
		"signature": "BatchPin(address,uint256,string,bytes32,bytes32,string,bytes32[])",
		"logIndex": "50",
		"timestamp": "1620576488"
  },
  {
		"address": "0x1C197604587F046FD40684A8f21f4609FB811A7b",
		"blockNumber": "38011",
		"transactionIndex": "0x1",
		"transactionHash": "0x0c50dff0893e795293189d9cc5ba0d63c4020d8758ace4a69d02c9d6d43cb695",
		"data": {
			"author": "0x91d2b4381a4cd5c7c0f27565a7d4b829844c8635",
			"namespace": "ns1",
			"uuids": "0x8a578549e56b49f9bd78d731f22b08d7a04c7cc37d444c2ba3b054e21326697e",
			"batchHash": "0x20e6ef9b9c4df7fdb77a7de1e00347f4b02d996f2e56a7db361038be7b32a154",
			"payloadRef": "Qmf412jQZiuVUtdgnB36FXFX7xg5V6KEbSJ4dpQuhkLyfD",
			"contexts": [
				"0x8a63eb509713b0cf9250a8eee24ee2dfc4b37225e3ad5c29c95127699d382f85"
			]
    },
		"subId": "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
		"signature": "BatchPin(address,uint256,string,bytes32,bytes32,string,bytes32[])",
		"logIndex": "51",
		"timestamp": "1620576488"
  },
	{
		"address": "0x06d34B270F15a0d82913EFD0627B0F62Fd22ecd5",
		"blockNumber": "38011",
		"transactionIndex": "0x2",
		"transactionHash": "0x0c50dff0893e795293189d9cc5ba0d63c4020d8758ace4a69d02c9d6d43cb695",
		"data": {
			"author": "0x91d2b4381a4cd5c7c0f27565a7d4b829844c8635",
			"namespace": "ns1",
			"uuids": "0x8a578549e56b49f9bd78d731f22b08d7a04c7cc37d444c2ba3b054e21326697e",
			"batchHash": "0x892b31099b8476c0692a5f2982ea23a0614949eacf292a64a358aa73ecd404b4",
			"payloadRef": "Qmf412jQZiuVUtdgnB36FXFX7xg5V6KEbSJ4dpQuhkLyfD",
			"contexts": [
				"0xdab67320f1a0d0f1da572975e3a9ab6ef0fed315771c99fea0bfb54886c1aa94"
			]
    },
		"subId": "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
		"signature": "Random(address,uint256,bytes32,bytes32,bytes32)",
		"logIndex": "51",
		"timestamp": "1620576488"
  }
]`)

	em := &blockchainmocks.Callbacks{}
	e := &Ethereum{
		callbacks: em,
	}
	e.initInfo.sub = &subscription{
		ID: "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
	}

	expectedSigningKeyRef := &fftypes.VerifierRef{
		Type:  fftypes.VerifierTypeEthAddress,
		Value: "0x91d2b4381a4cd5c7c0f27565a7d4b829844c8635",
	}

	em.On("BatchPinComplete", mock.Anything, expectedSigningKeyRef, mock.Anything).Return(nil)

	var events []interface{}
	err := json.Unmarshal(data.Bytes(), &events)
	assert.NoError(t, err)
	err = e.handleMessageBatch(context.Background(), events)
	assert.NoError(t, err)

	b := em.Calls[0].Arguments[0].(*blockchain.BatchPin)
	assert.Equal(t, "ns1", b.Namespace)
	assert.Equal(t, "e19af8b3-9060-4051-812d-7597d19adfb9", b.TransactionID.String())
	assert.Equal(t, "847d3bfd-0742-49ef-b65d-3fed15f5b0a6", b.BatchID.String())
	assert.Equal(t, "d71eb138d74c229a388eb0e1abc03f4c7cbb21d4fc4b839fbf0ec73e4263f6be", b.BatchHash.String())
	assert.Equal(t, "Qmf412jQZiuVUtdgnB36FXFX7xg5V6KEbSJ4dpQuhkLyfD", b.BatchPayloadRef)
	assert.Equal(t, expectedSigningKeyRef, em.Calls[0].Arguments[1])
	assert.Len(t, b.Contexts, 2)
	assert.Equal(t, "68e4da79f805bca5b912bcda9c63d03e6e867108dabb9b944109aea541ef522a", b.Contexts[0].String())
	assert.Equal(t, "19b82093de5ce92a01e333048e877e2374354bf846dd034864ef6ffbd6438771", b.Contexts[1].String())

	info1 := fftypes.JSONObject{
		"address":          "0x1C197604587F046FD40684A8f21f4609FB811A7b",
		"blockNumber":      "38011",
		"logIndex":         "50",
		"signature":        "BatchPin(address,uint256,string,bytes32,bytes32,string,bytes32[])",
		"subId":            "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
		"transactionHash":  "0xc26df2bf1a733e9249372d61eb11bd8662d26c8129df76890b1beb2f6fa72628",
		"transactionIndex": "0x0",
		"timestamp":        "1620576488",
	}
	assert.Equal(t, info1, b.Event.Info)

	b2 := em.Calls[1].Arguments[0].(*blockchain.BatchPin)
	info2 := fftypes.JSONObject{
		"address":          "0x1C197604587F046FD40684A8f21f4609FB811A7b",
		"blockNumber":      "38011",
		"logIndex":         "51",
		"signature":        "BatchPin(address,uint256,string,bytes32,bytes32,string,bytes32[])",
		"subId":            "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
		"transactionHash":  "0x0c50dff0893e795293189d9cc5ba0d63c4020d8758ace4a69d02c9d6d43cb695",
		"transactionIndex": "0x1",
		"timestamp":        "1620576488",
	}
	assert.Equal(t, info2, b2.Event.Info)

	em.AssertExpectations(t)

}

func TestHandleMessageEmptyPayloadRef(t *testing.T) {
	data := fftypes.JSONAnyPtr(`
[
  {
		"address": "0x1C197604587F046FD40684A8f21f4609FB811A7b",
		"blockNumber": "38011",
		"transactionIndex": "0x0",
		"transactionHash": "0xc26df2bf1a733e9249372d61eb11bd8662d26c8129df76890b1beb2f6fa72628",
		"data": {
			"author": "0X91D2B4381A4CD5C7C0F27565A7D4B829844C8635",
			"namespace": "ns1",
			"uuids": "0xe19af8b390604051812d7597d19adfb9847d3bfd074249efb65d3fed15f5b0a6",
			"batchHash": "0xd71eb138d74c229a388eb0e1abc03f4c7cbb21d4fc4b839fbf0ec73e4263f6be",
			"payloadRef": "",
			"contexts": [
				"0x68e4da79f805bca5b912bcda9c63d03e6e867108dabb9b944109aea541ef522a",
				"0x19b82093de5ce92a01e333048e877e2374354bf846dd034864ef6ffbd6438771"
			]
    },
		"subId": "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
		"signature": "BatchPin(address,uint256,string,bytes32,bytes32,string,bytes32[])",
		"logIndex": "50",
		"timestamp": "1620576488"
  }
]`)

	em := &blockchainmocks.Callbacks{}
	e := &Ethereum{
		callbacks: em,
	}
	e.initInfo.sub = &subscription{
		ID: "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
	}

	expectedSigningKeyRef := &fftypes.VerifierRef{
		Type:  fftypes.VerifierTypeEthAddress,
		Value: "0x91d2b4381a4cd5c7c0f27565a7d4b829844c8635",
	}

	em.On("BatchPinComplete", mock.Anything, expectedSigningKeyRef, mock.Anything).Return(nil)

	var events []interface{}
	err := json.Unmarshal(data.Bytes(), &events)
	assert.NoError(t, err)
	err = e.handleMessageBatch(context.Background(), events)
	assert.NoError(t, err)

	b := em.Calls[0].Arguments[0].(*blockchain.BatchPin)
	assert.Equal(t, "ns1", b.Namespace)
	assert.Equal(t, "e19af8b3-9060-4051-812d-7597d19adfb9", b.TransactionID.String())
	assert.Equal(t, "847d3bfd-0742-49ef-b65d-3fed15f5b0a6", b.BatchID.String())
	assert.Equal(t, "d71eb138d74c229a388eb0e1abc03f4c7cbb21d4fc4b839fbf0ec73e4263f6be", b.BatchHash.String())
	assert.Empty(t, b.BatchPayloadRef)
	assert.Equal(t, expectedSigningKeyRef, em.Calls[0].Arguments[1])
	assert.Len(t, b.Contexts, 2)
	assert.Equal(t, "68e4da79f805bca5b912bcda9c63d03e6e867108dabb9b944109aea541ef522a", b.Contexts[0].String())
	assert.Equal(t, "19b82093de5ce92a01e333048e877e2374354bf846dd034864ef6ffbd6438771", b.Contexts[1].String())

	em.AssertExpectations(t)

}

func TestHandleMessageBatchPinExit(t *testing.T) {
	data := fftypes.JSONAnyPtr(`
[
  {
		"address": "0x1C197604587F046FD40684A8f21f4609FB811A7b",
		"blockNumber": "38011",
		"transactionIndex": "0x1",
		"transactionHash": "0x0c50dff0893e795293189d9cc5ba0d63c4020d8758ace4a69d02c9d6d43cb695",
		"data": {
			"author": "0x91d2b4381a4cd5c7c0f27565a7d4b829844c8635",
			"namespace": "ns1",
			"uuids": "0xe19af8b390604051812d7597d19adfb9a04c7cc37d444c2ba3b054e21326697e",
			"batchHash": "0x9c19a93b6e85fee041f60f097121829e54cd4aa97ed070d1bc76147caf911fed",
			"payloadRef": "Qmf412jQZiuVUtdgnB36FXFX7xg5V6KEbSJ4dpQuhkLyfD"
    },
		"subId": "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
		"signature": "BatchPin(address,uint256,string,bytes32,bytes32,string,bytes32[])",
		"logIndex": "51",
		"timestamp": "1620576488"
  }
]`)

	expectedSigningKeyRef := &fftypes.VerifierRef{
		Type:  fftypes.VerifierTypeEthAddress,
		Value: "0x91d2b4381a4cd5c7c0f27565a7d4b829844c8635",
	}

	em := &blockchainmocks.Callbacks{}
	e := &Ethereum{
		callbacks: em,
	}
	e.initInfo.sub = &subscription{
		ID: "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
	}

	em.On("BatchPinComplete", mock.Anything, expectedSigningKeyRef, mock.Anything).Return(fmt.Errorf("pop"))

	var events []interface{}
	err := json.Unmarshal(data.Bytes(), &events)
	assert.NoError(t, err)
	err = e.handleMessageBatch(context.Background(), events)
	assert.EqualError(t, err, "pop")

}

func TestHandleMessageBatchPinEmpty(t *testing.T) {
	em := &blockchainmocks.Callbacks{}
	e := &Ethereum{callbacks: em}
	e.initInfo.sub = &subscription{
		ID: "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
	}

	var events []interface{}
	err := json.Unmarshal([]byte(`
	[
		{
			"subId": "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
			"signature": "BatchPin(address,uint256,string,bytes32,bytes32,string,bytes32[])"
		}
	]`), &events)
	assert.NoError(t, err)
	err = e.handleMessageBatch(context.Background(), events)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(em.Calls))
}

func TestHandleMessageBatchMissingData(t *testing.T) {
	em := &blockchainmocks.Callbacks{}
	e := &Ethereum{callbacks: em}
	e.initInfo.sub = &subscription{
		ID: "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
	}

	var events []interface{}
	err := json.Unmarshal([]byte(`
	[
		{
			"subId": "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
			"signature": "BatchPin(address,uint256,string,bytes32,bytes32,string,bytes32[])",
			"timestamp": "1620576488"
		}
	]`), &events)
	assert.NoError(t, err)
	err = e.handleMessageBatch(context.Background(), events)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(em.Calls))
}

func TestHandleMessageBatchPinBadTransactionID(t *testing.T) {
	em := &blockchainmocks.Callbacks{}
	e := &Ethereum{callbacks: em}
	e.initInfo.sub = &subscription{
		ID: "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
	}
	data := fftypes.JSONAnyPtr(`[{
		"subId": "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
		"signature": "BatchPin(address,uint256,string,bytes32,bytes32,string,bytes32[])",
		"blockNumber": "38011",
		"transactionIndex": "0x1",
		"transactionHash": "0x0c50dff0893e795293189d9cc5ba0d63c4020d8758ace4a69d02c9d6d43cb695",
		"data": {
			"author": "0X91D2B4381A4CD5C7C0F27565A7D4B829844C8635",
			"namespace": "ns1",
			"uuids": "!good",
			"batchHash": "0xd71eb138d74c229a388eb0e1abc03f4c7cbb21d4fc4b839fbf0ec73e4263f6be",
			"payloadRef": "0xeda586bd8f3c4bc1db5c4b5755113b9a9b4174abe28679fdbc219129400dd7ae",
			"contexts": [
				"0xb41753f11522d4ef5c4a467972cf54744c04628ff84a1c994f1b288b2f6ec836",
				"0xc6c683a0fbe15e452e1ecc3751657446e2f645a8231e3ef9f3b4a8eae03c4136"
			]
		},
		"timestamp": "1620576488"
	}]`)
	var events []interface{}
	err := json.Unmarshal(data.Bytes(), &events)
	assert.NoError(t, err)
	err = e.handleMessageBatch(context.Background(), events)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(em.Calls))
}

func TestHandleMessageBatchPinBadIDentity(t *testing.T) {
	em := &blockchainmocks.Callbacks{}
	e := &Ethereum{callbacks: em}
	e.initInfo.sub = &subscription{
		ID: "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
	}
	data := fftypes.JSONAnyPtr(`[{
		"subId": "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
		"signature": "BatchPin(address,uint256,string,bytes32,bytes32,string,bytes32[])",
		"blockNumber": "38011",
		"transactionIndex": "0x1",
		"transactionHash": "0x0c50dff0893e795293189d9cc5ba0d63c4020d8758ace4a69d02c9d6d43cb695",
		"data": {
			"author": "!good",
			"namespace": "ns1",
			"uuids": "0xe19af8b390604051812d7597d19adfb9847d3bfd074249efb65d3fed15f5b0a6",
			"batchHash": "0xd71eb138d74c229a388eb0e1abc03f4c7cbb21d4fc4b839fbf0ec73e4263f6be",
			"payloadRef": "0xeda586bd8f3c4bc1db5c4b5755113b9a9b4174abe28679fdbc219129400dd7ae",
			"contexts": [
				"0xb41753f11522d4ef5c4a467972cf54744c04628ff84a1c994f1b288b2f6ec836",
				"0xc6c683a0fbe15e452e1ecc3751657446e2f645a8231e3ef9f3b4a8eae03c4136"
			]
		},
		"timestamp": "1620576488"
	}]`)
	var events []interface{}
	err := json.Unmarshal(data.Bytes(), &events)
	assert.NoError(t, err)
	err = e.handleMessageBatch(context.Background(), events)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(em.Calls))
}

func TestHandleMessageBatchPinBadBatchHash(t *testing.T) {
	em := &blockchainmocks.Callbacks{}
	e := &Ethereum{callbacks: em}
	e.initInfo.sub = &subscription{
		ID: "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
	}
	data := fftypes.JSONAnyPtr(`[{
		"subId": "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
		"signature": "BatchPin(address,uint256,string,bytes32,bytes32,string,bytes32[])",
		"blockNumber": "38011",
		"transactionIndex": "0x1",
		"transactionHash": "0x0c50dff0893e795293189d9cc5ba0d63c4020d8758ace4a69d02c9d6d43cb695",
		"data": {
			"author": "0X91D2B4381A4CD5C7C0F27565A7D4B829844C8635",
			"namespace": "ns1",
			"uuids": "0xe19af8b390604051812d7597d19adfb9847d3bfd074249efb65d3fed15f5b0a6",
			"batchHash": "!good",
			"payloadRef": "0xeda586bd8f3c4bc1db5c4b5755113b9a9b4174abe28679fdbc219129400dd7ae",
			"contexts": [
				"0xb41753f11522d4ef5c4a467972cf54744c04628ff84a1c994f1b288b2f6ec836",
				"0xc6c683a0fbe15e452e1ecc3751657446e2f645a8231e3ef9f3b4a8eae03c4136"
			]
		},
		"timestamp": "1620576488"
	}]`)
	var events []interface{}
	err := json.Unmarshal(data.Bytes(), &events)
	assert.NoError(t, err)
	err = e.handleMessageBatch(context.Background(), events)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(em.Calls))
}

func TestHandleMessageBatchPinBadPin(t *testing.T) {
	em := &blockchainmocks.Callbacks{}
	e := &Ethereum{callbacks: em}
	e.initInfo.sub = &subscription{
		ID: "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
	}
	data := fftypes.JSONAnyPtr(`[{
		"subId": "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
		"signature": "BatchPin(address,uint256,string,bytes32,bytes32,string,bytes32[])",
		"blockNumber": "38011",
		"transactionIndex": "0x1",
		"transactionHash": "0x0c50dff0893e795293189d9cc5ba0d63c4020d8758ace4a69d02c9d6d43cb695",
		"data": {
			"author": "0X91D2B4381A4CD5C7C0F27565A7D4B829844C8635",
			"namespace": "ns1",
			"uuids": "0xe19af8b390604051812d7597d19adfb9847d3bfd074249efb65d3fed15f5b0a6",
			"batchHash": "0xd71eb138d74c229a388eb0e1abc03f4c7cbb21d4fc4b839fbf0ec73e4263f6be",
			"payloadRef": "0xeda586bd8f3c4bc1db5c4b5755113b9a9b4174abe28679fdbc219129400dd7ae",
			"contexts": [
				"0xb41753f11522d4ef5c4a467972cf54744c04628ff84a1c994f1b288b2f6ec836",
				"!good"
			]
		},
		"timestamp": "1620576488"
	}]`)
	var events []interface{}
	err := json.Unmarshal(data.Bytes(), &events)
	assert.NoError(t, err)
	err = e.handleMessageBatch(context.Background(), events)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(em.Calls))
}

func TestHandleMessageBatchBadJSON(t *testing.T) {
	em := &blockchainmocks.Callbacks{}
	e := &Ethereum{callbacks: em}
	err := e.handleMessageBatch(context.Background(), []interface{}{10, 20})
	assert.NoError(t, err)
	assert.Equal(t, 0, len(em.Calls))
}

func TestEventLoopContextCancelled(t *testing.T) {
	e, cancel := newTestEthereum()
	cancel()
	r := make(<-chan []byte)
	wsm := e.wsconn.(*wsmocks.WSClient)
	wsm.On("Receive").Return(r)
	wsm.On("Close").Return()
	e.closed = make(chan struct{})
	e.eventLoop() // we're simply looking for it exiting
}

func TestEventLoopReceiveClosed(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	r := make(chan []byte)
	wsm := e.wsconn.(*wsmocks.WSClient)
	close(r)
	wsm.On("Receive").Return((<-chan []byte)(r))
	wsm.On("Close").Return()
	e.closed = make(chan struct{})
	e.eventLoop() // we're simply looking for it exiting
}

func TestEventLoopSendClosed(t *testing.T) {
	e, cancel := newTestEthereum()
	cancel()
	r := make(chan []byte)
	wsm := e.wsconn.(*wsmocks.WSClient)
	close(r)
	wsm.On("Receive").Return((<-chan []byte)(r))
	wsm.On("Send", mock.Anything, mock.Anything).Return(fmt.Errorf("pop"))
	wsm.On("Close").Return()
	e.closed = make(chan struct{})
	e.eventLoop() // we're simply looking for it exiting
}

func TestHandleReceiptTXSuccess(t *testing.T) {
	em := &blockchainmocks.Callbacks{}
	wsm := &wsmocks.WSClient{}
	e := &Ethereum{
		ctx:       context.Background(),
		topic:     "topic1",
		callbacks: em,
		wsconn:    wsm,
	}

	var reply fftypes.JSONObject
	operationID := fftypes.NewUUID()
	data := fftypes.JSONAnyPtr(`{
		"_id": "4373614c-e0f7-47b0-640e-7eacec417a9e",
		"blockHash": "0xad269b2b43481e44500f583108e8d24bd841fb767c7f526772959d195b9c72d5",
		"blockNumber": "209696",
		"cumulativeGasUsed": "24655",
		"from": "0x91d2b4381a4cd5c7c0f27565a7d4b829844c8635",
		"gasUsed": "24655",
		"headers": {
			"id": "4603a151-f212-446e-5c15-0f36b57cecc7",
			"requestId": "` + operationID.String() + `",
			"requestOffset": "zzn4y4v4si-zzjjepe9x4-requests:0:12",
			"timeElapsed": 3.966414429,
			"timeReceived": "2021-05-28T20:54:27.481245697Z",
			"type": "TransactionSuccess"
    },
		"nonce": "0",
		"receivedAt": 1622235271565,
		"status": "1",
		"to": "0xd3266a857285fb75eb7df37353b4a15c8bb828f5",
		"transactionHash": "0x71a38acb7a5d4a970854f6d638ceb1fa10a4b59cbf4ed7674273a1a8dc8b36b8",
		"transactionIndex": "0"
  }`)

	em.On("BlockchainOpUpdate",
		operationID,
		fftypes.OpStatusSucceeded,
		"0x71a38acb7a5d4a970854f6d638ceb1fa10a4b59cbf4ed7674273a1a8dc8b36b8",
		"",
		mock.Anything).Return(nil)

	err := json.Unmarshal(data.Bytes(), &reply)
	assert.NoError(t, err)
	err = e.handleReceipt(context.Background(), reply)
	assert.NoError(t, err)

}

func TestHandleBadPayloadsAndThenReceiptFailure(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	r := make(chan []byte)
	wsm := e.wsconn.(*wsmocks.WSClient)
	e.closed = make(chan struct{})

	wsm.On("Receive").Return((<-chan []byte)(r))
	wsm.On("Close").Return()
	operationID := fftypes.NewUUID()
	data := fftypes.JSONAnyPtr(`{
		"_id": "6fb94fff-81d3-4094-567d-e031b1871694",
		"errorMessage": "Packing arguments for method 'broadcastBatch': abi: cannot use [3]uint8 as type [32]uint8 as argument",
		"headers": {
			"id": "3a37b17b-13b6-4dc5-647a-07c11eae0be3",
			"requestId": "` + operationID.String() + `",
			"requestOffset": "zzn4y4v4si-zzjjepe9x4-requests:0:0",
			"timeElapsed": 0.020969053,
			"timeReceived": "2021-05-31T02:35:11.458880504Z",
			"type": "Error"
		},
		"receivedAt": 1622428511616,
		"requestPayload": "{\"from\":\"0x91d2b4381a4cd5c7c0f27565a7d4b829844c8635\",\"gas\":0,\"gasPrice\":0,\"headers\":{\"id\":\"6fb94fff-81d3-4094-567d-e031b1871694\",\"type\":\"SendTransaction\"},\"method\":{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"txnId\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"batchId\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"payloadRef\",\"type\":\"bytes32\"}],\"name\":\"broadcastBatch\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},\"params\":[\"12345\",\"!\",\"!\"],\"to\":\"0xd3266a857285fb75eb7df37353b4a15c8bb828f5\",\"value\":0}"
	}`)

	em := e.callbacks.(*blockchainmocks.Callbacks)
	txsu := em.On("BlockchainOpUpdate",
		operationID,
		fftypes.OpStatusFailed,
		"",
		"Packing arguments for method 'broadcastBatch': abi: cannot use [3]uint8 as type [32]uint8 as argument",
		mock.Anything).Return(fmt.Errorf("Shutdown"))
	done := make(chan struct{})
	txsu.RunFn = func(a mock.Arguments) {
		close(done)
	}

	go e.eventLoop()
	r <- []byte(`!badjson`)        // ignored bad json
	r <- []byte(`"not an object"`) // ignored wrong type
	r <- data.Bytes()
	<-done
}

func TestHandleReceiptNoRequestID(t *testing.T) {
	em := &blockchainmocks.Callbacks{}
	wsm := &wsmocks.WSClient{}
	e := &Ethereum{
		ctx:       context.Background(),
		topic:     "topic1",
		callbacks: em,
		wsconn:    wsm,
	}

	var reply fftypes.JSONObject
	data := fftypes.JSONAnyPtr(`{}`)
	err := json.Unmarshal(data.Bytes(), &reply)
	assert.NoError(t, err)
	err = e.handleReceipt(context.Background(), reply)
	assert.NoError(t, err)
}

func TestHandleReceiptBadRequestID(t *testing.T) {
	em := &blockchainmocks.Callbacks{}
	wsm := &wsmocks.WSClient{}
	e := &Ethereum{
		ctx:       context.Background(),
		topic:     "topic1",
		callbacks: em,
		wsconn:    wsm,
	}

	var reply fftypes.JSONObject
	data := fftypes.JSONAnyPtr(`{"headers":{"requestId":"1","type":"TransactionSuccess"}}`)
	err := json.Unmarshal(data.Bytes(), &reply)
	assert.NoError(t, err)
	err = e.handleReceipt(context.Background(), reply)
	assert.NoError(t, err)
}

func TestFormatNil(t *testing.T) {
	assert.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000000", ethHexFormatB32(nil))
}

func encodeDetails(internalType string) *fftypes.JSONAny {
	result, _ := json.Marshal(&paramDetails{Type: internalType})
	return fftypes.JSONAnyPtrBytes(result)
}

func TestAddSubscription(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	httpmock.ActivateNonDefault(e.client.GetClient())
	defer httpmock.DeactivateAndReset()
	e.initInfo.stream = &eventStream{
		ID: "es-1",
	}
	e.streams = &streamManager{
		client: e.client,
	}

	sub := &fftypes.ContractListenerInput{
		ContractListener: fftypes.ContractListener{
			Location: fftypes.JSONAnyPtr(fftypes.JSONObject{
				"address": "0x123",
			}.String()),
			Event: &fftypes.FFISerializedEvent{
				FFIEventDefinition: fftypes.FFIEventDefinition{
					Name: "Changed",
					Params: fftypes.FFIParams{
						{
							Name:   "value",
							Schema: fftypes.JSONAnyPtr(`{"type": "string", "details": {"type": "string"}}`),
						},
					},
				},
			},
			Options: &fftypes.ContractListenerOptions{
				FirstEvent: string(fftypes.SubOptsFirstEventNewest),
			},
		},
	}

	httpmock.RegisterResponder("POST", `http://localhost:12345/subscriptions`,
		httpmock.NewJsonResponderOrPanic(200, &subscription{}))

	err := e.AddContractListener(context.Background(), sub)

	assert.NoError(t, err)
}

func TestAddSubscriptionBadParamDetails(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	httpmock.ActivateNonDefault(e.client.GetClient())
	defer httpmock.DeactivateAndReset()
	e.initInfo.stream = &eventStream{
		ID: "es-1",
	}
	e.streams = &streamManager{
		client: e.client,
	}

	sub := &fftypes.ContractListenerInput{
		ContractListener: fftypes.ContractListener{
			Location: fftypes.JSONAnyPtr(fftypes.JSONObject{
				"address": "0x123",
			}.String()),
			Event: &fftypes.FFISerializedEvent{
				FFIEventDefinition: fftypes.FFIEventDefinition{
					Name: "Changed",
					Params: fftypes.FFIParams{
						{
							Name:   "value",
							Schema: fftypes.JSONAnyPtr(`{"type": "string", "details": {"type": ""}}`),
						},
					},
				},
			},
		},
	}

	httpmock.RegisterResponder("POST", `http://localhost:12345/subscriptions`,
		httpmock.NewJsonResponderOrPanic(200, &subscription{}))

	err := e.AddContractListener(context.Background(), sub)

	assert.Regexp(t, "FF10311", err)
}

func TestAddSubscriptionBadLocation(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	httpmock.ActivateNonDefault(e.client.GetClient())
	defer httpmock.DeactivateAndReset()

	e.initInfo.stream = &eventStream{
		ID: "es-1",
	}
	e.streams = &streamManager{
		client: e.client,
	}

	sub := &fftypes.ContractListenerInput{
		ContractListener: fftypes.ContractListener{
			Location: fftypes.JSONAnyPtr(""),
			Event:    &fftypes.FFISerializedEvent{},
		},
	}

	err := e.AddContractListener(context.Background(), sub)

	assert.Regexp(t, "FF10310", err)
}

func TestAddSubscriptionFail(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	httpmock.ActivateNonDefault(e.client.GetClient())
	defer httpmock.DeactivateAndReset()

	e.initInfo.stream = &eventStream{
		ID: "es-1",
	}
	e.streams = &streamManager{
		client: e.client,
	}

	sub := &fftypes.ContractListenerInput{
		ContractListener: fftypes.ContractListener{
			Location: fftypes.JSONAnyPtr(fftypes.JSONObject{
				"address": "0x123",
			}.String()),
			Event: &fftypes.FFISerializedEvent{},
			Options: &fftypes.ContractListenerOptions{
				FirstEvent: string(fftypes.SubOptsFirstEventNewest),
			},
		},
	}

	httpmock.RegisterResponder("POST", `http://localhost:12345/subscriptions`,
		httpmock.NewStringResponder(500, "pop"))

	err := e.AddContractListener(context.Background(), sub)

	assert.Regexp(t, "FF10111", err)
	assert.Regexp(t, "pop", err)
}

func TestDeleteSubscription(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	httpmock.ActivateNonDefault(e.client.GetClient())
	defer httpmock.DeactivateAndReset()

	e.initInfo.stream = &eventStream{
		ID: "es-1",
	}
	e.streams = &streamManager{
		client: e.client,
	}

	sub := &fftypes.ContractListener{
		ProtocolID: "sb-1",
	}

	httpmock.RegisterResponder("DELETE", `http://localhost:12345/subscriptions/sb-1`,
		httpmock.NewStringResponder(204, ""))

	err := e.DeleteContractListener(context.Background(), sub)

	assert.NoError(t, err)
}

func TestDeleteSubscriptionFail(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	httpmock.ActivateNonDefault(e.client.GetClient())
	defer httpmock.DeactivateAndReset()

	e.initInfo.stream = &eventStream{
		ID: "es-1",
	}
	e.streams = &streamManager{
		client: e.client,
	}

	sub := &fftypes.ContractListener{
		ProtocolID: "sb-1",
	}

	httpmock.RegisterResponder("DELETE", `http://localhost:12345/subscriptions/sb-1`,
		httpmock.NewStringResponder(500, ""))

	err := e.DeleteContractListener(context.Background(), sub)

	assert.Regexp(t, "FF10111", err)
}

func TestHandleMessageContractEvent(t *testing.T) {
	data := fftypes.JSONAnyPtr(`
[
  {
		"address": "0x1C197604587F046FD40684A8f21f4609FB811A7b",
		"blockNumber": "38011",
		"transactionIndex": "0x0",
		"transactionHash": "0xc26df2bf1a733e9249372d61eb11bd8662d26c8129df76890b1beb2f6fa72628",
		"data": {
			"from": "0x91D2B4381A4CD5C7C0F27565A7D4B829844C8635",
			"value": "1"
    },
		"subId": "sub2",
		"signature": "Changed(address,uint256)",
		"logIndex": "50",
		"timestamp": "1640811383"
  }
]`)

	em := &blockchainmocks.Callbacks{}
	e := &Ethereum{
		callbacks: em,
	}
	e.initInfo.sub = &subscription{
		ID: "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
	}

	em.On("BlockchainEvent", mock.MatchedBy(func(e *blockchain.EventWithSubscription) bool {
		assert.Equal(t, "0xc26df2bf1a733e9249372d61eb11bd8662d26c8129df76890b1beb2f6fa72628", e.BlockchainTXID)
		assert.Equal(t, "000000038011/000000/000050", e.Event.ProtocolID)
		return true
	})).Return(nil)

	var events []interface{}
	err := json.Unmarshal(data.Bytes(), &events)
	assert.NoError(t, err)
	err = e.handleMessageBatch(context.Background(), events)
	assert.NoError(t, err)

	ev := em.Calls[0].Arguments[0].(*blockchain.EventWithSubscription)
	assert.Equal(t, "sub2", ev.Subscription)
	assert.Equal(t, "Changed", ev.Event.Name)

	outputs := fftypes.JSONObject{
		"from":  "0x91D2B4381A4CD5C7C0F27565A7D4B829844C8635",
		"value": "1",
	}
	assert.Equal(t, outputs, ev.Event.Output)

	info := fftypes.JSONObject{
		"address":          "0x1C197604587F046FD40684A8f21f4609FB811A7b",
		"blockNumber":      "38011",
		"logIndex":         "50",
		"signature":        "Changed(address,uint256)",
		"subId":            "sub2",
		"transactionHash":  "0xc26df2bf1a733e9249372d61eb11bd8662d26c8129df76890b1beb2f6fa72628",
		"transactionIndex": "0x0",
		"timestamp":        "1640811383",
	}
	assert.Equal(t, info, ev.Event.Info)

	em.AssertExpectations(t)
}

func TestHandleMessageContractEventNoTimestamp(t *testing.T) {
	data := fftypes.JSONAnyPtr(`
[
  {
		"address": "0x1C197604587F046FD40684A8f21f4609FB811A7b",
		"blockNumber": "38011",
		"transactionIndex": "0x0",
		"transactionHash": "0xc26df2bf1a733e9249372d61eb11bd8662d26c8129df76890b1beb2f6fa72628",
		"data": {
			"from": "0x91D2B4381A4CD5C7C0F27565A7D4B829844C8635",
			"value": "1"
    },
		"subId": "sub2",
		"signature": "Changed(address,uint256)",
		"logIndex": "50"
  }
]`)

	em := &blockchainmocks.Callbacks{}
	e := &Ethereum{
		callbacks: em,
	}
	e.initInfo.sub = &subscription{
		ID: "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
	}

	em.On("BlockchainEvent", mock.Anything).Return(nil)

	var events []interface{}
	err := json.Unmarshal(data.Bytes(), &events)
	assert.NoError(t, err)
	err = e.handleMessageBatch(context.Background(), events)
	assert.Regexp(t, "FF10165", err)
}

func TestHandleMessageContractEventError(t *testing.T) {
	data := fftypes.JSONAnyPtr(`
[
  {
		"address": "0x1C197604587F046FD40684A8f21f4609FB811A7b",
		"blockNumber": "38011",
		"transactionIndex": "0x0",
		"transactionHash": "0xc26df2bf1a733e9249372d61eb11bd8662d26c8129df76890b1beb2f6fa72628",
		"data": {
			"from": "0x91D2B4381A4CD5C7C0F27565A7D4B829844C8635",
			"value": "1"
    },
		"subId": "sub2",
		"signature": "Changed(address,uint256)",
		"logIndex": "50",
		"timestamp": "1640811383"
  }
]`)

	em := &blockchainmocks.Callbacks{}
	e := &Ethereum{
		callbacks: em,
	}
	e.initInfo.sub = &subscription{
		ID: "sb-b5b97a4e-a317-4053-6400-1474650efcb5",
	}

	em.On("BlockchainEvent", mock.Anything).Return(fmt.Errorf("pop"))

	var events []interface{}
	err := json.Unmarshal(data.Bytes(), &events)
	assert.NoError(t, err)
	err = e.handleMessageBatch(context.Background(), events)
	assert.EqualError(t, err, "pop")

	em.AssertExpectations(t)
}

func TestInvokeContractOK(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	httpmock.ActivateNonDefault(e.client.GetClient())
	defer httpmock.DeactivateAndReset()
	signingKey := ethHexFormatB32(fftypes.NewRandB32())
	location := &Location{
		Address: "0x12345",
	}
	method := testFFIMethod()
	params := map[string]interface{}{
		"x": float64(1),
		"y": float64(2),
	}
	locationBytes, err := json.Marshal(location)
	assert.NoError(t, err)
	httpmock.RegisterResponder("POST", `http://localhost:12345/`,
		func(req *http.Request) (*http.Response, error) {
			var body map[string]interface{}
			json.NewDecoder(req.Body).Decode(&body)
			params := body["params"].([]interface{})
			headers := body["headers"].(map[string]interface{})
			assert.Equal(t, "SendTransaction", headers["type"])
			assert.Equal(t, float64(1), params[0])
			assert.Equal(t, float64(2), params[1])
			return httpmock.NewJsonResponderOrPanic(200, "")(req)
		})
	err = e.InvokeContract(context.Background(), nil, signingKey, fftypes.JSONAnyPtrBytes(locationBytes), method, params)
	assert.NoError(t, err)
}

func TestInvokeContractAddressNotSet(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	signingKey := ethHexFormatB32(fftypes.NewRandB32())
	location := &Location{}
	method := testFFIMethod()
	params := map[string]interface{}{
		"x": float64(1),
		"y": float64(2),
	}
	locationBytes, err := json.Marshal(location)
	assert.NoError(t, err)
	err = e.InvokeContract(context.Background(), nil, signingKey, fftypes.JSONAnyPtrBytes(locationBytes), method, params)
	assert.Regexp(t, "'address' not set", err)
}

func TestInvokeContractEthconnectError(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	httpmock.ActivateNonDefault(e.client.GetClient())
	defer httpmock.DeactivateAndReset()
	signingKey := ethHexFormatB32(fftypes.NewRandB32())
	location := &Location{
		Address: "0x12345",
	}
	method := testFFIMethod()
	params := map[string]interface{}{
		"x": float64(1),
		"y": float64(2),
	}
	locationBytes, err := json.Marshal(location)
	assert.NoError(t, err)
	httpmock.RegisterResponder("POST", `http://localhost:12345/`,
		func(req *http.Request) (*http.Response, error) {
			return httpmock.NewJsonResponderOrPanic(400, "")(req)
		})
	err = e.InvokeContract(context.Background(), nil, signingKey, fftypes.JSONAnyPtrBytes(locationBytes), method, params)
	assert.Regexp(t, "FF10111", err)
}

func TestInvokeContractPrepareFail(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	httpmock.ActivateNonDefault(e.client.GetClient())
	defer httpmock.DeactivateAndReset()
	signingKey := ethHexFormatB32(fftypes.NewRandB32())
	location := &Location{
		Address: "0x12345",
	}
	method := &fftypes.FFIMethod{
		Name: "set",
		Params: fftypes.FFIParams{
			{
				Schema: fftypes.JSONAnyPtr("{bad schema!"),
			},
		},
	}
	params := map[string]interface{}{
		"x": float64(1),
		"y": float64(2),
	}
	locationBytes, err := json.Marshal(location)
	assert.NoError(t, err)
	err = e.InvokeContract(context.Background(), nil, signingKey, fftypes.JSONAnyPtrBytes(locationBytes), method, params)
	assert.Regexp(t, "invalid json", err)
}

func TestQueryContractOK(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	httpmock.ActivateNonDefault(e.client.GetClient())
	defer httpmock.DeactivateAndReset()
	location := &Location{
		Address: "0x12345",
	}
	method := testFFIMethod()
	params := map[string]interface{}{}
	locationBytes, err := json.Marshal(location)
	assert.NoError(t, err)
	httpmock.RegisterResponder("POST", `http://localhost:12345/`,
		func(req *http.Request) (*http.Response, error) {
			var body map[string]interface{}
			json.NewDecoder(req.Body).Decode(&body)
			headers := body["headers"].(map[string]interface{})
			assert.Equal(t, "Query", headers["type"])
			return httpmock.NewJsonResponderOrPanic(200, queryOutput{Output: "3"})(req)
		})
	result, err := e.QueryContract(context.Background(), fftypes.JSONAnyPtrBytes(locationBytes), method, params)
	assert.NoError(t, err)
	j, err := json.Marshal(result)
	assert.NoError(t, err)
	assert.Equal(t, `{"output":"3"}`, string(j))
}

func TestQueryContractErrorPrepare(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	httpmock.ActivateNonDefault(e.client.GetClient())
	defer httpmock.DeactivateAndReset()
	location := &Location{
		Address: "0x12345",
	}
	method := &fftypes.FFIMethod{
		Params: fftypes.FFIParams{
			{
				Name:   "bad",
				Schema: fftypes.JSONAnyPtr("{badschema}"),
			},
		},
	}
	params := map[string]interface{}{}
	locationBytes, err := json.Marshal(location)
	assert.NoError(t, err)
	_, err = e.QueryContract(context.Background(), fftypes.JSONAnyPtrBytes(locationBytes), method, params)
	assert.Regexp(t, "invalid json", err)
}

func TestQueryContractAddressNotSet(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	location := &Location{}
	method := testFFIMethod()
	params := map[string]interface{}{
		"x": float64(1),
		"y": float64(2),
	}
	locationBytes, err := json.Marshal(location)
	assert.NoError(t, err)
	_, err = e.QueryContract(context.Background(), fftypes.JSONAnyPtrBytes(locationBytes), method, params)
	assert.Regexp(t, "'address' not set", err)
}

func TestQueryContractEthconnectError(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	httpmock.ActivateNonDefault(e.client.GetClient())
	defer httpmock.DeactivateAndReset()
	location := &Location{
		Address: "0x12345",
	}
	method := testFFIMethod()
	params := map[string]interface{}{
		"x": float64(1),
		"y": float64(2),
	}
	locationBytes, err := json.Marshal(location)
	assert.NoError(t, err)
	httpmock.RegisterResponder("POST", `http://localhost:12345/`,
		func(req *http.Request) (*http.Response, error) {
			return httpmock.NewJsonResponderOrPanic(400, queryOutput{})(req)
		})
	_, err = e.QueryContract(context.Background(), fftypes.JSONAnyPtrBytes(locationBytes), method, params)
	assert.Regexp(t, "FF10111", err)
}

func TestQueryContractUnmarshalResponseError(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	httpmock.ActivateNonDefault(e.client.GetClient())
	defer httpmock.DeactivateAndReset()
	location := &Location{
		Address: "0x12345",
	}
	method := testFFIMethod()
	params := map[string]interface{}{
		"x": float64(1),
		"y": float64(2),
	}
	locationBytes, err := json.Marshal(location)
	assert.NoError(t, err)
	httpmock.RegisterResponder("POST", `http://localhost:12345/`,
		func(req *http.Request) (*http.Response, error) {
			var body map[string]interface{}
			json.NewDecoder(req.Body).Decode(&body)
			headers := body["headers"].(map[string]interface{})
			assert.Equal(t, "Query", headers["type"])
			return httpmock.NewStringResponder(200, "[definitely not JSON}")(req)
		})
	_, err = e.QueryContract(context.Background(), fftypes.JSONAnyPtrBytes(locationBytes), method, params)
	assert.Regexp(t, "invalid character", err)
}

func TestValidateContractLocation(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()
	location := &Location{
		Address: "0x12345",
	}
	locationBytes, err := json.Marshal(location)
	assert.NoError(t, err)
	err = e.ValidateContractLocation(context.Background(), fftypes.JSONAnyPtrBytes(locationBytes))
	assert.NoError(t, err)
}

func TestGetContractAddressBadJSON(t *testing.T) {
	e, cancel := newTestEthereum()
	defer cancel()

	mockedClient := &http.Client{}
	httpmock.ActivateNonDefault(mockedClient)
	defer httpmock.DeactivateAndReset()

	httpmock.RegisterResponder("GET", "http://localhost:12345/contracts/firefly",
		httpmock.NewBytesResponder(200, []byte("{not json!")),
	)

	resetConf()
	utEthconnectConf.Set(restclient.HTTPConfigURL, "http://localhost:12345")
	utEthconnectConf.Set(restclient.HTTPCustomClient, mockedClient)
	utEthconnectConf.Set(EthconnectConfigInstancePath, "0x12345")
	utEthconnectConf.Set(EthconnectConfigTopic, "topic1")

	e.client = restclient.New(e.ctx, utEthconnectConf)

	_, err := e.getContractAddress(context.Background(), "/contracts/firefly")

	assert.Regexp(t, "invalid character 'n' looking for beginning of object key string", err)
}

func TestFFIMethodToABI(t *testing.T) {
	e, _ := newTestEthereum()

	method := &fftypes.FFIMethod{
		Name: "set",
		Params: []*fftypes.FFIParam{
			{
				Name: "newValue",
				Schema: fftypes.JSONAnyPtr(`{
					"type": "integer",
					"details": {
						"type": "uint256"
					}
				}`),
			},
		},
		Returns: []*fftypes.FFIParam{},
	}

	expectedABIElement := ABIElementMarshaling{
		Name: "set",
		Type: "function",
		Inputs: []ABIArgumentMarshaling{
			{
				Name:    "newValue",
				Type:    "uint256",
				Indexed: false,
			},
		},
		Outputs: []ABIArgumentMarshaling{},
	}

	abi, err := e.FFIMethodToABI(context.Background(), method)
	assert.NoError(t, err)
	assert.Equal(t, expectedABIElement, abi)
}

func TestFFIMethodToABIObject(t *testing.T) {
	e, _ := newTestEthereum()

	method := &fftypes.FFIMethod{
		Name: "set",
		Params: []*fftypes.FFIParam{
			{
				Name: "widget",
				Schema: fftypes.JSONAnyPtr(`{
					"type": "object",
					"details": {
						"type": "tuple"
					},
					"properties": {
						"radius": {
							"type": "integer",
							"details": {
								"type": "uint256",
								"index": 0,
								"indexed": true
							}
						},
						"numbers": {
							"type": "array",
							"details": {
								"type": "uint256[]",
								"index": 1
							},
							"items": {
								"type": "integer",
								"details": {
									"type": "uint256"
								}
							}
						}
					}
				}`),
			},
		},
		Returns: []*fftypes.FFIParam{},
	}

	expectedABIElement := ABIElementMarshaling{
		Name: "set",
		Type: "function",
		Inputs: []ABIArgumentMarshaling{
			{
				Name:    "widget",
				Type:    "tuple",
				Indexed: false,
				Components: []ABIArgumentMarshaling{
					{
						Name:         "radius",
						Type:         "uint256",
						Indexed:      true,
						InternalType: "",
					},
					{
						Name:         "numbers",
						Type:         "uint256[]",
						Indexed:      false,
						InternalType: "",
					},
				},
			},
		},
		Outputs: []ABIArgumentMarshaling{},
	}

	abi, err := e.FFIMethodToABI(context.Background(), method)
	assert.NoError(t, err)
	assert.Equal(t, expectedABIElement, abi)
}

func TestFFIMethodToABINestedArray(t *testing.T) {
	e, _ := newTestEthereum()

	method := &fftypes.FFIMethod{
		Name: "set",
		Params: []*fftypes.FFIParam{
			{
				Name: "widget",
				Schema: fftypes.JSONAnyPtr(`{
					"type": "array",
					"details": {
						"type": "string[][]",
						"internalType": "string[][]"
					},
					"items": {
						"type": "array",
						"items": {
							"type": "string"
						}
					}
				}`),
			},
		},
		Returns: []*fftypes.FFIParam{},
	}

	expectedABIElement := ABIElementMarshaling{
		Name: "set",
		Type: "function",
		Inputs: []ABIArgumentMarshaling{
			{
				Name:         "widget",
				Type:         "string[][]",
				InternalType: "string[][]",
				Indexed:      false,
			},
		},
		Outputs: []ABIArgumentMarshaling{},
	}

	abi, err := e.FFIMethodToABI(context.Background(), method)
	assert.NoError(t, err)
	assert.Equal(t, expectedABIElement, abi)
}

func TestFFIMethodToABIInvalidJSON(t *testing.T) {
	e, _ := newTestEthereum()

	method := &fftypes.FFIMethod{
		Name: "set",
		Params: []*fftypes.FFIParam{
			{
				Name:   "newValue",
				Schema: fftypes.JSONAnyPtr(`{#!`),
			},
		},
		Returns: []*fftypes.FFIParam{},
	}

	_, err := e.FFIMethodToABI(context.Background(), method)
	assert.Regexp(t, "invalid json", err)
}

func TestFFIMethodToABIBadSchema(t *testing.T) {
	e, _ := newTestEthereum()

	method := &fftypes.FFIMethod{
		Name: "set",
		Params: []*fftypes.FFIParam{
			{
				Name: "newValue",
				Schema: fftypes.JSONAnyPtr(`{
					"type": "integer",
					"detailz": {
						"type": "uint256"
					}
				}`),
			},
		},
		Returns: []*fftypes.FFIParam{},
	}

	_, err := e.FFIMethodToABI(context.Background(), method)
	assert.Regexp(t, "compilation failed", err)
}

func TestFFIMethodToABIBadReturn(t *testing.T) {
	e, _ := newTestEthereum()

	method := &fftypes.FFIMethod{
		Name:   "set",
		Params: []*fftypes.FFIParam{},
		Returns: []*fftypes.FFIParam{
			{
				Name: "newValue",
				Schema: fftypes.JSONAnyPtr(`{
					"type": "integer",
					"detailz": {
						"type": "uint256"
					}
				}`),
			},
		},
	}

	_, err := e.FFIMethodToABI(context.Background(), method)
	assert.Regexp(t, "compilation failed", err)
}

func TestConvertABIToFFI(t *testing.T) {
	e, _ := newTestEthereum()

	abi := []ABIElementMarshaling{
		{
			Name: "set",
			Type: "function",
			Inputs: []ABIArgumentMarshaling{
				{
					Name:         "newValue",
					Type:         "uint256",
					InternalType: "uint256",
				},
			},
			Outputs: []ABIArgumentMarshaling{},
		},
		{
			Name:   "get",
			Type:   "function",
			Inputs: []ABIArgumentMarshaling{},
			Outputs: []ABIArgumentMarshaling{
				{
					Name:         "value",
					Type:         "uint256",
					InternalType: "uint256",
				},
			},
		},
		{
			Name: "Updated",
			Type: "event",
			Inputs: []ABIArgumentMarshaling{{
				Name:         "value",
				Type:         "uint256",
				InternalType: "uint256",
			}},
			Outputs: []ABIArgumentMarshaling{},
		},
	}

	schema := fftypes.JSONAnyPtr(`{"type":"integer","details":{"type":"uint256","internalType":"uint256"}}`)

	expectedFFI := &fftypes.FFI{
		Name:        "SimpleStorage",
		Version:     "v0.0.1",
		Namespace:   "default",
		Description: "desc",
		Methods: []*fftypes.FFIMethod{
			{
				Name: "set",
				Params: fftypes.FFIParams{
					{
						Name:   "newValue",
						Schema: schema,
					},
				},
				Returns: fftypes.FFIParams{},
			},
			{
				Name:   "get",
				Params: fftypes.FFIParams{},
				Returns: fftypes.FFIParams{
					{
						Name:   "value",
						Schema: schema,
					},
				},
			},
		},
		Events: []*fftypes.FFIEvent{
			{
				FFIEventDefinition: fftypes.FFIEventDefinition{
					Name: "Updated",
					Params: fftypes.FFIParams{
						{
							Name:   "value",
							Schema: schema,
						},
					},
				},
			},
		},
	}

	actualFFI := e.convertABIToFFI("default", "SimpleStorage", "v0.0.1", "desc", abi)
	assert.NotNil(t, actualFFI)
	assert.Equal(t, expectedFFI, actualFFI)
}

func TestConvertABIToFFIWithObject(t *testing.T) {
	e, _ := newTestEthereum()

	abi := []ABIElementMarshaling{
		{
			Name: "set",
			Type: "function",
			Inputs: []ABIArgumentMarshaling{
				{
					Name:         "newValue",
					Type:         "tuple",
					InternalType: "struct WidgetFactory.Widget",
					Components: []ABIArgumentMarshaling{
						{
							Name:         "size",
							Type:         "uint256",
							InternalType: "uint256",
						},
						{
							Name:         "description",
							Type:         "string",
							InternalType: "string",
						},
					},
				},
			},
			Outputs: []ABIArgumentMarshaling{},
		},
	}

	schema := fftypes.JSONAnyPtr(`{"type":"object","details":{"type":"tuple","internalType":"struct WidgetFactory.Widget"},"properties":{"description":{"type":"string","details":{"type":"string","internalType":"string","index":1}},"size":{"type":"integer","details":{"type":"uint256","internalType":"uint256","index":0}}}}`)

	expectedFFI := &fftypes.FFI{
		Name:        "WidgetTest",
		Version:     "v0.0.1",
		Namespace:   "default",
		Description: "desc",
		Methods: []*fftypes.FFIMethod{
			{
				Name: "set",
				Params: fftypes.FFIParams{
					{
						Name:   "newValue",
						Schema: schema,
					},
				},
				Returns: fftypes.FFIParams{},
			},
		},
		Events: []*fftypes.FFIEvent{},
	}

	actualFFI := e.convertABIToFFI("default", "WidgetTest", "v0.0.1", "desc", abi)
	assert.NotNil(t, actualFFI)
	assert.Equal(t, expectedFFI, actualFFI)
}

func TestConvertABIToFFIWithArray(t *testing.T) {
	e, _ := newTestEthereum()

	abi := []ABIElementMarshaling{
		{
			Name: "set",
			Type: "function",
			Inputs: []ABIArgumentMarshaling{
				{
					Name:         "newValue",
					Type:         "string[]",
					InternalType: "string[]",
				},
			},
			Outputs: []ABIArgumentMarshaling{},
		},
	}

	schema := fftypes.JSONAnyPtr(`{"type":"array","details":{"type":"string[]","internalType":"string[]"},"items":{"type":"string"}}`)

	expectedFFI := &fftypes.FFI{
		Name:        "WidgetTest",
		Version:     "v0.0.1",
		Namespace:   "default",
		Description: "desc",
		Methods: []*fftypes.FFIMethod{
			{
				Name: "set",
				Params: fftypes.FFIParams{
					{
						Name:   "newValue",
						Schema: schema,
					},
				},
				Returns: fftypes.FFIParams{},
			},
		},
		Events: []*fftypes.FFIEvent{},
	}

	actualFFI := e.convertABIToFFI("default", "WidgetTest", "v0.0.1", "desc", abi)
	assert.NotNil(t, actualFFI)
	assert.Equal(t, expectedFFI, actualFFI)
}

func TestConvertABIToFFIWithNestedArray(t *testing.T) {
	e, _ := newTestEthereum()

	abi := []ABIElementMarshaling{
		{
			Name: "set",
			Type: "function",
			Inputs: []ABIArgumentMarshaling{
				{
					Name:         "newValue",
					Type:         "string[][]",
					InternalType: "string[][]",
				},
			},
			Outputs: []ABIArgumentMarshaling{},
		},
	}

	schema := fftypes.JSONAnyPtr(`{"type":"array","details":{"type":"string[][]","internalType":"string[][]"},"items":{"type":"array","items":{"type":"string"}}}`)
	expectedFFI := &fftypes.FFI{
		Name:        "WidgetTest",
		Version:     "v0.0.1",
		Namespace:   "default",
		Description: "desc",
		Methods: []*fftypes.FFIMethod{
			{
				Name: "set",
				Params: fftypes.FFIParams{
					{
						Name:   "newValue",
						Schema: schema,
					},
				},
				Returns: fftypes.FFIParams{},
			},
		},
		Events: []*fftypes.FFIEvent{},
	}

	actualFFI := e.convertABIToFFI("default", "WidgetTest", "v0.0.1", "desc", abi)
	assert.NotNil(t, actualFFI)
	assert.Equal(t, expectedFFI, actualFFI)
}

func TestConvertABIToFFIWithNestedArrayOfObjects(t *testing.T) {
	e, _ := newTestEthereum()

	abi := []ABIElementMarshaling{
		{
			Name: "set",
			Type: "function",
			Inputs: []ABIArgumentMarshaling{
				{
					InternalType: "struct WidgetFactory.Widget[][]",
					Name:         "gears",
					Type:         "tuple[][]",
					Components: []ABIArgumentMarshaling{
						{
							InternalType: "string",
							Name:         "description",
							Type:         "string",
						},
						{
							InternalType: "uint256",
							Name:         "size",
							Type:         "uint256",
						},
						{
							InternalType: "bool",
							Name:         "inUse",
							Type:         "bool",
						},
					},
				},
			},
			Outputs: []ABIArgumentMarshaling{},
		},
	}

	schema := fftypes.JSONAnyPtr(`{"type":"array","details":{"type":"tuple[][]","internalType":"struct WidgetFactory.Widget[][]"},"items":{"type":"array","items":{"type":"object","properties":{"description":{"type":"string","details":{"type":"string","internalType":"string","index":0}},"inUse":{"type":"boolean","details":{"type":"bool","internalType":"bool","index":2}},"size":{"type":"integer","details":{"type":"uint256","internalType":"uint256","index":1}}}}}}`)
	expectedFFI := &fftypes.FFI{
		Name:        "WidgetTest",
		Version:     "v0.0.1",
		Namespace:   "default",
		Description: "desc",
		Methods: []*fftypes.FFIMethod{
			{
				Name: "set",
				Params: fftypes.FFIParams{
					{
						Name:   "gears",
						Schema: schema,
					},
				},
				Returns: fftypes.FFIParams{},
			},
		},
		Events: []*fftypes.FFIEvent{},
	}

	actualFFI := e.convertABIToFFI("default", "WidgetTest", "v0.0.1", "desc", abi)
	assert.NotNil(t, actualFFI)
	assert.Equal(t, expectedFFI, actualFFI)
}

func TestGenerateFFI(t *testing.T) {
	e, _ := newTestEthereum()
	_, err := e.GenerateFFI(context.Background(), &fftypes.FFIGenerationRequest{
		Name:        "Simple",
		Version:     "v0.0.1",
		Description: "desc",
		Input:       fftypes.JSONAnyPtr(`{"abi": [{}]}`),
	})
	assert.NoError(t, err)
}

func TestGenerateFFIInlineNamespace(t *testing.T) {
	e, _ := newTestEthereum()
	ffi, err := e.GenerateFFI(context.Background(), &fftypes.FFIGenerationRequest{
		Name:        "Simple",
		Version:     "v0.0.1",
		Description: "desc",
		Namespace:   "ns1",
		Input:       fftypes.JSONAnyPtr(`{"abi":[{}]}`),
	})
	assert.NoError(t, err)
	assert.Equal(t, ffi.Namespace, "ns1")
}

func TestGenerateFFIEmptyABI(t *testing.T) {
	e, _ := newTestEthereum()
	_, err := e.GenerateFFI(context.Background(), &fftypes.FFIGenerationRequest{
		Name:        "Simple",
		Version:     "v0.0.1",
		Description: "desc",
		Input:       fftypes.JSONAnyPtr(`{"abi": []}`),
	})
	assert.Regexp(t, "FF10346", err)
}

func TestGenerateFFIBadABI(t *testing.T) {
	e, _ := newTestEthereum()
	_, err := e.GenerateFFI(context.Background(), &fftypes.FFIGenerationRequest{
		Name:        "Simple",
		Version:     "v0.0.1",
		Description: "desc",
		Input:       fftypes.JSONAnyPtr(`{"abi": "not an array"}`),
	})
	assert.Regexp(t, "FF10346", err)
}

func TestGetFFIType(t *testing.T) {
	e, _ := newTestEthereum()
	assert.Equal(t, e.getFFIType("string"), "string")
	assert.Equal(t, e.getFFIType("address"), "string")
	assert.Equal(t, e.getFFIType("byte"), "string")
	assert.Equal(t, e.getFFIType("bool"), "boolean")
	assert.Equal(t, e.getFFIType("uint256"), "integer")
	assert.Equal(t, e.getFFIType("string[]"), "array")
	assert.Equal(t, e.getFFIType("tuple"), "object")
	assert.Equal(t, e.getFFIType("foobar"), "")
}
