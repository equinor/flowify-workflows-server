package rest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

type MockSecretAuthorization struct {
	Permissions map[auth.Action]bool
}

func (m MockSecretAuthorization) Authorize(subject auth.Subject, action auth.Action, user user.User, object any) (bool, error) {
	if subject != auth.Secrets {
		return false, errors.Errorf("Access denied", "Cannot authorize subject: %s", string(subject))
	}
	if access, ok := m.Permissions[action]; ok {
		return access, nil
	}
	return false, errors.Errorf("Access denied", "Could not authorize action: %s", string(action))
}

func Fail(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusUnauthorized)
	fmt.Fprintf(w, "no access")
}
func Pass(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK")
}

func first[T any](t T, err error) T {
	if err != nil {
		panic(err)
	}
	return t
}

func Test_PathAuthorization(t *testing.T) {
	mux := gmux.NewRouter()
	sclient := NewMockSecrets()
	sclient.On("ListAvailableKeys", mock.Anything, mock.Anything).Return([]string{"s1", "s3"}, nil)

	authz := MockSecretAuthorization{Permissions: map[auth.Action]bool{auth.List: true}}

	mux.HandleFunc("/", rest.PathAuthorization(auth.Secrets, auth.List, "workspace", authz, Pass)).Methods(http.MethodGet)
	mux.HandleFunc("/{workspace}/", rest.PathAuthorization(auth.Secrets, auth.List, "workspace", authz, Pass)).Methods(http.MethodGet)

	URL, err := url.Parse("/")
	require.NoError(t, err)

	type test struct {
		Name           string
		WorkspacePath  *url.URL
		ExpectedResult int
	}
	/*
	   These test make sure the handling of the url path variable works as expected
	*/
	for _, test := range []test{
		{
			Name:           "no workspace var in request",
			WorkspacePath:  first(url.Parse("")),
			ExpectedResult: http.StatusUnauthorized,
		},
		{
			Name: "with workspace var in request",
			// its a path variable, so any string matching the position will enable the variable to be read by the handler
			WorkspacePath:  first(url.Parse("/workspace-name/")),
			ExpectedResult: http.StatusOK,
		},
	} {
		t.Run(test.Name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, URL.ResolveReference(test.WorkspacePath).String(), nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)
			res := w.Result()
			require.Equal(t, test.ExpectedResult, res.StatusCode, BodyStringer{res.Body})
		})
	}

}

func Test_ListSecretsHTTPHandler(t *testing.T) {
	mux := gmux.NewRouter()

	sclient := NewMockSecrets()

	authz := MockSecretAuthorization{Permissions: map[auth.Action]bool{}}

	rest.RegisterSecretRoutes(mux.PathPrefix(apiserver.ApiV1Path), sclient, &authz)

	type ClientResponse struct {
		Keys  []string
		Error error
	}
	for _, test := range []struct {
		Name                       string
		Workspace                  string
		Access                     bool
		ExpectedResponseStatusCode int
		ClientResponse             ClientResponse
	}{
		{
			Name:                       "test list pass",
			Workspace:                  "mock",
			Access:                     true,
			ExpectedResponseStatusCode: http.StatusOK,
			ClientResponse:             ClientResponse{Keys: []string{"s1", "s3"}, Error: nil},
		},
		{
			Name:                       "test list authz fail",
			Workspace:                  "mock",
			Access:                     false,
			ClientResponse:             ClientResponse{Keys: []string{"s1", "s3"}, Error: nil},
			ExpectedResponseStatusCode: http.StatusUnauthorized,
		},
		{
			Name:                       "test list client fail",
			Workspace:                  "mock",
			Access:                     true,
			ClientResponse:             ClientResponse{Keys: []string{}, Error: errors.Errorf("could not list keys %s", "mock")},
			ExpectedResponseStatusCode: http.StatusInternalServerError,
		},
	} {
		t.Run(test.Name, func(t *testing.T) {
			URL := path.Join(apiserver.ApiV1Path, "secrets", test.Workspace) + "/" // need explicit trailing
			req := httptest.NewRequest(http.MethodGet, URL, nil)                   /*bytes.NewReader(test.Body)*/
			req.Header["Content-Type"] = []string{"application/json"}
			req = gmux.SetURLVars(req, map[string]string{"workspace": test.Workspace})
			w := httptest.NewRecorder()

			// override authz for test
			authz.Permissions[auth.List] = test.Access

			call := sclient.On("ListAvailableKeys", mock.Anything, mock.Anything).Return(test.ClientResponse.Keys, test.ClientResponse.Error)
			defer call.Unset()

			mux.ServeHTTP(w, req)
			res := w.Result()

			require.Equal(t, test.ExpectedResponseStatusCode, w.Code, URL, BodyStringer{res.Body})

			if w.Code == http.StatusOK {
				type SecretListing struct {
					Items []string
				}
				sl, err := ReadType[SecretListing](res)
				require.NoError(t, err)
				require.Len(t, sl.Items, len(test.ClientResponse.Keys), err)
			}
		})
	}
}

func Test_AddSecretsHTTPHandler(t *testing.T) {
	mux := gmux.NewRouter()

	sclient := NewMockSecrets()
	sclient.On("ListAvailableKeys", mock.Anything, mock.Anything).Return([]string{"s1", "s3"}, nil)

	authz := MockSecretAuthorization{Permissions: map[auth.Action]bool{}}
	rest.RegisterSecretRoutes(mux.PathPrefix(apiserver.ApiV1Path), sclient, &authz)

	for _, test := range []struct {
		Name                       string
		Workspace                  string
		OnAddResponse              error
		Body                       []byte
		Key                        string
		Secret                     rest.SecretField
		Access                     bool
		ExpectedResponseStatusCode int
	}{
		{
			Name:                       "add secret passing",
			Workspace:                  "mock",
			Key:                        "s2",
			Secret:                     rest.SecretField{Key: "s2", Value: "***"},
			Access:                     true,
			ExpectedResponseStatusCode: http.StatusCreated,
		},
		{
			Name:                       "add secret passing no access",
			Workspace:                  "mock",
			Key:                        "s2",
			Secret:                     rest.SecretField{Key: "s2", Value: "***"},
			Access:                     false,
			ExpectedResponseStatusCode: http.StatusUnauthorized,
		},
		{
			Name:                       "add secret empty content, may surprise where it fails",
			Workspace:                  "mock",
			Key:                        "s2",
			Secret:                     rest.SecretField{Key: "s2", Value: "***"},
			Body:                       []byte("{}"),
			Access:                     true,
			ExpectedResponseStatusCode: http.StatusBadRequest,
		},
		{
			Name:                       "add secret bad content",
			Workspace:                  "mock",
			Key:                        "s2",
			Secret:                     rest.SecretField{Key: "s2", Value: "***"},
			Body:                       []byte("{"),
			Access:                     true,
			ExpectedResponseStatusCode: http.StatusBadRequest,
		},
		{
			Name:                       "add secret mismatching key",
			Workspace:                  "mock",
			Key:                        "s1",
			Secret:                     rest.SecretField{Key: "s2", Value: "***"},
			Access:                     true,
			ExpectedResponseStatusCode: http.StatusBadRequest,
		},
		{
			Name:                       "add secret existing key",
			Workspace:                  "mock",
			Key:                        "s1",
			Secret:                     rest.SecretField{Key: "s1", Value: "***"},
			Access:                     true,
			ExpectedResponseStatusCode: http.StatusNoContent,
		},
		{
			Name:                       "failing client",
			Workspace:                  "mock",
			Key:                        "s2",
			Secret:                     rest.SecretField{Key: "s2", Value: "***"},
			Access:                     true,
			OnAddResponse:              errors.Errorf("could not add secret %s", "key"),
			ExpectedResponseStatusCode: http.StatusInternalServerError,
		},
	} {
		t.Run(test.Name, func(t *testing.T) {
			URL := path.Join(apiserver.ApiV1Path, "secrets", test.Workspace, test.Key) // no trailing on singular objs
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
			defer c.Unset()

			req := httptest.NewRequest(http.MethodPut, URL, bytes.NewReader(Body))
			req.Header["Content-Type"] = []string{"application/json"}
			req = gmux.SetURLVars(req, map[string]string{"workspace": test.Workspace})
			w := httptest.NewRecorder()

			// override authz for test
			authz.Permissions[auth.Write] = test.Access

			mux.ServeHTTP(w, req)
			res := w.Result()

			require.Equal(t, test.ExpectedResponseStatusCode, w.Code, URL, BodyStringer{res.Body})
		})
	}
}

func Test_DeleteSecretsHTTPHandler(t *testing.T) {
	mux := gmux.NewRouter()

	sclient := NewMockSecrets()
	authz := MockSecretAuthorization{Permissions: map[auth.Action]bool{}}
	rest.RegisterSecretRoutes(mux.PathPrefix(apiserver.ApiV1Path), sclient, &authz)

	for _, test := range []struct {
		Name                       string
		Workspace                  string
		Body                       []byte
		Key                        string
		Access                     bool
		ExpectedResponseStatusCode int
		SecretClientError          error
	}{
		{
			Name:                       "delete secret passing",
			Workspace:                  "mock",
			Key:                        "s2",
			Access:                     true,
			ExpectedResponseStatusCode: http.StatusNoContent,
		},
		{
			Name:                       "delete secret no auth",
			Workspace:                  "mock",
			Key:                        "s2",
			Access:                     false,
			ExpectedResponseStatusCode: http.StatusUnauthorized,
		},

		{
			Name:                       "delete secret client not found",
			Workspace:                  "mock",
			Key:                        "s2",
			Access:                     true,
			ExpectedResponseStatusCode: http.StatusNotFound,
			SecretClientError:          k8serrors.NewNotFound(schema.GroupResource{}, "mock"),
		},
		{
			Name:                       "delete secret client fail",
			Workspace:                  "mock",
			Key:                        "s2",
			Access:                     true,
			ExpectedResponseStatusCode: http.StatusInternalServerError,
			SecretClientError:          errors.Errorf("could not delete secret %s", "mock"),
		},
	} {
		t.Run(test.Name, func(t *testing.T) {
			URL := path.Join(apiserver.ApiV1Path, "secrets", test.Workspace, test.Key) // no trailing on singular objs

			// add a custom response to secret client
			c := sclient.On("DeleteSecretKey", mock.Anything, mock.Anything, mock.Anything).Return(test.SecretClientError)
			defer c.Unset() // unset custom response at scope end

			req := httptest.NewRequest(http.MethodDelete, URL, nil)
			req.Header["Content-Type"] = []string{"application/json"}
			req = gmux.SetURLVars(req, map[string]string{"workspace": test.Workspace})
			w := httptest.NewRecorder()

			// override authz for test
			authz.Permissions[auth.Delete] = test.Access

			mux.ServeHTTP(w, req)
			res := w.Result()
			defer res.Body.Close()

			require.Equal(t, test.ExpectedResponseStatusCode, w.Code, URL, BodyStringer{res.Body})

		})
	}
}

func ReadType[T any](r *http.Response) (T, error) {
	bytes := ResponseBodyBytes(r)
	var item T

	err := json.Unmarshal(bytes, &item)
	return item, err
}
