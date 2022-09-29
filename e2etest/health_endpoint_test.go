package test

import (
	"context"
	"io/ioutil"
	"net/http"
	"testing"

	v1alpha "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/equinor/flowify-workflows-server/apiserver"
	"github.com/equinor/flowify-workflows-server/auth"
	"github.com/equinor/flowify-workflows-server/storage"
)

const (
	test_host              = "localhost"
	test_namespace         = "testing-namespace"
	test_port              = 27017
	test_db_name           = "test"
	n_items                = 5
	ext_mongo_hostname_env = "FLOWIFY_MONGO_ADDRESS"
	ext_mongo_port_env     = "FLOWIFY_MONGO_PORT"
)

func TestLiveEndpoint(t *testing.T) {
	ctx := context.Background()

	kubeclient := fake.NewSimpleClientset()
	wfclient := v1alpha.NewSimpleClientset()
	var nodeStorage storage.ComponentClient = nil /* storage.NewMongoStorageClient(storage.NewMongoClient(), test_db_name) */
	var volumeStorage storage.VolumeClient = nil  /* storage.NewMongoVolumeClient(storage.NewMongoClient(), test_db_name) */
	var authc auth.AuthClient = auth.MockAuthenticator{}

	server, _ := apiserver.NewFlowifyServer(
		kubeclient,
		test_namespace,
		wfclient,
		nodeStorage,
		volumeStorage,
		8842,
		authc,
	)

	readyNotifier := make(chan bool, 1)

	go server.Run(ctx, &readyNotifier)

	assert.True(t, <-readyNotifier, "wait for server start before testing")

	resp, err := http.Get("http://localhost:8842/livez")
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	payload, err := ioutil.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.Equal(t, "alive", string(payload))
}
