package apiserver

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/equinor/flowify-workflows-server/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	test_server_port       = 1234
	mongo_test_host        = "localhost"
	mongo_test_port        = 27017
	test_db_name           = "test"
	test_namespace         = "testing-namespace"
	n_items                = 5
	ext_mongo_hostname_env = "FLOWIFY_MONGO_ADDRESS"
	ext_mongo_port_env     = "FLOWIFY_MONGO_PORT"
)

type testCase struct {
	Name       string
	URL        string
	StatusCode int
	Body       string
}

func Test_ApiServer(t *testing.T) {
	server, err := NewFlowifyServer(
		fake.NewSimpleClientset(),
		"not-used", /* config namespace for k8s */
		nil,        /* wfclient cs_workflow.Interface */
		nil,        /* storage  */
		nil,        /* volumeStorage  */
		1234,
		auth.AzureTokenAuthenticator{},
	)
	require.NoError(t, err)

	/*
		spin up a apiserver server with some functionality not connected
	*/

	ready := make(chan bool, 1)
	go server.Run(context.TODO(), &ready)

	require.True(t, <-ready, "make sure the server started before we continue")

	testcases := []testCase{
		{Name: "z-page/live", URL: "livez", StatusCode: http.StatusOK, Body: "alive"},
		{Name: "z-page/ready", URL: "readyz", StatusCode: http.StatusOK, Body: "ready"},
		{Name: "z-page/version", URL: "versionz", StatusCode: http.StatusOK, Body: CommitSHA},
	}

	for _, test := range testcases {
		t.Run(test.Name, func(t *testing.T) {
			endpoint := fmt.Sprintf("http://localhost:%d/%s", test_server_port, test.URL)
			fmt.Println("URL ", endpoint)
			resp, err := http.Get(endpoint)
			require.NoError(t, err)
			require.NotNil(t, resp)

			assert.Equal(t, test.StatusCode, resp.StatusCode)
			payload, err := ioutil.ReadAll(resp.Body)
			assert.NoError(t, err)
			assert.Equal(t, test.Body, string(payload))
		})
	}
}
