package test

import (
	"bytes"
	"io"
	//	flowifypkg "github.com/equinor/flowify-workflows-server/pkg/apiclient/interceptor"
	//	"github.com/equinor/flowify-workflows-server/workflowserver"
)

func body2string(body io.ReadCloser) []byte {
	buf := new(bytes.Buffer)
	buf.ReadFrom(body)

	return buf.Bytes()
}

func wrap(workflowstring string) string {
	return "{ \"template\":" + workflowstring + "}"
}

func (s *e2eTestSuite) Test_components() {
	/*

		requestor := make_requestor(s.client)

		ccstore := workflowserver.NewFlowifyWorkflowStorageClient(storageclient.NewMongoClient())

		// Clear DB collection before returning the client handler
		ccstore.Clear()

		// Change names to match e2e tests jwt token
		wf1 := mockdata.WorkflowTemplate1
		wf2 := mockdata.WorkflowTemplate2

		resp, err := requestor("http://localhost:8842/api/v1/flowify-workflows/?workspace=test", http.MethodPost, wrap(wf1))
		s.NoError(err)
		s.Equal(http.StatusOK, http.StatusOK)

		resp, err = requestor("http://localhost:8842/api/v1/flowify-workflows/?workspace=test", http.MethodPost, wrap(wf1))
		s.NoError(err)
		s.Equal(http.StatusOK, http.StatusOK)

		resp, err = requestor("http://localhost:8842/api/v1/flowify-workflows/?workspace=test", http.MethodPost, wrap(wf1))
		s.NoError(err)
		s.Equal(http.StatusOK, http.StatusOK)

		resp, err = requestor("http://localhost:8842/api/v1/flowify-workflows/?workspace=test", http.MethodPost, wrap(wf2))
		s.NoError(err)
		s.Equal(http.StatusOK, http.StatusOK)

		resp, err = requestor("http://localhost:8842/api/v1/flowify-workflows/?workspace=test", http.MethodGet, "")
		s.Equal(http.StatusOK, resp.StatusCode)

		var l1 workflowserver.WorkflowList
		json.Unmarshal(body2string(resp.Body), &l1)
		s.Len(l1.Items, 2)

		var wft v1alpha1.WorkflowTemplate
		json.Unmarshal(l1.Items[0].Content, &wft)

		resp, err = requestor("http://localhost:8842/api/v1/flowify-workflows/"+wft.ObjectMeta.Name+"/versions?workspace=test", http.MethodGet, "")
		s.Equal(http.StatusOK, resp.StatusCode)

		var l2 workflowserver.VersionList
		json.Unmarshal(body2string(resp.Body), &l2)

		s.Len(l2.Versions, 3)

		Names := make([]string, 3)
		Versions := make([]string, 3)

		for i, item := range l2.Versions {
			Names[i] = item.WrittenBy
			Versions[i] = item.Version
		}

		s.Len(l2.Versions, 3)

		s.ElementsMatch([]string{"0", "1", "2"}, Versions)
		s.ElementsMatch([]string{"test@test.com", "test@test.com", "test@test.com"}, Names) // injected from the used test auth token

		resp, err = requestor("http://localhost:8842/api/v1/flowify-workflows/workflowtemplate1?workspace=test", http.MethodGet, "")
		s.NoError(err)
		s.Equal(http.StatusOK, resp.StatusCode)
		s.True(json.Valid(body2string(resp.Body)))

		_, err = requestor("http://localhost:8842/api/v1/flowify-workflows/workflowtemplate1?version=1&workspace=test", http.MethodGet, "")
		s.NoError(err)
		s.Equal(http.StatusOK, resp.StatusCode)

		// Submit non-existing version
		req := flowifypkg.WorkflowSubmitRequest{Namespace: "test", ResourceKind: "WorkflowTemplate", ResourceName: "workflowtemplate2", Version: "1"}
		payload, err := json.Marshal(req)
		s.NoError(err)

		resp, err = requestor("http://localhost:8842/api/v1/workflows/test/submit", http.MethodPost, string(payload))
		s.NoError(err)
		s.Equal(http.StatusNotFound, resp.StatusCode)

		// Submit existing version, with explicit version
		req = flowifypkg.WorkflowSubmitRequest{Namespace: "test", ResourceKind: "WorkflowTemplate", ResourceName: "workflowtemplate2", Version: "0"}
		payload, err = json.Marshal(req)
		s.NoError(err)

		resp, err = requestor("http://localhost:8842/api/v1/workflows/test/submit", http.MethodPost, string(payload))
		s.NoError(err)
		s.Equal(http.StatusOK, resp.StatusCode)

		// submit with implicit 'last' version
		req = flowifypkg.WorkflowSubmitRequest{Namespace: "test", ResourceKind: "WorkflowTemplate", ResourceName: "workflowtemplate2"}
		payload, err = json.Marshal(req)
		s.NoError(err)

		resp, err = requestor("http://localhost:8842/api/v1/workflows/test/submit", http.MethodPost, string(payload))
		s.NoError(err)
		s.Equal(http.StatusOK, resp.StatusCode)

		var wf v1alpha1.Workflow
		err = json.Unmarshal(body2string(resp.Body), &wf)
		s.NoError(err)

		name := wf.ObjectMeta.Name

		// Fetch workflow, and verify name is the same as the submitted workflow object
		resp, err = requestor("http://localhost:8842/api/v1/workflows/test/"+name, http.MethodGet, "")
		s.Equal(http.StatusOK, resp.StatusCode)
		s.NoError(err)

		err = json.Unmarshal(body2string(resp.Body), &wf)
		s.NoError(err)

		s.Equal(name, wf.ObjectMeta.Name)

		// Check that the workflowtemplate was reaped
		resp, err = requestor("http://localhost:8842/api/v1/workflow-templates/test", http.MethodGet, "")
		s.NoError(err)
		s.Equal(http.StatusOK, resp.StatusCode)

		var wftList v1alpha1.WorkflowTemplateList
		err = json.Unmarshal(body2string(resp.Body), &wftList)

		s.Len(wftList.Items, 0)
		s.NoError(err)

		// Remove the workflow
		resp, err = requestor("http://localhost:8842/api/v1/workflows/test/"+name, http.MethodDelete, "")
		s.NoError(err)
		s.Equal(http.StatusOK, resp.StatusCode)
	*/
}
