// Code generated by mockery v1.0.0. DO NOT EDIT.

package blockchainmocks

import (
	config "github.com/hyperledger/firefly/internal/config"
	blockchain "github.com/hyperledger/firefly/pkg/blockchain"

	context "context"

	fftypes "github.com/hyperledger/firefly/pkg/fftypes"

	metrics "github.com/hyperledger/firefly/internal/metrics"

	mock "github.com/stretchr/testify/mock"
)

// Plugin is an autogenerated mock type for the Plugin type
type Plugin struct {
	mock.Mock
}

// AddContractListener provides a mock function with given fields: ctx, subscription
func (_m *Plugin) AddContractListener(ctx context.Context, subscription *fftypes.ContractListenerInput) error {
	ret := _m.Called(ctx, subscription)

	var r0 error
	if rf, ok := ret.Get(0).(func(context.Context, *fftypes.ContractListenerInput) error); ok {
		r0 = rf(ctx, subscription)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// Capabilities provides a mock function with given fields:
func (_m *Plugin) Capabilities() *blockchain.Capabilities {
	ret := _m.Called()

	var r0 *blockchain.Capabilities
	if rf, ok := ret.Get(0).(func() *blockchain.Capabilities); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*blockchain.Capabilities)
		}
	}

	return r0
}

// DeleteContractListener provides a mock function with given fields: ctx, subscription
func (_m *Plugin) DeleteContractListener(ctx context.Context, subscription *fftypes.ContractListener) error {
	ret := _m.Called(ctx, subscription)

	var r0 error
	if rf, ok := ret.Get(0).(func(context.Context, *fftypes.ContractListener) error); ok {
		r0 = rf(ctx, subscription)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// GenerateFFI provides a mock function with given fields: ctx, generationRequest
func (_m *Plugin) GenerateFFI(ctx context.Context, generationRequest *fftypes.FFIGenerationRequest) (*fftypes.FFI, error) {
	ret := _m.Called(ctx, generationRequest)

	var r0 *fftypes.FFI
	if rf, ok := ret.Get(0).(func(context.Context, *fftypes.FFIGenerationRequest) *fftypes.FFI); ok {
		r0 = rf(ctx, generationRequest)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*fftypes.FFI)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, *fftypes.FFIGenerationRequest) error); ok {
		r1 = rf(ctx, generationRequest)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// GetFFIParamValidator provides a mock function with given fields: ctx
func (_m *Plugin) GetFFIParamValidator(ctx context.Context) (fftypes.FFIParamValidator, error) {
	ret := _m.Called(ctx)

	var r0 fftypes.FFIParamValidator
	if rf, ok := ret.Get(0).(func(context.Context) fftypes.FFIParamValidator); ok {
		r0 = rf(ctx)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(fftypes.FFIParamValidator)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context) error); ok {
		r1 = rf(ctx)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// Init provides a mock function with given fields: ctx, prefix, callbacks, _a3
func (_m *Plugin) Init(ctx context.Context, prefix config.Prefix, callbacks blockchain.Callbacks, _a3 metrics.Manager) error {
	ret := _m.Called(ctx, prefix, callbacks, _a3)

	var r0 error
	if rf, ok := ret.Get(0).(func(context.Context, config.Prefix, blockchain.Callbacks, metrics.Manager) error); ok {
		r0 = rf(ctx, prefix, callbacks, _a3)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// InitPrefix provides a mock function with given fields: prefix
func (_m *Plugin) InitPrefix(prefix config.Prefix) {
	_m.Called(prefix)
}

// InvokeContract provides a mock function with given fields: ctx, operationID, signingKey, location, method, input
func (_m *Plugin) InvokeContract(ctx context.Context, operationID *fftypes.UUID, signingKey string, location *fftypes.JSONAny, method *fftypes.FFIMethod, input map[string]interface{}) error {
	ret := _m.Called(ctx, operationID, signingKey, location, method, input)

	var r0 error
	if rf, ok := ret.Get(0).(func(context.Context, *fftypes.UUID, string, *fftypes.JSONAny, *fftypes.FFIMethod, map[string]interface{}) error); ok {
		r0 = rf(ctx, operationID, signingKey, location, method, input)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// Name provides a mock function with given fields:
func (_m *Plugin) Name() string {
	ret := _m.Called()

	var r0 string
	if rf, ok := ret.Get(0).(func() string); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(string)
	}

	return r0
}

// NormalizeSigningKey provides a mock function with given fields: ctx, keyRef
func (_m *Plugin) NormalizeSigningKey(ctx context.Context, keyRef string) (string, error) {
	ret := _m.Called(ctx, keyRef)

	var r0 string
	if rf, ok := ret.Get(0).(func(context.Context, string) string); ok {
		r0 = rf(ctx, keyRef)
	} else {
		r0 = ret.Get(0).(string)
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, string) error); ok {
		r1 = rf(ctx, keyRef)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// QueryContract provides a mock function with given fields: ctx, location, method, input
func (_m *Plugin) QueryContract(ctx context.Context, location *fftypes.JSONAny, method *fftypes.FFIMethod, input map[string]interface{}) (interface{}, error) {
	ret := _m.Called(ctx, location, method, input)

	var r0 interface{}
	if rf, ok := ret.Get(0).(func(context.Context, *fftypes.JSONAny, *fftypes.FFIMethod, map[string]interface{}) interface{}); ok {
		r0 = rf(ctx, location, method, input)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(interface{})
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, *fftypes.JSONAny, *fftypes.FFIMethod, map[string]interface{}) error); ok {
		r1 = rf(ctx, location, method, input)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// Start provides a mock function with given fields:
func (_m *Plugin) Start() error {
	ret := _m.Called()

	var r0 error
	if rf, ok := ret.Get(0).(func() error); ok {
		r0 = rf()
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// SubmitBatchPin provides a mock function with given fields: ctx, operationID, ledgerID, signingKey, batch
func (_m *Plugin) SubmitBatchPin(ctx context.Context, operationID *fftypes.UUID, ledgerID *fftypes.UUID, signingKey string, batch *blockchain.BatchPin) error {
	ret := _m.Called(ctx, operationID, ledgerID, signingKey, batch)

	var r0 error
	if rf, ok := ret.Get(0).(func(context.Context, *fftypes.UUID, *fftypes.UUID, string, *blockchain.BatchPin) error); ok {
		r0 = rf(ctx, operationID, ledgerID, signingKey, batch)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// VerifierType provides a mock function with given fields:
func (_m *Plugin) VerifierType() fftypes.FFEnum {
	ret := _m.Called()

	var r0 fftypes.FFEnum
	if rf, ok := ret.Get(0).(func() fftypes.FFEnum); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(fftypes.FFEnum)
	}

	return r0
}
