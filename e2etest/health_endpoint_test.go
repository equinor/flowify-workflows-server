package test

import (
	"context"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	v1alpha "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned/fake"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/equinor/flowify-workflows-server/apiserver"
	"github.com/equinor/flowify-workflows-server/v2/auth"
	"github.com/equinor/flowify-workflows-server/v2/storage"
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
		wfclient,
		nodeStorage,
		volumeStorage,
		8842,
		authc,
	)

	go server.Run(ctx)

	// Exponential backoff needs a largish initial value to allow for server startup
	// 1s works in local dev, may need to adjust for hosted testing
	err := wait.ExponentialBackoff(wait.Backoff{Duration: time.Second, Steps: 10}, func() (bool, error) { return checkLiveEndpoint(t) })
	assert.NoError(t, err)
}

func checkLiveEndpoint(t *testing.T) (bool, error) {
	// TODO: Check the string value (alive)
	resp, err := http.Get("http://localhost:8842/livez")
	if resp == nil {
		// server has not yet started, signal retry
		return false, nil
	}

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	payload, err := ioutil.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.Equal(t, "alive", string(payload))
	return true, err
}
