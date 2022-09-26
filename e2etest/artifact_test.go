package test

import (
	"net/http"
)

func (s *e2eTestSuite) Test_getArtifact() {
	s.T().Skip("Artifact test known to fail")
	requestor := make_requestor(s.client)

	defer func() {
		resp, err := requestor("http://localhost:8842/api/v1/workflows/test/workflow1", http.MethodDelete, "")

		s.NoError(err)
		s.Equal(http.StatusOK, resp.StatusCode, "Expected and known to fail")
	}()
	/*
	   // Push a workflow
	   resp, err := requestor("http://localhost:8842/api/v1/workflows/test", http.MethodPost, mockdata.WorkflowWithOutputArtifact)
	   s.NoError(err)

	   s.Equal(http.StatusOK, resp.StatusCode)

	   	if err != nil {
	   		s.T().Fatalf("Error reaching the flowify server: %v", err)
	   	}

	   s.Equal(http.StatusOK, resp.StatusCode)

	   // Give container time to spin up and do stuff. Should be changed for a
	   // wait condition at some point.
	   time.Sleep(10 * time.Second)
	   resp, err = requestor("http://localhost:8842/artifacts/test/artifact-passing/artifact-passing/hello-art", http.MethodGet, "")
	   s.NoError(err)
	*/
}
