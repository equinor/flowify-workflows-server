package apiserver

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/equinor/flowify-workflows-server/auth"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

func init() {
	log.SetOutput(ioutil.Discard)
}

type testCase struct {
	Name string
	URL  string
}

func Test_Routes(t *testing.T) {

	fs, err := NewFlowifyServer(
		fake.NewSimpleClientset(),
		nil, /* wfclient cs_workflow.Interface */
		nil, /* v2.storage  */
		nil, /* v2.volumeStorage  */
		1234,
		auth.AzureTokenAuthenticator{},
	)

	if err != nil {
		log.Fatal(err)
	}

	mux := mux.NewRouter()
	fs.registerApplicationRoutes(mux)

	testcases := []testCase{
		{Name: "api/v2", URL: "/api/v2/components/"},
		{Name: "z-page/live", URL: "/livez"},
		{Name: "z-page/ready", URL: "/readyz"},
		{Name: "z-page/version", URL: "/versionz"},
	}

	for _, test := range testcases {
		t.Run(test.Name, func(t *testing.T) {
			// TODO: the binary needs at the correct path to work. This should be fixed.
			if test.Name == "swagger" {
				t.Skip()
			}

			req := httptest.NewRequest(http.MethodGet, test.URL, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			res := w.Result()

			require.NotEqual(t, http.StatusNotFound, res.StatusCode)
		})
	}
}
