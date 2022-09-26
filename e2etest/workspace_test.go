package test

import (
	"net/http"

	"github.com/equinor/flowify-workflows-server/pkg/workspace"
)

func init() {
	/*
	   program := "kubectl"
	   appcmd := "apply"
	   flags := "-f"
	   arg := "workspace_cm_test.yaml"

	   	if exec.Command(program, appcmd, flags, arg).Run() != nil {
	   		panic("Error applying workspace manifests")
	   	}
	*/
}

func (s *e2eTestSuite) Test_workspaces() {
	requestor := make_requestor(s.client)

	resp, err := requestor("http://localhost:8842/api/v1/workspaces/", http.MethodGet, "")
	s.NoError(err)

	s.Len(resp.Header["Content-Type"], 1)
	s.Equal("application/json", resp.Header["Content-Type"][0])

	type WorkspaceList struct {
		Items []workspace.Workspace `json:"items"`
	}
	var list WorkspaceList
	marshalResponse(resp, &list)

	s.Len(list.Items, 2)

	accesses := make([]bool, 2)

	for i, ws := range list.Items {
		accesses[i] = ws.HasAccess
	}

	s.Contains(accesses, false)
	s.Contains(accesses, true)
}
