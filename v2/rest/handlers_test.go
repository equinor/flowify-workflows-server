package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned/fake"
	"github.com/equinor/flowify-workflows-server/auth"
	"github.com/equinor/flowify-workflows-server/pkg/workspace"
	"github.com/equinor/flowify-workflows-server/v2/models"
	"github.com/equinor/flowify-workflows-server/v2/storage"
	"github.com/equinor/flowify-workflows-server/v2/user"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	gmux "github.com/gorilla/mux"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	ktesting "k8s.io/client-go/testing"
)

func init() {
	// log.SetOutput(ioutil.Discard)
}

var (
	cmp1, _ = ioutil.ReadFile("../models/examples/minimal-any-component.json")
	cmpReq  = []byte(fmt.Sprintf(`
	{
		"options": {},
		"component": %s
	}`, cmp1))

	wf1, _ = ioutil.ReadFile("../models/examples/minimal-any-workflow.json")
	wfReq  = []byte(fmt.Sprintf(`
{
	"options": {},
	"workflow": %s
}`, wf1))
)

// implement a mock client/db for testing
type componentClient struct {
	mock.Mock
}

func NewMockClient() *componentClient {
	return &componentClient{}
}

func (c *componentClient) GetComponent(ctx context.Context, id interface{}) (models.Component, error) {
	args := c.Called(ctx, id)
	return args.Get(0).(models.Component), args.Error(1)
}

func (c *componentClient) GetWorkflow(ctx context.Context, id interface{}) (models.Workflow, error) {
	args := c.Called(ctx, id)
	return args.Get(0).(models.Workflow), args.Error(1)
}

func (c *componentClient) CreateComponent(ctx context.Context, node models.Component) error {
	args := c.Called(ctx, node)
	return args.Error(0)
}

func (c *componentClient) PutComponent(ctx context.Context, node models.Component) error {
	args := c.Called(ctx, node)
	return args.Error(0)
}

func (c *componentClient) PutWorkflow(ctx context.Context, node models.Workflow) error {
	args := c.Called(ctx, node)
	return args.Error(0)
}

func (c *componentClient) CreateWorkflow(ctx context.Context, node models.Workflow) error {
	args := c.Called(ctx, node)
	return args.Error(0)
}

func (c *componentClient) ListComponentsMetadata(ctx context.Context, pagination storage.Pagination, filterquery []string, sortquery []string) (models.MetadataList, error) {
	args := c.Called(ctx, filterquery, sortquery)

	if args.Get(0) == nil {
		return models.MetadataList{}, args.Error(1)
	} else {
		return args.Get(0).(models.MetadataList), args.Error(1)
	}
}

func (c *componentClient) ListComponentVersionsMetadata(ctx context.Context, id models.ComponentReference, pagination storage.Pagination, sortquery []string) (models.MetadataList, error) {
	args := c.Called(ctx, sortquery)

	if args.Get(0) == nil {
		return models.MetadataList{}, args.Error(1)
	} else {
		return args.Get(0).(models.MetadataList), args.Error(1)
	}
}

func (c *componentClient) ListWorkflowsMetadata(ctx context.Context, pagination storage.Pagination, filterquery []string, sortquery []string) (models.MetadataWorkspaceList, error) {
	args := c.Called(ctx, filterquery, sortquery)

	if args.Get(0) == nil {
		return models.MetadataWorkspaceList{}, args.Error(1)
	} else {
		return args.Get(0).(models.MetadataWorkspaceList), args.Error(1)
	}
}

func (c *componentClient) ListWorkflowVersionsMetadata(ctx context.Context, id models.ComponentReference, pagination storage.Pagination, sortquery []string) (models.MetadataWorkspaceList, error) {
	args := c.Called(ctx, sortquery)

	if args.Get(0) == nil {
		return models.MetadataWorkspaceList{}, args.Error(1)
	} else {
		return args.Get(0).(models.MetadataWorkspaceList), args.Error(1)
	}
}

func (c *componentClient) ListJobsMetadata(ctx context.Context, pagination storage.Pagination, filterquery []string, sortquery []string) (models.MetadataWorkspaceList, error) {
	args := c.Called(ctx, filterquery, sortquery)

	if args.Get(0) == nil {
		return models.MetadataWorkspaceList{}, args.Error(1)
	} else {
		return args.Get(0).(models.MetadataWorkspaceList), args.Error(1)
	}
}

func (c *componentClient) CreateJob(ctx context.Context, node models.Job) error {
	args := c.Called(ctx, node)
	return args.Error(0)
}

func (c *componentClient) GetJob(ctx context.Context, id models.ComponentReference) (models.Job, error) {
	args := c.Called(ctx, id)
	return args.Get(0).(models.Job), args.Error(1)
}

func (c *componentClient) DeleteDocument(ctx context.Context, kind storage.DocumentKind, id models.CRefVersion) (models.CRefVersion, error) {
	args := c.Called(ctx, kind, id)
	return args.Get(0).(models.CRefVersion), args.Error(1)
}

func (c *componentClient) PatchComponent(ctx context.Context, node models.Component, oldTimestamp time.Time) (models.Component, error) {
	args := c.Called(ctx, node, oldTimestamp)
	return args.Get(0).(models.Component), args.Error(1)
}

func (c *componentClient) PatchWorkflow(ctx context.Context, node models.Workflow, oldTimestamp time.Time) (models.Workflow, error) {
	args := c.Called(ctx, node, oldTimestamp)
	return args.Get(0).(models.Workflow), args.Error(1)
}

type testCase struct {
	Name                       string
	Method                     string
	URL                        string
	Body                       []byte
	ExpectedResponseStatusCode int
	Headers                    map[string]string
	ExpectedResponseHeaders    map[string]string
}

func stringify(item any) []byte {
	raw, err := json.Marshal(item)
	if err != nil {
		log.Panic(err)
	}
	return raw
}

func Test_ComponentHTTPHandler(t *testing.T) {
	c1 := models.Component{ComponentBase: models.ComponentBase{Type: "component"},
		Implementation: models.Any{ImplementationBase: models.ImplementationBase{Type: "any"}}}
	c2Uid := models.NewComponentReference()
	c2v1 := models.Component{ComponentBase: models.ComponentBase{Type: "component", Metadata: models.Metadata{Version: models.Version{Current: models.VersionInit}, Uid: c2Uid}},
		Implementation: models.Any{ImplementationBase: models.ImplementationBase{Type: "any"}}}
	c2v2 := c2v1
	c2v2.Metadata.Version = models.Version{Current: c2v1.Version.Current + 1, Previous: models.CRefVersion{Version: c2v1.Version.Current}, Tags: []string{models.VersionTagLatest}}
	w1v1 := models.Workflow{Metadata: models.Metadata{Uid: c2Uid, Version: models.Version{Current: models.VersionNumber(5)}}, Workspace: "test"}
	w1v2 := w1v1
	w1v2.Metadata.Version = models.Version{Current: w1v1.Version.Current + 1, Previous: models.CRefVersion{Version: w1v2.Version.Current}, Tags: []string{models.VersionTagLatest}}
	crefver := models.CRefVersion{Uid: c2Uid, Version: c2v1.Version.Current}
	wrefver := models.CRefVersion{Uid: w1v2.Uid, Version: w1v2.Version.Current}
	require.NoError(t, json.Unmarshal(cmp1, &c1))
	require.NoError(t, json.Unmarshal(cmp1, &c2v1))
	require.NoError(t, json.Unmarshal(cmp1, &c2v2))

	client := NewMockClient()
	client.On("GetComponent", mock.Anything, mock.Anything).Return(c1, nil)
	client.On("GetComponent", mock.Anything, c2Uid).Return(c2v2, nil)
	client.On("GetComponent", mock.Anything, crefver).Return(c2v1, nil)
	client.On("PutComponent", mock.Anything, mock.Anything).Return(nil, nil)
	client.On("PutWorkflow", mock.Anything, mock.Anything).Return(nil, nil)

	compWithUid := c1
	compWithUid.ComponentBase.Uid = models.ComponentReference(uuid.MustParse("f0b85568-afd3-4ed0-b8f2-18cd813c5239"))

	var w1 models.Workflow
	require.NoError(t, json.Unmarshal(wf1, &w1))
	wfWithUid := w1
	wfWithUid.Uid = models.ComponentReference(uuid.MustParse("c0661adb-d7a2-4820-b15c-f81ff9d87120"))
	wfWithUidBytes, _ := json.Marshal(wfWithUid)

	client.On("ListComponentsMetadata", mock.Anything, []string(nil), []string(nil)).Return(models.MetadataList{Items: []models.Metadata{c1.Metadata, c1.Metadata}, PageInfo: models.PageInfo{TotalNumber: 2, Skip: 0, Limit: 0}}, nil)
	client.On("ListComponentVersionsMetadata", mock.Anything, []string(nil)).Return(models.MetadataList{Items: []models.Metadata{c2v1.Metadata, c2v2.Metadata}, PageInfo: models.PageInfo{TotalNumber: 2, Skip: 0, Limit: 0}}, nil)
	client.On("ListWorkflowsMetadata", mock.Anything, []string(nil), []string(nil)).Return(models.MetadataWorkspaceList{Items: []models.MetadataWorkspace{{Metadata: c1.Metadata, Workspace: "test"}}}, nil)
	client.On("ListWorkflowVersionsMetadata", mock.Anything, []string(nil)).Return(
		models.MetadataWorkspaceList{
			Items:    []models.MetadataWorkspace{{Metadata: w1v1.Metadata, Workspace: w1v1.Workspace}},
			PageInfo: models.PageInfo{TotalNumber: 2, Skip: 0, Limit: 0}},
		nil,
	)
	client.On("CreateComponent", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	client.On("CreateWorkflow", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	client.On("GetWorkflow", mock.Anything, mock.Anything, mock.Anything).Return(w1, nil)

	client.On("DeleteDocument", mock.Anything, storage.ComponentKind, crefver).Return(crefver, nil)
	client.On("DeleteDocument", mock.Anything, storage.WorkflowKind, wrefver).Return(wrefver, nil)

	c2v2u1 := c2v2
	c2v2u1.Description = "Updated description"
	client.On("PatchComponent", mock.Anything, mock.Anything, c2v1.Timestamp).Return(c2v2u1, nil)
	w1v2u1 := wfWithUid
	w1v2u1.Name = "Updated name"
	client.On("PatchWorkflow", mock.Anything, mock.Anything, w1v2.Timestamp).Return(w1v2u1, nil)
	c2v2u1baduid := c2v2u1
	c2v2u1baduid.Uid = models.NewComponentReference()

	mux := gmux.NewRouter()
	RegisterComponentRoutes(mux.PathPrefix("/api/v2"), client)
	RegisterWorkflowRoutes(mux.PathPrefix("/api/v2"), client)

	testcases := []testCase{
		{Name: "list components", Method: http.MethodGet, URL: "/api/v2/components/", Body: nil, ExpectedResponseStatusCode: http.StatusOK, Headers: nil, ExpectedResponseHeaders: map[string]string{}},
		{Name: "post component", Method: http.MethodPost, URL: "/api/v2/components/", Body: cmpReq, ExpectedResponseStatusCode: http.StatusCreated, Headers: map[string]string{"Content-Type": "application/json"}, ExpectedResponseHeaders: map[string]string{"Location": "/api/v2/components/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"}},
		{Name: "get component", Method: http.MethodGet, URL: "/api/v2/components/" + c1.Uid.String(), Body: cmpReq, ExpectedResponseStatusCode: http.StatusOK, Headers: nil, ExpectedResponseHeaders: map[string]string{}}, //"Location": "/api/v2/components/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"}},
		{Name: "put component", Method: http.MethodPut, URL: "/api/v2/components/" + compWithUid.Uid.String(), Body: []byte(fmt.Sprintf(`{ "component": %s , "options": {}}`, stringify(compWithUid))), ExpectedResponseStatusCode: http.StatusNoContent, Headers: map[string]string{"Content-Type": "application/json"}, ExpectedResponseHeaders: map[string]string{"Location": "/api/v2/components/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"}},
		{Name: "list component versions", Method: http.MethodGet, URL: "/api/v2/components/" + c2Uid.String() + "/versions/", Body: nil, ExpectedResponseStatusCode: http.StatusOK, Headers: nil, ExpectedResponseHeaders: map[string]string{}},
		{Name: "list workflows", Method: http.MethodGet, URL: "/api/v2/workflows/", Body: nil, ExpectedResponseStatusCode: http.StatusOK, Headers: nil, ExpectedResponseHeaders: map[string]string{}},
		{Name: "post workflow", Method: http.MethodPost, URL: "/api/v2/workflows/", Body: wfReq, ExpectedResponseStatusCode: http.StatusCreated, Headers: map[string]string{"Content-Type": "application/json"}, ExpectedResponseHeaders: map[string]string{"Location": "/api/v2/workflows/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"}},
		{Name: "get workflow", Method: http.MethodGet, URL: "/api/v2/workflows/" + wfWithUid.Uid.String(), Body: wfWithUidBytes, ExpectedResponseStatusCode: http.StatusOK, Headers: map[string]string{"Content-Type": "application/json"}, ExpectedResponseHeaders: map[string]string{}},
		{Name: "put workflow", Method: http.MethodPut, URL: "/api/v2/workflows/" + wfWithUid.Uid.String(), Body: stringify(models.WorkflowPostRequest{Workflow: wfWithUid}), ExpectedResponseStatusCode: http.StatusNoContent, Headers: map[string]string{"Content-Type": "application/json"}, ExpectedResponseHeaders: map[string]string{}},
		{Name: "list workflow versions", Method: http.MethodGet, URL: "/api/v2/workflows/" + c2Uid.String() + "/versions/", Body: nil, ExpectedResponseStatusCode: http.StatusOK, Headers: nil, ExpectedResponseHeaders: map[string]string{}},
		{Name: "delete component", Method: http.MethodDelete, URL: "/api/v2/components/" + crefver.Uid.String() + "/" + crefver.Version.String(), Body: nil, ExpectedResponseStatusCode: http.StatusNoContent, Headers: nil, ExpectedResponseHeaders: map[string]string{}},
		{Name: "delete workflow", Method: http.MethodDelete, URL: "/api/v2/workflows/" + wrefver.Uid.String() + "/" + wrefver.Version.String(), Body: nil, ExpectedResponseStatusCode: http.StatusNoContent, Headers: nil, ExpectedResponseHeaders: map[string]string{}},
		{Name: "patch component", Method: http.MethodPatch, URL: "/api/v2/components/" + c2v2.Uid.String(), Body: []byte(fmt.Sprintf(`{ "component": %s , "options": {}}`, stringify(c2v2u1))), ExpectedResponseStatusCode: http.StatusOK, Headers: map[string]string{"Content-Type": "application/json"}, ExpectedResponseHeaders: map[string]string{"Location": "/api/v2/components/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"}},
		{Name: "patch component bad uid", Method: http.MethodPatch, URL: "/api/v2/components/" + c2v2.Uid.String(), Body: []byte(fmt.Sprintf(`{ "component": %s , "options": {}}`, stringify(c2v2u1baduid))), ExpectedResponseStatusCode: http.StatusBadRequest, Headers: map[string]string{"Content-Type": "application/json"}, ExpectedResponseHeaders: map[string]string{}},
		{Name: "patch workflow", Method: http.MethodPatch, URL: "/api/v2/workflows/" + w1v2u1.Uid.String(), Body: stringify(models.WorkflowPostRequest{Workflow: w1v2u1}), ExpectedResponseStatusCode: http.StatusOK, Headers: map[string]string{"Content-Type": "application/json"}, ExpectedResponseHeaders: map[string]string{"Location": "/api/v2/workflows/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"}},
	}

	for _, test := range testcases {
		t.Run(test.Name, func(t *testing.T) {
			log.Info("Test: ", test.Name, test.URL)
			var payload []byte
			var err error

			defer func() {
				if t.Failed() {
					log.Errorf("test %s response payload: %s", test.Name, string(payload))
				}
			}()

			req := httptest.NewRequest(test.Method, test.URL, bytes.NewReader(test.Body))
			for k, v := range test.Headers {
				req.Header.Add(k, v)
			}

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			res := w.Result()

			defer res.Body.Close()
			payload, err = ioutil.ReadAll(res.Body)
			require.NoError(t, err)
			if test.ExpectedResponseStatusCode != http.StatusNoContent {
				require.True(t, json.Valid(payload))
			}

			require.Equal(t, test.ExpectedResponseStatusCode, w.Code, w.Body)
			require.Len(t, res.Header["Content-Type"], 1)
			require.Equal(t, "application/json", res.Header["Content-Type"][0])

			for k, v := range test.ExpectedResponseHeaders {
				require.GreaterOrEqual(t, len(res.Header[k]), 1)
				require.Regexp(t, regexp.MustCompile(v), res.Header[k][0])
			}

			if test.URL == "/api/v2/components/" && test.Method == http.MethodGet && w.Code == http.StatusOK {
				roundtrip := models.MetadataList{}
				require.NoError(t, json.Unmarshal(payload, &roundtrip))
				require.Len(t, roundtrip.Items, 2)
			}
		})
	}
}

const jobSubmitRequest = `{
	"job": {
	  "name": "test-job",
	  "type": "job",
	  "workflow": {
	    "workspace": "test",
		"type": "workflow",
		"component": {
		  "type": "component",
 		  "uid": "00000000-0000-0000-0000-000000000001",
		  "inputs": [],
		  "outputs": [],
		  "implementation": { 
		    "type": "brick",
			"container": {
			  "name": "containername",
			  "image": "docker/whalesay",
			  "command": ["cowsay"],
			  "args": ["Hello Test"]
			}
		  }
		}
	  }
	},
	"options": {
		"tags": [
			"testing",
			"mocking",
			"general awesomeness"
		]
	}
}`

func UIDReactor(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
	wf := action.(ktesting.CreateAction).GetObject().(*v1alpha1.Workflow)
	wf.SetUID("3b971ada-15d6-4422-abbb-8ecf4a3f90bb")

	return true, wf, nil
}

func GetReactor(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
	wf := v1alpha1.Workflow{}
	wf.SetUID("3b971ada-15d6-4422-abbb-8ecf4a3f90bb")
	wf.Status.FinishedAt = metav1.NewTime(time.Now())
	return true, &wf, nil
}

func DeleteReactor(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
	return true, nil, nil
}

func Test_JobSubmitHTTPHandler(t *testing.T) {
	client := NewMockClient()

	id := models.NewReference("9b75d74e-681c-496a-bc43-496a798b9fc8")
	client.On("GetWorkflow", mock.Anything, id).Return(
		models.Workflow{Metadata: models.Metadata{Uid: models.NewComponentReference(), Name: "test-wf"},
			Component: models.Component{ComponentBase: models.ComponentBase{Metadata: models.Metadata{Name: "test-comp", Uid: models.NewComponentReference()},
				Inputs:  make([]models.Data, 0),
				Outputs: make([]models.Data, 0)}},
			Type:      "workflow",
			Workspace: "test"},
		nil)
	client.On("CreateJob", mock.Anything, mock.Anything).Return(nil)

	argoClientSet := fake.NewSimpleClientset()
	argoClientSet.PrependReactor("create", "workflows", UIDReactor)
	mux := gmux.NewRouter()
	RegisterJobRoutes(mux.PathPrefix("/api/v2"), client, argoClientSet)

	testcases := []testCase{
		{Name: "submit jobs", Method: http.MethodPost, URL: "/api/v2/jobs/", Body: []byte(jobSubmitRequest), ExpectedResponseStatusCode: http.StatusCreated, Headers: nil, ExpectedResponseHeaders: map[string]string{"Location": "/api/v2/jobs/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"}},
	}

	for _, test := range testcases {
		t.Run(test.Name, func(t *testing.T) {
			var payload []byte
			var err error

			defer func() {
				if t.Failed() {
					log.Errorf("test '%s' response payload: %s", test.Name, string(payload))
				}
			}()

			req := httptest.NewRequest(test.Method, test.URL, bytes.NewReader(test.Body))
			req.Header["Content-Type"] = []string{"application/json"}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			res := w.Result()

			defer res.Body.Close()
			payload, err = ioutil.ReadAll(res.Body)
			require.NoError(t, err)
			require.True(t, json.Valid(payload))

			require.Equal(t, test.ExpectedResponseStatusCode, w.Code, string(payload))
			require.Len(t, res.Header["Content-Type"], 1)
			require.Equal(t, "application/json", res.Header["Content-Type"][0])

			for k, v := range test.ExpectedResponseHeaders {
				require.Regexp(t, regexp.MustCompile(v), res.Header[k][0])
			}
		})
	}
}

func Test_PermissionMiddleware(t *testing.T) {
	mux := gmux.NewRouter()
	subrouter := mux.PathPrefix("/").Subrouter()

	mockUser := user.MockUser{Uid: "nonce", Name: "John Doe", Email: "user@test.com", Roles: []user.Role{"role1", "role2"}}
	jwtUser := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"name":  mockUser.Name,
		"email": mockUser.Email,
		"roles": mockUser.Roles,
		"iat":   time.Now().Unix(),
		"nbf":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Minute * 5).Unix(),
		"aud":   "tester",
		"iss":   "permission-handler-test",
	})
	const secretKey = "my_secret_key"
	tokenString, err := jwtUser.SignedString([]byte(secretKey))
	require.Nil(t, err)

	subrouter.HandleFunc("/with-tokens", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, mockUser.Email, user.GetUser(r.Context()).GetEmail())
		require.ElementsMatch(t, mockUser.Roles, user.GetUser(r.Context()).GetRoles())
		w.WriteHeader(http.StatusOK)
	})

	subrouter.HandleFunc("/no-tokens", func(w http.ResponseWriter, r *http.Request) {
		// this handler is prevented by the middleware (if it works correctly)
		require.Nil(t, user.GetUser(r.Context()))
		w.WriteHeader(http.StatusNotFound)
	})

	keyFunc := func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return []byte{}, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secretKey), nil
	}
	subrouter.Use(NewAuthenticationMiddleware(auth.AzureTokenAuthenticator{
		Issuer:   "permission-handler-test",
		Audience: "tester",
		KeyFunc:  keyFunc}))
	//subrouter.Use(NewAuthenticationMiddleware(auth.MockAuthenticator{User: mockUser}))

	t.Run("with token", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/with-tokens", nil)
		req.Header.Set("authorization", fmt.Sprintf("Bearer %s", tokenString))
		mux.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body)
	})

	t.Run("without token", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/no-tokens", nil)
		mux.ServeHTTP(w, req)
		// the middleware should prevent the running of the handler
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body)
	})
}

func Test_JobEventHTTPHandler(t *testing.T) {
	// Fake client will not use the fieldselector.
	argoClientSet := fake.NewSimpleClientset(&v1alpha1.Workflow{ObjectMeta: metav1.ObjectMeta{Namespace: "testing"}})
	watcher := watch.NewFake()
	argoClientSet.PrependWatchReactor("workflows", ktesting.DefaultWatchReactor(watcher, nil))

	go func() {
		defer watcher.Stop()
		for i := 0; i < 3; i++ {
			watcher.Add(&v1alpha1.Workflow{})
		}
	}()

	mux := gmux.NewRouter()
	RegisterJobRoutes(mux.PathPrefix("/api/v2"), nil, argoClientSet)

	testcases := []testCase{
		{Name: "listen for job events", Method: http.MethodGet, URL: "/api/v2/jobs/dummy/events/", Body: nil, ExpectedResponseStatusCode: http.StatusOK, Headers: nil, ExpectedResponseHeaders: nil}}

	for _, test := range testcases {
		t.Run(test.Name, func(t *testing.T) {
			var payload []byte
			var err error

			defer func() {
				if t.Failed() {
					log.Errorf("test %s response payload: %s", test.Name, string(payload))
				}
			}()

			req := httptest.NewRequest(test.Method, test.URL, bytes.NewReader(test.Body))
			req.Header["Content-Type"] = []string{"application/json"}
			req = gmux.SetURLVars(req, map[string]string{"id": "dummy"})
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			res := w.Result()
			defer res.Body.Close()

			require.Equal(t, test.ExpectedResponseStatusCode, w.Code)
			require.Len(t, res.Header["Content-Type"], 1)
			require.Equal(t, "text/event-stream", res.Header["Content-Type"][0])
			require.Equal(t, "no-cache, no-store", res.Header["Cache-Control"][0])
			require.Equal(t, "keep-alive", res.Header["Connection"][0])

			payload, err = ioutil.ReadAll(res.Body)
			require.NoError(t, err)

			messages := bytes.Split(payload, []byte("\n\n"))
			require.Len(t, messages, 4)

			for i := 0; i < 3; i++ {
				require.True(t, json.Valid(bytes.Split(payload, []byte("data:"))[1]))
			}
		})
	}
}

func Test_JobDeleteHandler(t *testing.T) {
	cRefVer := models.CRefVersion{Uid: models.NewReference("3b971ada-15d6-4422-abbb-8ecf4a3f90bb")}
	cRefVerNoAccess := models.CRefVersion{Uid: models.NewReference("1a971ada-15d6-4422-abbb-8ecf4a3f9000")}
	client := NewMockClient()
	client.On("DeleteDocument", mock.Anything, storage.JobKind, cRefVer).Return(cRefVer, nil)
	client.On("DeleteDocument", mock.Anything, storage.JobKind, cRefVerNoAccess).Return(models.CRefVersion{}, errors.Errorf("could not access job from storage or document not found"))

	argoClient := fake.NewSimpleClientset(&v1alpha1.Workflow{ObjectMeta: metav1.ObjectMeta{Namespace: "testing"}})
	argoClient.PrependReactor("get", "workflows", GetReactor)
	argoClient.PrependReactor("delete", "workflows", DeleteReactor)
	mux := gmux.NewRouter()
	RegisterJobRoutes(mux.PathPrefix("/api/v2"), client, argoClient)

	testcases := []testCase{
		{Name: "terminate job", Method: http.MethodDelete, URL: fmt.Sprintf("/api/v2/jobs/%s", cRefVer.Uid.String()), Body: []byte(cRefVer.Uid.String()), ExpectedResponseStatusCode: http.StatusOK, Headers: nil, ExpectedResponseHeaders: nil},
		{Name: "terminate job, no access", Method: http.MethodDelete, URL: fmt.Sprintf("/api/v2/jobs/%s", cRefVerNoAccess.Uid.String()), Body: nil, ExpectedResponseStatusCode: http.StatusInternalServerError, Headers: nil, ExpectedResponseHeaders: nil},
	}
	for _, test := range testcases {
		t.Run(test.Name, func(t *testing.T) {
			var payload []byte
			var err error

			defer func() {
				if t.Failed() {
					log.Errorf("test %s response payload: %s", test.Name, string(payload))
				}
			}()

			req := httptest.NewRequest(test.Method, test.URL, bytes.NewReader(test.Body))

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			res := w.Result()

			defer res.Body.Close()
			payload, err = ioutil.ReadAll(res.Body)
			require.NoError(t, err)

			if test.ExpectedResponseStatusCode == http.StatusOK {
				var actual models.ComponentReference
				err = json.Unmarshal(payload, &actual)
				require.NoError(t, err)
				expected := models.ComponentReference(uuid.MustParse(string(test.Body)))
				require.Equal(t, expected, actual)
			}

			require.Equal(t, test.ExpectedResponseStatusCode, w.Code, w.Body)
		})
	}
}

// Workspace client can be mocked using context injection
func Test_WorkspacesHTTPHandler(t *testing.T) {
	mux := gmux.NewRouter()
	RegisterWorkspaceRoutes(mux.PathPrefix("/api/v2"))

	type testCase struct {
		Name        string
		GivenAccess []workspace.Workspace
	}

	testcases := []testCase{
		{Name: "list empty workspaces",
			GivenAccess: []workspace.Workspace{}},
		{Name: "list workspaces with access",
			GivenAccess: []workspace.Workspace{{Name: "test", HasAccess: true, MissingRoles: nil}}},
	}

	for _, test := range testcases {
		t.Run(test.Name, func(t *testing.T) {
			log.Info("Test: ", test.Name)
			var payload []byte
			var err error

			defer func() {
				if t.Failed() {
					log.Errorf("test %s response payload: %s", test.Name, string(payload))
				}
			}()

			request := httptest.NewRequest(http.MethodGet, "/api/v2/workspaces/", nil)
			// inject test context here
			ctx := request.Context()
			ctx = context.WithValue(ctx, workspace.WorkspaceKey, test.GivenAccess)
			request = request.WithContext(ctx)

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, request)
			res := w.Result()

			defer res.Body.Close()
			payload, err = ioutil.ReadAll(res.Body)
			require.NoError(t, err)

			type WorkspaceAccessList struct {
				Items []workspace.Workspace `json:"items"`
			}
			ac := WorkspaceAccessList{}
			err = json.Unmarshal(payload, &ac)
			require.Nil(t, err)
			require.Equal(t, test.GivenAccess, ac.Items, string(payload))

		})
	}

}
