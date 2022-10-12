package test

import (
	"net/http"

	"github.com/equinor/flowify-workflows-server/pkg/workspace"
	"github.com/stretchr/testify/require"
)

func (s *e2eTestSuite) Test_Workspaces() {
	requestor := make_authenticated_requestor(s.client, mockUser)

	resp, err := requestor(server_addr+"/api/v1/workspaces/", http.MethodGet, "")
	require.NoError(s.T(), err, BodyStringer{resp.Body})
	require.Equal(s.T(), http.StatusOK, resp.StatusCode, BodyStringer{resp.Body})

	type WorkspaceList struct {
		Items []workspace.Workspace `json:"items"`
	}
	var list WorkspaceList
	err = marshalResponse(ResponseBodyBytes(resp), &list)

	s.NoError(err)
	s.NotEmpty(list.Items)
}
