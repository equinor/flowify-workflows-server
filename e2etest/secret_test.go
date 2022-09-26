package test

import (
	"bytes"
	"encoding/json"
	"net/http"

	wf "github.com/argoproj/argo-workflows/v3/pkg/apiclient/workflow"
)

func ignore[T any](T) {}
func (s *e2eTestSuite) Test_SecretHandling_live_system() {
	requestor := make_requestor(s.client)
	ignore(requestor)
	// Push some secrets

	type SecretField struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}

	type SecretFieldList struct {
		Items []SecretField `json:"items"`
	}

	payload_obj1 := SecretFieldList{
		Items: []SecretField{
			{Key: "key1", Value: "val1"},
			{Key: "key2", Value: "val2"},
			{Key: "fake_key1", Value: "dummyload"}},
	}

	ignore(payload_obj1)
	/*

				// There is no check on write, so this is legit. But the token cannot read it back again...

		   	payload_obj2 := secret.SecretFieldList{
		   		Items: []secret.SecretField{secret.SecretField{"key1", "val1"}, secret.SecretField{"key2", "val2"}, secret.SecretField{"fake_key1", "valX"}}}

		   workspaces := []string{"test", "test-no-access", "not-existing-workspace"}
		   statuses := []int{http.StatusCreated, http.StatusForbidden, http.StatusNotFound}

		   	for i, obj := range []secret.SecretFieldList{payload_obj1, payload_obj2, payload_obj2} {
		   		payload_json, err := json.Marshal(obj)
		   		s.NoError(err)
		   		resp, err := requestor("http://localhost:8842/api/v1/secrets/"+workspaces[i], http.MethodPost, string(payload_json))
		   		s.NoError(err)
		   		s.Equal(statuses[i], resp.StatusCode)
		   	}

		   // Read back available fields
		   resp, err := requestor("http://localhost:8842/api/v1/secrets/test", http.MethodGet, "")

		   s.NoError(err)
		   s.Equal(http.StatusOK, resp.StatusCode)

		   var list secret.SecretKeyList
		   marshalResponse(resp, &list)

		   s.ElementsMatch(list.Keys, []string{"key1", "key2", "fake_key1"})

		   // Run a workflow with valid secret access
		   resp, err = requestor("http://localhost:8842/api/v1/workflows/test", http.MethodPost, mockdata.WorkflowWithSecret)

		   s.NoError(err)
		   s.Equal(http.StatusOK, resp.StatusCode)

		   checkLogMessage(s, "wfwithsecret", "dummyload")

		   resp, err = requestor("http://localhost:8842/api/v1/workflows/test/wfwithsecret", http.MethodDelete, "")
		   s.NoError(err)
		   s.Equal(http.StatusOK, resp.StatusCode)
	*/
}

func checkLogMessage(s *e2eTestSuite, wfName, expectedMessage string) {
	requestor := make_requestor(s.client)
	resp, err := requestor("http://localhost:8842/api/v1/workflows/test/"+wfName+"/log?logOptions.container=main&logOptions.follow=true", http.MethodGet, "")
	s.NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)
	s.Equal("text/event-stream", resp.Header.Get("Content-Type"))

	buf_log := new(bytes.Buffer)
	buf_log.ReadFrom(resp.Body)
	buf_log.Next(6) // remove data prefix
	var objmap map[string]json.RawMessage

	err = json.Unmarshal(buf_log.Bytes(), &objmap)
	s.NoError(err)

	var entry wf.LogEntry
	s.NoError(json.Unmarshal(objmap["result"], &entry))
	s.Equal(expectedMessage, entry.Content)
}
