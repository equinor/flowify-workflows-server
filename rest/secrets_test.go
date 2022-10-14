package rest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"testing"

	"github.com/argoproj/argo-workflows/v3/errors"
	"github.com/equinor/flowify-workflows-server/apiserver"
	"github.com/equinor/flowify-workflows-server/auth"
	"github.com/equinor/flowify-workflows-server/rest"
	"github.com/equinor/flowify-workflows-server/user"
	gmux "github.com/gorilla/mux"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

/*
	ListAvailableKeys(ctx context.Context, group string) ([]string, error)
	AddSecretKey(ctx context.Context, group, name, key string) error
	DeleteSecretKey(ctx context.Context, group, name string) error
*/

// implement a mock secret client for testing
type MockSecrets struct {
	mock.Mock
}

func NewMockSecrets() *MockSecrets {
	return &MockSecrets{}
}

func (m *MockSecrets) ListAvailableKeys(ctx context.Context, group string) ([]string, error) {
	args := m.Called(ctx, group)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockSecrets) AddSecretKey(ctx context.Context, group string, name string, value string) error {
	args := m.Called(ctx, group, name, value)
	return args.Error(0)
}

func (m *MockSecrets) DeleteSecretKey(ctx context.Context, group string, name string) error {
	args := m.Called(ctx, group)
	return args.Error(0)
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

type MockAuthorization struct {
	GivenPermissions auth.Permission
}

func (m MockAuthorization) Authorize(subject string, action string, req auth.Permission, user user.User, object any) (bool, error) {
	return auth.HasPermission(req, m.GivenPermissions), nil
}

func Test_ListSecretsHTTPHandler(t *testing.T) {
	mux := gmux.NewRouter()

	sclient := NewMockSecrets()

	authz := MockAuthorization{GivenPermissions: auth.Permission{}}

	rest.RegisterSecretRoutes(mux.PathPrefix(apiserver.ApiV1), sclient, &authz)

	type ClientResponse struct {
		Keys  []string
		Error error
	}
	for _, test := range []struct {
		Name                       string
		Workspace                  string
		Permissions                auth.Permission
		ExpectedResponseStatusCode int
		ClientResponse             ClientResponse
	}{
		{
			Name:                       "test list pass",
			Workspace:                  "mock",
			Permissions:                auth.Permission{Read: true},
			ExpectedResponseStatusCode: http.StatusOK,
			ClientResponse:             ClientResponse{Keys: []string{"s1", "s3"}, Error: nil},
		},
		{
			Name:                       "test list authz fail",
			Workspace:                  "mock",
			Permissions:                auth.Permission{},
			ClientResponse:             ClientResponse{Keys: []string{"s1", "s3"}, Error: nil},
			ExpectedResponseStatusCode: http.StatusUnauthorized,
		},
		{
			Name:                       "test list client fail",
			Workspace:                  "mock",
			Permissions:                auth.Permission{Read: true},
			ClientResponse:             ClientResponse{Keys: []string{}, Error: errors.Errorf("could not list keys %s", "mock")},
			ExpectedResponseStatusCode: http.StatusInternalServerError,
		},
	} {
		t.Run(test.Name, func(t *testing.T) {
			URL := path.Join(apiserver.ApiV1, "secrets", test.Workspace) + "/" // need explicit trailing
			req := httptest.NewRequest(http.MethodGet, URL, nil)               /*bytes.NewReader(test.Body)*/
			req.Header["Content-Type"] = []string{"application/json"}
			req = gmux.SetURLVars(req, map[string]string{"workspace": test.Workspace})
			w := httptest.NewRecorder()

			// override authz for test
			authz.GivenPermissions = test.Permissions

			call := sclient.On("ListAvailableKeys", mock.Anything, mock.Anything).Return(test.ClientResponse.Keys, test.ClientResponse.Error)

			mux.ServeHTTP(w, req)
			res := w.Result()

			require.Equal(t, test.ExpectedResponseStatusCode, w.Code, URL, BodyStringer{res.Body})

			call.Unset()
		})
	}
}

func Test_AddSecretsHTTPHandler(t *testing.T) {
	mux := gmux.NewRouter()

	sclient := NewMockSecrets()
	sclient.On("ListAvailableKeys", mock.Anything, mock.Anything).Return([]string{"s1", "s3"}, nil)

	authz := MockAuthorization{GivenPermissions: auth.Permission{}}
	rest.RegisterSecretRoutes(mux.PathPrefix(apiserver.ApiV1), sclient, &authz)

	for _, test := range []struct {
		Name                       string
		Workspace                  string
		OnAddResponse              error
		Body                       []byte
		Key                        string
		Secret                     rest.SecretField
		Permissions                auth.Permission
		ExpectedResponseStatusCode int
	}{
		{
			Name:                       "add secret passing",
			Workspace:                  "mock",
			Key:                        "s2",
			Secret:                     rest.SecretField{Key: "s2", Value: "***"},
			Permissions:                auth.Permission{Write: true},
			ExpectedResponseStatusCode: http.StatusCreated,
		},
		{
			Name:                       "add secret passing only read access",
			Workspace:                  "mock",
			Key:                        "s2",
			Secret:                     rest.SecretField{Key: "s2", Value: "***"},
			Permissions:                auth.Permission{Read: true},
			ExpectedResponseStatusCode: http.StatusUnauthorized,
		},
		{
			Name:                       "add secret empty content, may surprise where it fails",
			Workspace:                  "mock",
			Key:                        "s2",
			Secret:                     rest.SecretField{Key: "s2", Value: "***"},
			Body:                       []byte("{}"),
			Permissions:                auth.Permission{Write: true},
			ExpectedResponseStatusCode: http.StatusBadRequest,
		},
		{
			Name:                       "add secret bad content",
			Workspace:                  "mock",
			Key:                        "s2",
			Secret:                     rest.SecretField{Key: "s2", Value: "***"},
			Body:                       []byte("{"),
			Permissions:                auth.Permission{Write: true},
			ExpectedResponseStatusCode: http.StatusBadRequest,
		},
		{
			Name:                       "add secret mismatching key",
			Workspace:                  "mock",
			Key:                        "s1",
			Secret:                     rest.SecretField{Key: "s2", Value: "***"},
			Permissions:                auth.Permission{Write: true},
			ExpectedResponseStatusCode: http.StatusBadRequest,
		},
		{
			Name:                       "add secret existing key",
			Workspace:                  "mock",
			Key:                        "s1",
			Secret:                     rest.SecretField{Key: "s1", Value: "***"},
			Permissions:                auth.Permission{Write: true},
			ExpectedResponseStatusCode: http.StatusNoContent,
		},
		{
			Name:                       "failing client",
			Workspace:                  "mock",
			Key:                        "s2",
			Secret:                     rest.SecretField{Key: "s2", Value: "***"},
			Permissions:                auth.Permission{Write: true},
			OnAddResponse:              errors.Errorf("could not add secret %s", "key"),
			ExpectedResponseStatusCode: http.StatusInternalServerError,
		},
	} {
		t.Run(test.Name, func(t *testing.T) {
			URL := path.Join(apiserver.ApiV1, "secrets", test.Workspace, test.Key) // no trailing on singular objs
			var Body []byte
			if test.Body == nil {
				body, err := json.Marshal(test.Secret)
				require.NoError(t, err)
				Body = body
			} else {
				Body = test.Body
			}

			// add a custom response to secret client
			c := sclient.On("AddSecretKey", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(test.OnAddResponse)

			req := httptest.NewRequest(http.MethodPut, URL, bytes.NewReader(Body))
			req.Header["Content-Type"] = []string{"application/json"}
			req = gmux.SetURLVars(req, map[string]string{"workspace": test.Workspace})
			w := httptest.NewRecorder()

			// override authz for test
			authz.GivenPermissions = test.Permissions

			mux.ServeHTTP(w, req)
			res := w.Result()
			defer res.Body.Close()

			require.Equal(t, test.ExpectedResponseStatusCode, w.Code, URL, BodyStringer{res.Body})

			c.Unset() // unset custom response here
		})
	}
}

func Test_DeleteSecretsHTTPHandler(t *testing.T) {
	mux := gmux.NewRouter()

	sclient := NewMockSecrets()
	authz := MockAuthorization{GivenPermissions: auth.Permission{}}
	rest.RegisterSecretRoutes(mux.PathPrefix(apiserver.ApiV1), sclient, &authz)

	for _, test := range []struct {
		Name                       string
		Workspace                  string
		Body                       []byte
		Key                        string
		Permissions                auth.Permission
		ExpectedResponseStatusCode int
		SecretClientError          error
	}{
		{
			Name:                       "delete secret passing",
			Workspace:                  "mock",
			Key:                        "s2",
			Permissions:                auth.Permission{Delete: true},
			ExpectedResponseStatusCode: http.StatusNoContent,
		},
		{
			Name:                       "delete secret no auth",
			Workspace:                  "mock",
			Key:                        "s2",
			Permissions:                auth.Permission{Delete: false},
			ExpectedResponseStatusCode: http.StatusUnauthorized,
		},

		{
			Name:                       "delete secret client not found",
			Workspace:                  "mock",
			Key:                        "s2",
			Permissions:                auth.Permission{Delete: true},
			ExpectedResponseStatusCode: http.StatusNotFound,
			SecretClientError:          k8serrors.NewNotFound(schema.GroupResource{}, "mock"),
		},
		{
			Name:                       "delete secret client fail",
			Workspace:                  "mock",
			Key:                        "s2",
			Permissions:                auth.Permission{Delete: true},
			ExpectedResponseStatusCode: http.StatusInternalServerError,
			SecretClientError:          errors.Errorf("could not delete secret %s", "mock"),
		},
	} {
		t.Run(test.Name, func(t *testing.T) {
			URL := path.Join(apiserver.ApiV1, "secrets", test.Workspace, test.Key) // no trailing on singular objs

			// add a custom response to secret client
			c := sclient.On("DeleteSecretKey", mock.Anything, mock.Anything, mock.Anything).Return(test.SecretClientError)

			req := httptest.NewRequest(http.MethodDelete, URL, nil)
			req.Header["Content-Type"] = []string{"application/json"}
			req = gmux.SetURLVars(req, map[string]string{"workspace": test.Workspace})
			w := httptest.NewRecorder()

			// override authz for test
			authz.GivenPermissions = test.Permissions

			mux.ServeHTTP(w, req)
			res := w.Result()
			defer res.Body.Close()

			require.Equal(t, test.ExpectedResponseStatusCode, w.Code, URL, BodyStringer{res.Body})

			c.Unset() // unset custom response here
		})
	}
}
