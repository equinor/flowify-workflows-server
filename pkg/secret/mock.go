package secret

import (
	"context"

	"github.com/stretchr/testify/mock"
)

type SecretClientMock struct {
	mock.Mock
}

func NewDefaultSecretClientMock() *SecretClientMock {
	obj := &SecretClientMock{}
	obj.On("ListAvailableKeys", mock.Anything, mock.Anything).Return([]string{"key1", "key2", "key3"}, nil)
	obj.On("AddSecretKey", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	obj.On("DeleteSecretKey", mock.Anything, mock.Anything, "key-ok").Return(nil)

	return obj
}

func (m *SecretClientMock) ListAvailableKeys(ctx context.Context, group string) ([]string, error) {
	args := m.Called(ctx, group)
	return args.Get(0).([]string), args.Error(1)
}

func (m *SecretClientMock) AddSecretKey(ctx context.Context, group, name, key string) error {
	args := m.Called(ctx, group, name, key)
	return args.Error(0)
}

func (m *SecretClientMock) DeleteSecretKey(ctx context.Context, group, name string) error {
	args := m.Called(ctx, group, name)
	return args.Error(0)
}
