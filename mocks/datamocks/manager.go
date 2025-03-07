// Code generated by mockery v1.0.0. DO NOT EDIT.

package datamocks

import (
	context "context"

	data "github.com/hyperledger/firefly/internal/data"
	fftypes "github.com/hyperledger/firefly/pkg/fftypes"

	io "io"

	mock "github.com/stretchr/testify/mock"
)

// Manager is an autogenerated mock type for the Manager type
type Manager struct {
	mock.Mock
}

// CheckDatatype provides a mock function with given fields: ctx, ns, datatype
func (_m *Manager) CheckDatatype(ctx context.Context, ns string, datatype *fftypes.Datatype) error {
	ret := _m.Called(ctx, ns, datatype)

	var r0 error
	if rf, ok := ret.Get(0).(func(context.Context, string, *fftypes.Datatype) error); ok {
		r0 = rf(ctx, ns, datatype)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// DownloadBLOB provides a mock function with given fields: ctx, ns, dataID
func (_m *Manager) DownloadBLOB(ctx context.Context, ns string, dataID string) (*fftypes.Blob, io.ReadCloser, error) {
	ret := _m.Called(ctx, ns, dataID)

	var r0 *fftypes.Blob
	if rf, ok := ret.Get(0).(func(context.Context, string, string) *fftypes.Blob); ok {
		r0 = rf(ctx, ns, dataID)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*fftypes.Blob)
		}
	}

	var r1 io.ReadCloser
	if rf, ok := ret.Get(1).(func(context.Context, string, string) io.ReadCloser); ok {
		r1 = rf(ctx, ns, dataID)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(io.ReadCloser)
		}
	}

	var r2 error
	if rf, ok := ret.Get(2).(func(context.Context, string, string) error); ok {
		r2 = rf(ctx, ns, dataID)
	} else {
		r2 = ret.Error(2)
	}

	return r0, r1, r2
}

// GetMessageDataCached provides a mock function with given fields: ctx, msg, options
func (_m *Manager) GetMessageDataCached(ctx context.Context, msg *fftypes.Message, options ...data.CacheReadOption) (fftypes.DataArray, bool, error) {
	_va := make([]interface{}, len(options))
	for _i := range options {
		_va[_i] = options[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, ctx, msg)
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	var r0 fftypes.DataArray
	if rf, ok := ret.Get(0).(func(context.Context, *fftypes.Message, ...data.CacheReadOption) fftypes.DataArray); ok {
		r0 = rf(ctx, msg, options...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(fftypes.DataArray)
		}
	}

	var r1 bool
	if rf, ok := ret.Get(1).(func(context.Context, *fftypes.Message, ...data.CacheReadOption) bool); ok {
		r1 = rf(ctx, msg, options...)
	} else {
		r1 = ret.Get(1).(bool)
	}

	var r2 error
	if rf, ok := ret.Get(2).(func(context.Context, *fftypes.Message, ...data.CacheReadOption) error); ok {
		r2 = rf(ctx, msg, options...)
	} else {
		r2 = ret.Error(2)
	}

	return r0, r1, r2
}

// GetMessageWithDataCached provides a mock function with given fields: ctx, msgID, options
func (_m *Manager) GetMessageWithDataCached(ctx context.Context, msgID *fftypes.UUID, options ...data.CacheReadOption) (*fftypes.Message, fftypes.DataArray, bool, error) {
	_va := make([]interface{}, len(options))
	for _i := range options {
		_va[_i] = options[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, ctx, msgID)
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	var r0 *fftypes.Message
	if rf, ok := ret.Get(0).(func(context.Context, *fftypes.UUID, ...data.CacheReadOption) *fftypes.Message); ok {
		r0 = rf(ctx, msgID, options...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*fftypes.Message)
		}
	}

	var r1 fftypes.DataArray
	if rf, ok := ret.Get(1).(func(context.Context, *fftypes.UUID, ...data.CacheReadOption) fftypes.DataArray); ok {
		r1 = rf(ctx, msgID, options...)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(fftypes.DataArray)
		}
	}

	var r2 bool
	if rf, ok := ret.Get(2).(func(context.Context, *fftypes.UUID, ...data.CacheReadOption) bool); ok {
		r2 = rf(ctx, msgID, options...)
	} else {
		r2 = ret.Get(2).(bool)
	}

	var r3 error
	if rf, ok := ret.Get(3).(func(context.Context, *fftypes.UUID, ...data.CacheReadOption) error); ok {
		r3 = rf(ctx, msgID, options...)
	} else {
		r3 = ret.Error(3)
	}

	return r0, r1, r2, r3
}

// HydrateBatch provides a mock function with given fields: ctx, persistedBatch
func (_m *Manager) HydrateBatch(ctx context.Context, persistedBatch *fftypes.BatchPersisted) (*fftypes.Batch, error) {
	ret := _m.Called(ctx, persistedBatch)

	var r0 *fftypes.Batch
	if rf, ok := ret.Get(0).(func(context.Context, *fftypes.BatchPersisted) *fftypes.Batch); ok {
		r0 = rf(ctx, persistedBatch)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*fftypes.Batch)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, *fftypes.BatchPersisted) error); ok {
		r1 = rf(ctx, persistedBatch)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// PeekMessageCache provides a mock function with given fields: ctx, id, options
func (_m *Manager) PeekMessageCache(ctx context.Context, id *fftypes.UUID, options ...data.CacheReadOption) (*fftypes.Message, fftypes.DataArray) {
	_va := make([]interface{}, len(options))
	for _i := range options {
		_va[_i] = options[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, ctx, id)
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	var r0 *fftypes.Message
	if rf, ok := ret.Get(0).(func(context.Context, *fftypes.UUID, ...data.CacheReadOption) *fftypes.Message); ok {
		r0 = rf(ctx, id, options...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*fftypes.Message)
		}
	}

	var r1 fftypes.DataArray
	if rf, ok := ret.Get(1).(func(context.Context, *fftypes.UUID, ...data.CacheReadOption) fftypes.DataArray); ok {
		r1 = rf(ctx, id, options...)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).(fftypes.DataArray)
		}
	}

	return r0, r1
}

// ResolveInlineData provides a mock function with given fields: ctx, msg
func (_m *Manager) ResolveInlineData(ctx context.Context, msg *data.NewMessage) error {
	ret := _m.Called(ctx, msg)

	var r0 error
	if rf, ok := ret.Get(0).(func(context.Context, *data.NewMessage) error); ok {
		r0 = rf(ctx, msg)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// UpdateMessageCache provides a mock function with given fields: msg, _a1
func (_m *Manager) UpdateMessageCache(msg *fftypes.Message, _a1 fftypes.DataArray) {
	_m.Called(msg, _a1)
}

// UpdateMessageIfCached provides a mock function with given fields: ctx, msg
func (_m *Manager) UpdateMessageIfCached(ctx context.Context, msg *fftypes.Message) {
	_m.Called(ctx, msg)
}

// UpdateMessageStateIfCached provides a mock function with given fields: ctx, id, state, confirmed
func (_m *Manager) UpdateMessageStateIfCached(ctx context.Context, id *fftypes.UUID, state fftypes.FFEnum, confirmed *fftypes.FFTime) {
	_m.Called(ctx, id, state, confirmed)
}

// UploadBLOB provides a mock function with given fields: ctx, ns, inData, blob, autoMeta
func (_m *Manager) UploadBLOB(ctx context.Context, ns string, inData *fftypes.DataRefOrValue, blob *fftypes.Multipart, autoMeta bool) (*fftypes.Data, error) {
	ret := _m.Called(ctx, ns, inData, blob, autoMeta)

	var r0 *fftypes.Data
	if rf, ok := ret.Get(0).(func(context.Context, string, *fftypes.DataRefOrValue, *fftypes.Multipart, bool) *fftypes.Data); ok {
		r0 = rf(ctx, ns, inData, blob, autoMeta)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*fftypes.Data)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, string, *fftypes.DataRefOrValue, *fftypes.Multipart, bool) error); ok {
		r1 = rf(ctx, ns, inData, blob, autoMeta)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// UploadJSON provides a mock function with given fields: ctx, ns, inData
func (_m *Manager) UploadJSON(ctx context.Context, ns string, inData *fftypes.DataRefOrValue) (*fftypes.Data, error) {
	ret := _m.Called(ctx, ns, inData)

	var r0 *fftypes.Data
	if rf, ok := ret.Get(0).(func(context.Context, string, *fftypes.DataRefOrValue) *fftypes.Data); ok {
		r0 = rf(ctx, ns, inData)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*fftypes.Data)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, string, *fftypes.DataRefOrValue) error); ok {
		r1 = rf(ctx, ns, inData)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// ValidateAll provides a mock function with given fields: ctx, _a1
func (_m *Manager) ValidateAll(ctx context.Context, _a1 fftypes.DataArray) (bool, error) {
	ret := _m.Called(ctx, _a1)

	var r0 bool
	if rf, ok := ret.Get(0).(func(context.Context, fftypes.DataArray) bool); ok {
		r0 = rf(ctx, _a1)
	} else {
		r0 = ret.Get(0).(bool)
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, fftypes.DataArray) error); ok {
		r1 = rf(ctx, _a1)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// VerifyNamespaceExists provides a mock function with given fields: ctx, ns
func (_m *Manager) VerifyNamespaceExists(ctx context.Context, ns string) error {
	ret := _m.Called(ctx, ns)

	var r0 error
	if rf, ok := ret.Get(0).(func(context.Context, string) error); ok {
		r0 = rf(ctx, ns)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// WaitStop provides a mock function with given fields:
func (_m *Manager) WaitStop() {
	_m.Called()
}

// WriteNewMessage provides a mock function with given fields: ctx, newMsg
func (_m *Manager) WriteNewMessage(ctx context.Context, newMsg *data.NewMessage) error {
	ret := _m.Called(ctx, newMsg)

	var r0 error
	if rf, ok := ret.Get(0).(func(context.Context, *data.NewMessage) error); ok {
		r0 = rf(ctx, newMsg)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}
