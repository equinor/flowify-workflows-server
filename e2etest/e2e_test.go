package test

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	v1alpha "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned/fake"
	"github.com/equinor/flowify-workflows-server/apiserver"
	"github.com/equinor/flowify-workflows-server/auth"
	"github.com/equinor/flowify-workflows-server/models"
	"github.com/equinor/flowify-workflows-server/pkg/workspace"
	"github.com/equinor/flowify-workflows-server/storage"
	fuser "github.com/equinor/flowify-workflows-server/user"
	"github.com/golang-jwt/jwt/v4"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type e2eTestSuite struct {
	suite.Suite
	client     *http.Client
	kubeclient *kubernetes.Clientset
}

var (
	auth_header = ""
	url         = "localhost:27017"
)

func TestE2ETestSuite(t *testing.T) {
	suite.Run(t, &e2eTestSuite{})
}

func getKubeClient() *kubernetes.Clientset {
	config, err := rest.InClusterConfig()

	if err != nil {
		env := os.Getenv("KUBECONFIG")

		var path string

		if env == "" {
			usr, _ := user.Current()

			log.Infof("No service account detected, running locally")
			path = filepath.Join(usr.HomeDir, ".kube/config")
		} else {
			path = env
		}
		kubeconfig := flag.String("kubeconfig", path, "kubeconfig file")
		flag.Parse()

		config, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)

		if err != nil {
			log.Errorf("Cannot load kube config: %s", err)
			panic("Cannot load .kube file")
		}
	}

	return kubernetes.NewForConfigOrDie(config)
}

var mockUser fuser.MockUser = fuser.MockUser{
	Uid:   "0",
	Name:  "Auth Disabled",
	Email: "auth@disabled",
	Roles: []fuser.Role{"tester", "dummy"},
}

var testWorkspace string = `
---
# Namespace 'sandbox-project-a'
apiVersion: v1
kind: Namespace
metadata:
  labels:
    app.kubernetes.io/part-of: "flowify"
  name: "test"

---
# Developer workspace environment
apiVersion: v1
kind: ConfigMap
metadata:
    labels:
        app.kubernetes.io/component: "workspace-config"
        app.kubernetes.io/part-of: "flowify"
    name: "test"
    namespace: "test"
data:
    roles: "[[\"tester\"]]"
`

func (s *e2eTestSuite) SetupSuite() {
	logrus.Info("Setting up e2eTestSuite")

	ctx := context.Background()
	s.client = &http.Client{}
	s.client.Timeout = time.Second * 30
	s.kubeclient = getKubeClient()

	//kubeclient := fake.NewSimpleClientset()

	wfclient := v1alpha.NewSimpleClientset()
	//	var nodeStorage storage.ComponentClient = nil /* storage.NewMongoStorageClient(storage.NewMongoClient(), test_db_name) */
	dbName := "e2etest"
	os.Setenv("FLOWIFY_MONGO_ADDRESS", "localhost")
	os.Setenv("FLOWIFY_MONGO_PORT", "27017")
	m := storage.NewMongoClient()
	log.Infof("Dropping db %s to make sure we're clean", dbName)
	m.Database(dbName).Drop(context.TODO())
	nodeStorage := storage.NewMongoStorageClient(m, dbName)

	var volumeStorage storage.VolumeClient = nil /* storage.NewMongoVolumeClient(storage.NewMongoClient(), test_db_name) */
	var authc auth.AuthClient = auth.MockAuthenticator{User: mockUser}

	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	stdout, _ := cmd.Output()

	apiserver.CommitSHA = strings.TrimSuffix(string(stdout), "\n")
	apiserver.BuildTime = time.Now().UTC().Format(time.RFC3339)
	namespace := "e2etest"

	server, _ := apiserver.NewFlowifyServer(
		s.kubeclient,
		wfclient,
		nodeStorage,
		volumeStorage,
		8842,
		authc,
	)

	ready := make(chan bool, 1)

	go server.Run(ctx, &ready)

	mockUser := fuser.MockUser{Uid: "nonce", Name: "John Doe", Email: "user@test.com", Roles: []fuser.Role{"role-x", "role-y"}}
	jwtUser := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"name":  mockUser.Name,
		"email": mockUser.Email,
		"roles": mockUser.Roles,
		"iat":   time.Now().Unix(),
		"nbf":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Minute * 5).Unix(),
		"aud":   "e2e-test",
		"iss":   "e2e-test",
	})
	const secretKey = "my_secret_key"
	tokenString, err := jwtUser.SignedString([]byte(secretKey))
	require.NoError(s.T(), err)
	auth_header = tokenString

	wsName := "test"
	if _, err := s.kubeclient.CoreV1().Namespaces().Get(context.TODO(), namespace, metav1.GetOptions{}); errors.IsNotFound(err) {
		ns := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name:      namespace,
			Namespace: namespace}}
		ns, err = s.kubeclient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		s.NoError(err)
	} else {
		fmt.Println("ns found", namespace)
	}

	if _, err := s.kubeclient.CoreV1().ConfigMaps(namespace).Get(context.TODO(), wsName, metav1.GetOptions{}); errors.IsNotFound(err) {
		ws_test := &v1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name:      wsName,
			Namespace: namespace,
			Labels:    map[string]string{"app.kubernetes.io/component": "workspace-config"},
		}, Data: map[string]string{"roles": "[[\"tester\"]]"}}
		ws_test, err = s.kubeclient.CoreV1().ConfigMaps(namespace).Create(context.TODO(), ws_test, metav1.CreateOptions{})
		s.NoError(err)
	} else {
		fmt.Println("Ws found", wsName)
	}

	// make sure we get the ready signal
	s.Equal(true, <-ready)
}

func (s *e2eTestSuite) TearDownSuite() {
	/*
	   opts := metav1.DeleteOptions{}
	   err := s.kubeclient.CoreV1().Namespaces().Delete(context.TODO(), "test", opts)
	   s.NoError(err)
	*/
}

func make_requestor(client *http.Client) func(string, string, string) (*http.Response, error) {
	return func(url, method string, payload string) (*http.Response, error) {
		return make_request_with_client(url, method, payload, client)
	}
}

type nameList struct {
	Names []string `json:"names"`
}

func make_request_with_client(url, method string, payload string, client *http.Client) (*http.Response, error) {
	req, _ := http.NewRequest(method, url, strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", auth_header)

	return client.Do(req)
}

func (s *e2eTestSuite) Test_zpages() {
	resp, err := http.Get("http://localhost:8842/versionz")
	require.Nil(s.T(), err)

	s.Equal(http.StatusOK, resp.StatusCode)
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	stdout, _ := cmd.Output()

	// test body
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	git_sha := strings.TrimSuffix(string(stdout), "\n")
	s.Equal(git_sha, buf.String(), "versionz returns git hash in body")

	// test headers
	s.Equal(git_sha, resp.Header.Get("X-Flowify-Version"), "version also set in X-header")
	buildtime, err := time.Parse(time.RFC3339, resp.Header.Get("X-Flowify-Buildtime"))
	s.NotEmpty(buildtime, "make sure we get a datetime string")
	s.NoError(err)
	s.Equal(resp.Header.Get("X-Wrong"), "")
}

func (s *e2eTestSuite) Test_Userinfo() {
	requestor := make_requestor(s.client)

	resp, err := requestor("http://localhost:8842/api/userinfo/", http.MethodGet, "")
	s.NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	var user fuser.MockUser
	err = marshalResponse(resp, &user)
	s.NoError(err)
	s.Equal(mockUser, user)
}

func (s *e2eTestSuite) Test_Workspaces() {
	requestor := make_requestor(s.client)

	resp, err := requestor("http://localhost:8842/api/v1/workspaces/", http.MethodGet, "")
	s.NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	type WorkspaceList struct {
		Items []workspace.Workspace `json:"items"`
	}
	var list WorkspaceList
	err = marshalResponse(resp, &list)

	s.NoError(err)
	s.NotEmpty(list.Items)
}

func (s *e2eTestSuite) Test_Roundtrip_Component() {
	requestor := make_requestor(s.client)

	cmp1, _ := ioutil.ReadFile("../v1/models/examples/minimal-any-component.json")
	cmpReq := fmt.Sprintf(`
	{
		"options": {},
		"component": %s
	}`, cmp1)

	resp, err := requestor("http://localhost:8842/api/v1/components/", http.MethodPost, cmpReq)
	s.NoError(err)
	require.Equal(s.T(), http.StatusCreated, resp.StatusCode)

	var cmpResp models.Component
	err = marshalResponse(resp, &cmpResp)
	s.NoError(err)

	resp2, err := requestor(fmt.Sprintf("http://localhost:8842/api/v1/components/%s", cmpResp.Metadata.Uid.String()), http.MethodGet, cmpReq)
	s.NoError(err)
	require.Equal(s.T(), http.StatusOK, resp2.StatusCode)

	var cmpResp2 models.Component
	err = marshalResponse(resp2, &cmpResp2)
	s.NoError(err)
	s.Equal(cmpResp, cmpResp2, "expect roundtrip equality")

}

func (s *e2eTestSuite) Test_Roundtrip_Workflow() {
	requestor := make_requestor(s.client)

	data, _ := ioutil.ReadFile("../v1/models/examples/minimal-any-workflow.json")
	wfReq := fmt.Sprintf(`
	{
		"options": {},
		"workflow": %s
	}`, data)

	resp, err := requestor("http://localhost:8842/api/v1/workflows/", http.MethodPost, wfReq)
	s.NoError(err)
	require.Equal(s.T(), http.StatusCreated, resp.StatusCode)

	var wfResp models.Workflow
	err = marshalResponse(resp, &wfResp)
	s.NoError(err)

	resp2, err := requestor(fmt.Sprintf("http://localhost:8842/api/v1/workflows/%s", wfResp.Metadata.Uid.String()), http.MethodGet, wfReq)
	s.NoError(err)

	var wfResp2 models.Workflow
	err = marshalResponse(resp2, &wfResp2)
	s.NoError(err)
	s.Equal(wfResp, wfResp2, "expect roundtrip equality")

}

/*
	func (s *e2eTestSuite) Test_Roundtrip_live_system() {
		requestor := make_requestor(s.client)

		var pp [7]string
		pp[0] = wf1
		pp[1] = wf2
		pp[2] = wf3

		for i := 0; i < 3; i++ {
			resp, err := requestor("http://localhost:8842/api/v1/workflows/test", http.MethodPost, pp[i])
			s.NoError(err)

			if err != nil {
				s.T().Fatalf("Error reaching the flowify server: %v", err)
			}

			s.Equal(http.StatusOK, resp.StatusCode)
		}

		{
			type TestResponse struct {
				name     string
				response int
			}

			wf_list := []TestResponse{{"hello-world-b6h5m", http.StatusOK}, {"hello-world-9tql2-test", http.StatusOK}, {"hello-world-b6h5m-test", http.StatusOK}, {"hello-missing-workflow", http.StatusNotFound}}

			for _, testcase := range wf_list {
				resp, err := requestor("http://localhost:8842/api/v1/workflows/test/"+testcase.name, http.MethodGet, "")
				s.Equal(testcase.response, resp.StatusCode)
				s.NoError(err)
			}
		}
		resp, err := requestor("http://localhost:8842/api/v1/workflows/test/hello-world-9tql2-test", http.MethodDelete, "")
		s.NoError(err)
		s.Equal(http.StatusOK, resp.StatusCode)

		resp, err = requestor("http://localhost:8842/api/v1/workflow-templates/test", http.MethodPost, wft1)
		s.NoError(err)
		s.Equal(http.StatusOK, resp.StatusCode)

		resp, err = requestor("http://localhost:8842/api/v1/workflows/test/submit", http.MethodPost, `{"resourceKind": "WorkflowTemplate", "ResourceName": "wft1"}`)
		s.NoError(err)
		s.Equal(http.StatusOK, resp.StatusCode)

		buf_subm := new(bytes.Buffer)
		buf_subm.ReadFrom(resp.Body)

		var wf_subm wfv1.Workflow
		err = json.Unmarshal(buf_subm.Bytes(), &wf_subm)
		name_submitted := wf_subm.ObjectMeta.Name

		time.Sleep(3 * time.Second)

		resp, err = requestor("http://localhost:8842/api/v1/workflows/test/"+name_submitted+"/log?logOptions.container=main&logOptions.follow=true&logOptions.podName="+name_submitted,
			http.MethodGet, "")
		s.NoError(err)
		s.Equal(http.StatusOK, resp.StatusCode)
		s.Equal("text/event-stream", resp.Header.Get("Content-Type"))

		buf_log := new(bytes.Buffer)

		// wait for up to 10s for the stream to deliver any body data
		for i := 0; i < 10; i++ {
			numRead, err := buf_log.ReadFrom(resp.Body)

			s.NoError(err)

			if numRead > 6 {
				break
			}
			time.Sleep(1 * time.Second)
		}

		buf_log.Next(6) // remove data prefix
		var objmap map[string]json.RawMessage

		err = json.Unmarshal(buf_log.Bytes(), &objmap)
		s.NoError(err)

		var entry wf.LogEntry
		s.NoError(json.Unmarshal(objmap["result"], &entry))
		s.Equal("hello world", entry.Content)

		var struk wfv1.WorkflowList
		{
			resp, err = requestor("http://localhost:8842/api/v1/workflows/test", http.MethodGet, "") // should be {wf1, wf3, wft}

			buf := new(bytes.Buffer)
			buf.ReadFrom(resp.Body)

			err = json.Unmarshal(buf.Bytes(), &struk)

			s.Equal(3, len(struk.Items))
		}

		resp, err = requestor("http://localhost:8842/api/v1/workflow-events/test", http.MethodGet, "")
		s.NoError(err)
		s.Equal(http.StatusOK, resp.StatusCode)
		s.NotNil(resp.Body)

		s.Equal("text/event-stream", resp.Header.Get("Content-Type"))

		resp, err = requestor("http://localhost:8842/api/v1/workflows/test/"+name_submitted, http.MethodDelete, "")
		s.NoError(err)
		s.Equal(http.StatusOK, resp.StatusCode)

		resp, err = requestor("http://localhost:8842/api/v1/workflow-templates/test/wft1", http.MethodDelete, "")
		s.NoError(err)
		s.Equal(http.StatusOK, resp.StatusCode)

		resp, err = requestor("http://localhost:8842/api/v1/workflows/test", http.MethodPost, wf4) // wf4
		s.NoError(err)

		// Post in wrong namespace
		resp, err = requestor("http://localhost:8842/api/v1/workflows/test-no-access", http.MethodPost, wf1)
		s.NoError(err)
		s.Equal(http.StatusForbidden, resp.StatusCode)

		// Post in notexisting namespace
		resp, err = requestor("http://localhost:8842/api/v1/workflows/test-does-not-exist", http.MethodPost, wf2)
		s.NoError(err)
		s.Equal(http.StatusNotFound, resp.StatusCode)

		// --- Test creating without owner label -----------------------------------

		resp, err = requestor("http://localhost:8842/api/v1/workflows/test", http.MethodPost, wf6) // wf 7 (use no owner)
		s.NoError(err)
		s.Equal(http.StatusBadRequest, resp.StatusCode)

		// --- Check if content is still as expected -------------------------------

		resp, err = requestor("http://localhost:8842/api/v1/workflows/test", http.MethodGet, "") // should be 3 {wf1, wf3, wf4}
		s.NoError(err)
		s.Equal(http.StatusOK, resp.StatusCode)

		buf3 := new(bytes.Buffer)
		buf3.ReadFrom(resp.Body)
		json.Unmarshal(buf3.Bytes(), &struk)
		s.Equal(3, len(struk.Items))

		// --- Delete all resources, test delete of already removed resources-------

		resp, err = requestor("http://localhost:8842/api/v1/workflows/test/hello-world-b6h5m-test", http.MethodDelete, "")
		s.NoError(err)
		s.Equal(http.StatusOK, resp.StatusCode)

		resp, err = requestor("http://localhost:8842/api/v1/workflows/test/hello-world-9tql2-test", http.MethodDelete, "")
		s.NoError(err)
		s.Equal(http.StatusNotFound, resp.StatusCode)

		resp, err = requestor("http://localhost:8842/api/v1/workflows/test/hello-world-b6h5m", http.MethodDelete, "")
		s.NoError(err)
		s.Equal(http.StatusOK, resp.StatusCode)

		resp, err = requestor("http://localhost:8842/api/v1/workflows/test/hello-world-b6h5m", http.MethodDelete, "")
		s.NoError(err)
		s.Equal(http.StatusNotFound, resp.StatusCode)

		resp, err = requestor("http://localhost:8842/api/v1/workflows/test/wf4", http.MethodDelete, "")
		s.NoError(err)
		s.Equal(http.StatusOK, resp.StatusCode)

		// --- In the end there should be no workflows left ------------------------

		resp, err = requestor("http://localhost:8842/api/v1/workflows/test", http.MethodGet, "") // should be {}
		s.NoError(err)
		s.Equal(http.StatusOK, resp.StatusCode)

		var struk2 wfv1.WorkflowList
		buf4 := new(bytes.Buffer)
		buf4.ReadFrom(resp.Body)
		json.Unmarshal(buf4.Bytes(), &struk2)
		s.Equal(0, len(struk2.Items))
	}
*/
func marshalResponse(resp *http.Response, obj any) error {
	buffer := new(bytes.Buffer)
	buffer.ReadFrom(resp.Body)
	logrus.Info("Buffer: ", buffer)
	return json.Unmarshal(buffer.Bytes(), obj)
}
