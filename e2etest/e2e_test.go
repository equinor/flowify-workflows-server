package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/equinor/flowify-workflows-server/apiserver"
	"github.com/equinor/flowify-workflows-server/models"
	fuser "github.com/equinor/flowify-workflows-server/user"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type e2eTestSuite struct {
	suite.Suite
	client     *http.Client
	kubeclient *kubernetes.Clientset
}

func TestE2ETestSuite(t *testing.T) {
	suite.Run(t,
		&e2eTestSuite{
			client: &http.Client{Timeout: 1 * time.Hour},
		})
}

var mockUser fuser.User = fuser.MockUser{
	Uid:   "0",
	Name:  "Auth Disabled",
	Email: "auth@disabled",
	Roles: []fuser.Role{"tester", "dummy", "sandbox-developer"},
}

// TODO: not in use, add clean workspace for e2e tests in k8s
// var testWorkspace string = `
// ---
// # Namespace 'sandbox-project-a'
// apiVersion: v1
// kind: Namespace
// metadata:
//   labels:
//     app.kubernetes.io/part-of: "flowify"
//   name: "test"

// ---
// # Developer workspace environment
// apiVersion: v1
// kind: ConfigMap
// metadata:
//     labels:
//         app.kubernetes.io/component: "workspace-config"
//         app.kubernetes.io/part-of: "flowify"
//     name: "test"
//     namespace: "test"
// data:
//     roles: "[[\"tester\"]]"
// `

var configString = []byte(`
db:
  # select which db to use
  select: mongo
  # the flowify document database
  dbname: e2e-test
#  mongo:
  config:
    # Mongo fields
    # (FLOWIFY_)DB_CONFIG_ADDRESS=...
    # url to database
    address: localhost
    # port where mongo is listening
    port: 27017

kubernetes:
  # how to locate the kubernetes server
  kubeconfigpath: SET_FROM_ENV
  # the namespace containing the flowify configuration and setup
  namespace: flowify-e2e

auth:
  handler: azure-oauth2-openid-token
  config:
    issuer: e2e-test-runner
    audience: e2e-test
#    keysurl: http://localhost:32023/jwkeys/
    keysurl: SET_FROM_ENV

logging:
  loglevel: info

server:
  port: 8443
`)

var cfg apiserver.Config
var server_addr string

func make_authentication_header(usr fuser.User, secret string) (string, error) {
	jwtUser := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"name":  usr.GetName(),
		"email": usr.GetEmail(),
		"roles": usr.GetRoles(),
		"iat":   time.Now().Unix(),
		"nbf":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Minute * 5).Unix(),
		"aud":   "e2e-test",        //cfg.AuthConfig.Config["audience"].(string),
		"iss":   "e2e-test-runner", //cfg.AuthConfig.Config["issuer"].(string),
	})
	tokenString, err := jwtUser.SignedString([]byte(secret))
	if err != nil {
		return "", errors.Wrap(err, "could not make authentication string")
	}
	return "Bearer " + tokenString, nil
}

func (s *e2eTestSuite) SetupSuite() {
	log.Info("Setting up e2eTestSuite")

	var err error
	cfg, err = apiserver.LoadConfigFromReader(bytes.NewBuffer(configString))
	s.NoError(err)

	log.Info(cfg)
	fmt.Println("Config:\n", cfg.String())

	ctx := context.Background()

	s.client = &http.Client{}
	s.client.Timeout = time.Second * 30

	if apiserver.CommitSHA == "" {
		log.Info("Trying to set build info from shel input")
		cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
		if stdout, err := cmd.Output(); err == nil {
			apiserver.CommitSHA = strings.TrimSuffix(string(stdout), "\n")
			apiserver.BuildTime = time.Now().UTC().Format(time.RFC3339)
		} else {
			log.Error("Failed to set build info from shell")
			s.Require().NoError(err, "Could not set up suite")
		}
	}

	server, err := apiserver.NewFlowifyServerFromConfig(cfg)
	require.NoError(s.T(), err, "cant recover without server")

	s.kubeclient = server.GetKubernetesClient().(*kubernetes.Clientset)

	ready := make(chan bool, 1)

	go server.Run(ctx, &ready)

	wsName := cfg.KubernetesKonfig.Namespace
	if _, err := s.kubeclient.CoreV1().Namespaces().Get(context.TODO(), cfg.KubernetesKonfig.Namespace, metav1.GetOptions{}); k8serrors.IsNotFound(err) {
		ns := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name:      cfg.KubernetesKonfig.Namespace,
			Namespace: cfg.KubernetesKonfig.Namespace}}
		ns, err = s.kubeclient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		s.NoError(err)
	}

	if _, err := s.kubeclient.CoreV1().ConfigMaps(cfg.KubernetesKonfig.Namespace).Get(context.TODO(), wsName, metav1.GetOptions{}); k8serrors.IsNotFound(err) {
		ws_test := &v1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name:      wsName,
			Namespace: cfg.KubernetesKonfig.Namespace,
			Labels:    map[string]string{"app.kubernetes.io/component": "workspace-config"},
		}, Data: map[string]string{"roles": "[[\"tester\"]]"}}
		ws_test, err = s.kubeclient.CoreV1().ConfigMaps(cfg.KubernetesKonfig.Namespace).Create(context.TODO(), ws_test, metav1.CreateOptions{})
		s.NoError(err)
	}

	// make sure we get the ready signal
	s.Equal(true, <-ready)

	server_addr = "http://" + server.GetAddress()

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

func make_authenticated_requestor(client *http.Client, usr fuser.User) func(string, string, string) (*http.Response, error) {
	if usr == nil {
		usr = mockUser
	}

	// inject auth user
	jwtUser := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"name":  usr.GetName(),
		"email": usr.GetEmail(),
		"roles": usr.GetRoles(),
		"iat":   time.Now().Unix(),
		"nbf":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Minute * 5).Unix(),
		"aud":   cfg.AuthConfig.Config["audience"].(string),
		"iss":   cfg.AuthConfig.Config["issuer"].(string),
	})
	const secretKey = "my_secret_key"
	tokenString, err := jwtUser.SignedString([]byte(secretKey))
	if err != nil {
		panic("unexpected")
	}
	auth_header := "Bearer " + tokenString

	return func(url, method string, payload string) (*http.Response, error) {
		return make_authenticated_request_with_client(url, method, payload, auth_header, client)
	}
}

type nameList struct {
	Names []string `json:"names"`
}

func make_authenticated_request_with_client(url, method string, payload string, auth_header string, client *http.Client) (*http.Response, error) {
	req, _ := http.NewRequest(method, url, strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", auth_header)
	return client.Do(req)
}

func make_request_with_client(url, method string, payload string, client *http.Client) (*http.Response, error) {
	req, _ := http.NewRequest(method, url, strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	auth, err := make_authentication_header(mockUser, "my_secret")
	if err != nil {
		logrus.Error("could not create auth")
		return nil, errors.Wrap(err, "could not create request")
	}
	req.Header.Set("Authorization", auth)
	return client.Do(req)
}

func (s *e2eTestSuite) Test_zpages() {
	resp, err := http.Get(server_addr + "/versionz")
	require.Nil(s.T(), err)

	s.Equal(http.StatusOK, resp.StatusCode)
	var git_sha string
	if apiserver.CommitSHA != "" {
		git_sha = apiserver.CommitSHA
	} else {
		cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
		stdout, err := cmd.Output()
		s.Require().NoError(err, "not set in build and not a git dir")
		git_sha = strings.TrimSuffix(string(stdout), "\n")
	}

	// test body
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	s.Equal(git_sha, buf.String(), "versionz returns git hash in body")

	// test headers
	s.Equal(git_sha, resp.Header.Get("X-Flowify-Version"), "version also set in X-header")
	buildtime, err := time.Parse(time.RFC3339, resp.Header.Get("X-Flowify-Buildtime"))
	s.NotEmpty(buildtime, "make sure we get a datetime string")
	s.NoError(err)
	s.Equal(resp.Header.Get("X-Wrong"), "")
}

func (s *e2eTestSuite) Test_Userinfo() {

	type testCase struct {
		User           fuser.User
		Name           string
		Auth           string
		ExpectedStatus int
	}

	jwtUser := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"name":  mockUser.GetName(),
		"email": mockUser.GetEmail(),
		"roles": mockUser.GetRoles(),
		"iat":   time.Now().Unix(),
		"nbf":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Minute * 5).Unix(),
		"aud":   cfg.AuthConfig.Config["audience"].(string),
		"iss":   cfg.AuthConfig.Config["issuer"].(string),
	})
	const secretKey = "my_secret_key"
	tokenString, err := jwtUser.SignedString([]byte(secretKey))
	s.NoError(err)

	testCases := []testCase{
		{
			User:           nil,
			Name:           "No auth",
			Auth:           "",
			ExpectedStatus: http.StatusBadRequest},
		{
			User:           mockUser,
			Name:           "JWT-Encoded",
			Auth:           tokenString,
			ExpectedStatus: http.StatusOK},
	}

	for _, test := range testCases {
		s.T().Run(test.Name, func(t *testing.T) {
			// prepare request
			req, _ := http.NewRequest(http.MethodGet, server_addr+"/api/v1/userinfo/", nil)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+test.Auth)

			resp, err := s.client.Do(req)
			require.NoError(t, err)
			assert.NotEmpty(t, resp)
			assert.Equal(t, test.ExpectedStatus, resp.StatusCode)

		})
	}
}

func (s *e2eTestSuite) Test_Roundtrip_Component() {

	requestor := make_authenticated_requestor(s.client, mockUser)

	cmp1, err := os.ReadFile("../models/examples/minimal-any-component.json")
	s.NoError(err)
	cmpReq := fmt.Sprintf(`
	{
		"options": {},
		"component": %s
	}`, cmp1)

	resp, err := requestor(server_addr+"/api/v1/components/", http.MethodPost, cmpReq)
	require.NoError(s.T(), err)
	require.Equal(s.T(), http.StatusCreated, resp.StatusCode)

	var cmpResp models.Component
	err = marshalResponse(ResponseBodyBytes(resp), &cmpResp)
	s.NoError(err)

	resp2, err := requestor(fmt.Sprintf(server_addr+"/api/v1/components/%s", cmpResp.Metadata.Uid.String()), http.MethodGet, cmpReq)
	s.NoError(err)
	require.Equal(s.T(), http.StatusOK, resp2.StatusCode)

	var cmpResp2 models.Component
	err = marshalResponse(ResponseBodyBytes(resp2), &cmpResp2)
	s.NoError(err)
	s.Equal(cmpResp, cmpResp2, "expect roundtrip equality")

}

func (s *e2eTestSuite) TestWorkspaceCreate() {
	requestor := make_authenticated_requestor(s.client, mockUser)

	id := uuid.New()
	body := fmt.Sprintf("{\"Name\":\"new-workspace-%s\", \"Roles\":[\"sandbox-developer\"]}", id.String())

	resp, err := requestor(server_addr+"/api/v1/workspaces/", http.MethodPost, body)
	require.NoError(s.T(), err)
	require.Equal(s.T(), http.StatusCreated, resp.StatusCode)
}

func (s *e2eTestSuite) Test_Roundtrip_Workflow() {
	requestor := make_authenticated_requestor(s.client, mockUser)

	data, _ := os.ReadFile("../models/examples/minimal-any-workflow.json")
	wfReq := fmt.Sprintf(`
	{
		"options": {},
		"workflow": %s
	}`, data)

	resp, err := requestor(server_addr+"/api/v1/workflows/", http.MethodPost, wfReq)
	s.NoError(err)
	require.Equal(s.T(), http.StatusCreated, resp.StatusCode, BodyStringer{resp.Body})

	var wfResp models.Workflow
	err = marshalResponse(ResponseBodyBytes(resp), &wfResp)
	s.NoError(err)

	resp2, err := requestor(fmt.Sprintf(server_addr+"/api/v1/workflows/%s", wfResp.Metadata.Uid.String()), http.MethodGet, wfReq)
	s.NoError(err)

	var wfResp2 models.Workflow
	err = marshalResponse(ResponseBodyBytes(resp2), &wfResp2)
	s.NoError(err)
	s.Equal(wfResp, wfResp2, "expect roundtrip equality")

}

type BodyStringer struct {
	rc io.ReadCloser
}

// bodystringer doesn't consume the response body until actually
// trying to print the output.
// It is useful to put in test-debug constructs:
//
//	resp, err := req...
//	s.NoError(err, BodyStringer{resp.Body})
//	s.Equal(ExpectedCode, resp.StatusCode, BodyStringer{resp.Body})
func (d BodyStringer) String() string {
	return string(d.Bytes())
}
func (d BodyStringer) Bytes() []byte {
	buf := new(bytes.Buffer)
	defer d.rc.Close()
	if _, err := buf.ReadFrom(d.rc); err != nil {
		return []byte("Error: " + err.Error())
	}
	return buf.Bytes()
}

func ResponseBodyBytes(resp *http.Response) []byte {
	return BodyStringer{resp.Body}.Bytes()
}

func (s *e2eTestSuite) Test_Roundtrip_Job() {
	requestor := make_authenticated_requestor(s.client, mockUser)

	data, _ := os.ReadFile("../models/examples/job-example.json")
	wfReq := fmt.Sprintf(`
	{
		"options": {},
		"job": %s
	}`, data)

	resp, err := requestor(server_addr+"/api/v1/jobs/", http.MethodPost, wfReq)
	s.NoError(err, BodyStringer{resp.Body})

	require.Equal(s.T(), http.StatusCreated, resp.StatusCode, BodyStringer{resp.Body})

	var wfResp models.Job
	err = marshalResponse(ResponseBodyBytes(resp), &wfResp)
	s.NoError(err)

	resp2, err := requestor(fmt.Sprintf(server_addr+"/api/v1/jobs/%s", wfResp.Metadata.Uid.String()), http.MethodGet, wfReq)
	s.NoError(err)

	var wfResp2 models.Job
	err = marshalResponse(ResponseBodyBytes(resp2), &wfResp2)
	s.NoError(err)
	s.Equal(wfResp, wfResp2, "expect roundtrip equality")
}

func marshalResponse(data []byte, obj any) error {
	//logrus.Info("Marshal data: ", data)
	return json.Unmarshal(data, obj)
}
