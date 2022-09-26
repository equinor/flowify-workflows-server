package workspace

import (
	"context"

	"github.com/stretchr/testify/mock"
)

type WorkspaceClientMock struct {
	mock.Mock
}

func NewDefaultWorkspaceClientMock() *WorkspaceClientMock {
	obj := &WorkspaceClientMock{}
	obj.On("ListWorkspaces", mock.Anything, mock.Anything).Return(nil, nil)

	return obj
}

func (m *WorkspaceClientMock) ListWorkspaces(ctx context.Context, userTokens []string) ([]Workspace, error) {
	args := m.Called(ctx, userTokens)
	return args.Get(0).([]Workspace), args.Error(1)
}

func (m *WorkspaceClientMock) HasAccessToWorkspace(ctx context.Context, workspaceName string, userTokens []string) (bool, error) {
	args := m.Called(ctx, workspaceName, userTokens)
	return args.Bool(0), args.Error(1)
}

func (m *WorkspaceClientMock) GetNamespace() string {
	args := m.Called()
	return args.String(0)
}
