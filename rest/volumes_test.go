package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"path"
	"testing"

	"github.com/equinor/flowify-workflows-server/models"
	"github.com/equinor/flowify-workflows-server/storage"
	"github.com/google/uuid"
	gmux "github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

type FlowifyVolume = models.FlowifyVolume

func NewMockVolumeClient() *volumeClient {
	return &volumeClient{}
}

// implement a mock client/db for testing
type volumeClient struct {
	mock.Mock
}

func (c *volumeClient) ListVolumes(ctx context.Context, pagination storage.Pagination, f []string, s []string) (models.FlowifyVolumeList, error) {
	args := c.Called(ctx)
	return args.Get(0).(models.FlowifyVolumeList), args.Error(1)
}
func (c *volumeClient) PutVolume(ctx context.Context, vol FlowifyVolume) error {
	args := c.Called(ctx, vol)
	return args.Error(0)
}
func (c *volumeClient) GetVolume(ctx context.Context, id models.ComponentReference) (FlowifyVolume, error) {
	args := c.Called(ctx, id)
	return args.Get(0).(FlowifyVolume), args.Error(1)
}
func (c *volumeClient) DeleteVolume(ctx context.Context, id models.ComponentReference) error {
	args := c.Called(ctx, id)
	return args.Error(0)
}

type MockVolumeAuthorization struct {
	Permissions map[auth.Action]bool
}

func (m MockVolumeAuthorization) Authorize(subject auth.Subject, action auth.Action, user user.User, object any) (bool, error) {
	if subject != auth.Volumes {
		return false, errors.Errorf("Cannot authorize subject: %s", string(subject))
	}
	if access, ok := m.Permissions[action]; ok {
		return access, nil
	}
	return false, errors.Errorf("Could not authorize action: %s", string(action))
}

// Volume endpoints testing
func Test_ListVolumesHTTPHandler(t *testing.T) {
	mux := gmux.NewRouter()
	client := NewMockVolumeClient()
	authz := MockVolumeAuthorization{Permissions: map[auth.Action]bool{auth.List: true}}
	rest.RegisterVolumeRoutes(mux.PathPrefix("/api/v1"), client, &authz)

	type testCase struct {
		Name       string
		MockList   models.FlowifyVolumeList
		ListAccess bool
		Path       string
		Verb       string
		Code       int
	}
	vol1 := FlowifyVolume{Uid: models.ComponentReference(uuid.MustParse("3a30874d-6e84-4340-858c-f53b2703da39")), Workspace: "sandbox-project-a", Volume: corev1.Volume{Name: "moc-volume-1"}}

	testcases := []testCase{
		{Name: "list volumes, no ws access",
			Path:       "ws/",
			ListAccess: false,
			Verb:       http.MethodGet,
			MockList:   models.FlowifyVolumeList{Items: []FlowifyVolume{}},
			Code:       http.StatusUnauthorized,
		},
		{Name: "list volumes, sandbox access",
			Path:       "sandbox-project-a/",
			ListAccess: true,
			Verb:       http.MethodGet,
			MockList: models.FlowifyVolumeList{Items: []models.FlowifyVolume{vol1},
				PageInfo: models.PageInfo{TotalNumber: 1, Skip: 0, Limit: 20}},
			Code: http.StatusOK},
		{Name: "list volumes, from other sandbox",
			Path:       "sandbox-project-z/",
			ListAccess: true,
			Verb:       http.MethodGet,
			MockList:   models.FlowifyVolumeList{Items: []FlowifyVolume{}},
			Code:       http.StatusOK},
	}

	for _, test := range testcases {
		t.Run(test.Name, func(t *testing.T) {
			log.Info("Test: ", test.Name)

			authz.Permissions[auth.List] = test.ListAccess

			// set the temporary response on the mock-service
			fake := client.On("ListVolumes", mock.Anything).Return(test.MockList, nil)
			// un-set the temporary response on the mock-service
			defer fake.Unset()

			url := "/api/v1/volumes/" + test.Path
			request := httptest.NewRequest(test.Verb, url, nil)

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, request)
			res := w.Result()

			defer res.Body.Close()

			require.Equal(t, test.Code, res.StatusCode, BodyStringer{res.Body})
			if res.StatusCode == http.StatusOK {
				list, err := ReadType[models.FlowifyVolumeList](res)
				require.NoError(t, err)
				require.Equal(t, test.MockList, list, test)
			}
		})
	}
}

func Test_GetVolumeHTTPHandler(t *testing.T) {
	mux := gmux.NewRouter()
	client := NewMockVolumeClient()
	authz := MockVolumeAuthorization{Permissions: map[auth.Action]bool{}}
	rest.RegisterVolumeRoutes(mux.PathPrefix("/api/v1"), client, &authz)

	type testCase struct {
		Name      string
		ResVolume FlowifyVolume
		ResError  error
		Path      string
		Access    bool
		Code      int
	}

	vol1 := FlowifyVolume{Uid: models.ComponentReference(uuid.MustParse("3a30874d-6e84-4340-858c-f53b2703da39")), Workspace: "sandbox-project-a", Volume: corev1.Volume{Name: "moc-volume-1"}}

	testcases := []testCase{
		{Name: "get volume, no access",
			Path:      path.Join(vol1.Workspace, vol1.Uid.String()),
			Access:    false,
			ResVolume: FlowifyVolume{},
			ResError:  storage.ErrNotFound,
			Code:      http.StatusUnauthorized},
		{Name: "get volume, simulate wrong sandbox access but with access",
			Path:      "sandbox-project-z/" + vol1.Uid.String(),
			Access:    true,
			ResVolume: FlowifyVolume{},
			ResError:  storage.ErrNotFound,
			Code:      http.StatusNotFound},
		{Name: "get volume, with access",
			Path:      path.Join(vol1.Workspace, vol1.Uid.String()),
			ResVolume: vol1,
			Access:    true,
			ResError:  nil,
			Code:      http.StatusOK},
	}

	for _, test := range testcases {
		t.Run(test.Name, func(t *testing.T) {
			log.Info("Test: ", test.Name)

			authz.Permissions[auth.Read] = test.Access
			// set the temporary response on the mock-service
			fake := client.On("GetVolume", mock.Anything, mock.Anything).Return(test.ResVolume, test.ResError)
			// un-set the temporary response on the mock-service
			defer fake.Unset()

			url := "/api/v1/volumes/" + test.Path
			request := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, request)
			res := w.Result()

			defer res.Body.Close()
			require.Equal(t, test.Code, res.StatusCode, BodyStringer{res.Body})

			if test.ResError == nil {
				vol, err := ReadType[FlowifyVolume](res)
				require.NoError(t, err)
				require.Equal(t, test.ResVolume, vol)
			}
		})
	}
}

func Test_PostVolumeHTTPHandler(t *testing.T) {
	mux := gmux.NewRouter()
	client := NewMockVolumeClient()
	authz := MockVolumeAuthorization{Permissions: map[auth.Action]bool{}}
	rest.RegisterVolumeRoutes(mux.PathPrefix("/api/v1"), client, &authz)

	type testCase struct {
		Name        string
		InputVolume FlowifyVolume
		ResError    error
		Path        string
		Verb        string
		WriteAccess bool
		Code        int
	}

	vol0 := FlowifyVolume{Uid: models.ComponentReference{}, Workspace: "sandbox-project-a", Volume: corev1.Volume{Name: "moc-volume-1"}}
	vol1 := FlowifyVolume{Uid: models.ComponentReference(uuid.MustParse("3a30874d-6e84-4340-858c-f53b2703da39")), Workspace: "sandbox-project-a", Volume: corev1.Volume{Name: "moc-volume-1"}}

	testcases := []testCase{
		{Name: "put volume, no access",
			Path:        path.Join(vol1.Workspace, vol1.Uid.String()),
			Verb:        http.MethodPut,
			InputVolume: vol1,
			WriteAccess: false,
			ResError:    nil,
			Code:        http.StatusUnauthorized},
		{Name: "put volume, ws mismatch",
			Path:        path.Join(vol1.Workspace+"X", vol1.Uid.String()),
			Verb:        http.MethodPut,
			InputVolume: vol1,
			WriteAccess: true,
			ResError:    storage.ErrNoAccess,
			Code:        http.StatusBadRequest},
		{Name: "put volume, ok",
			Path:        path.Join(vol1.Workspace, vol1.Uid.String()),
			Verb:        http.MethodPut,
			InputVolume: vol1,
			WriteAccess: true,
			ResError:    nil,
			Code:        http.StatusNoContent},
		{Name: "post volume, no access",
			Path:        path.Join(vol0.Workspace) + "/",
			Verb:        http.MethodPost,
			InputVolume: vol0,
			WriteAccess: false,
			ResError:    nil,
			Code:        http.StatusUnauthorized},
		{Name: "post empty volume, ok access == fail",
			Path:        "ws/",
			Verb:        http.MethodPost,
			InputVolume: FlowifyVolume{},
			WriteAccess: true,
			ResError:    nil,
			Code:        http.StatusBadRequest},
		{Name: "post volume, ok access",
			Path:        path.Join(vol0.Workspace) + "/",
			Verb:        http.MethodPost,
			InputVolume: vol0,
			WriteAccess: true,
			ResError:    nil,
			Code:        http.StatusCreated},
		{Name: "post volume, error has uid set",
			Path:        path.Join(vol1.Workspace) + "/",
			Verb:        http.MethodPost,
			InputVolume: vol1,
			ResError:    nil,
			WriteAccess: true,
			Code:        http.StatusBadRequest},
	}

	for _, test := range testcases {
		t.Run(test.Name, func(t *testing.T) {
			log.Info("Test: ", test.Name)

			authz.Permissions[auth.Write] = test.WriteAccess

			// set the temporary response on the mock-service
			fake := client.On("PutVolume", mock.Anything, mock.Anything).Return(test.ResError)
			// un-set the temporary response on the mock-service at end of scope
			defer fake.Unset()

			url := "/api/v1/volumes/" + test.Path
			request := httptest.NewRequest(test.Verb, url, bytes.NewReader(first(json.Marshal(test.InputVolume))))
			request.Header.Add("Content-Type", "application/json")

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, request)
			res := w.Result()

			defer res.Body.Close()

			require.Equal(t, test.Code, res.StatusCode, BodyStringer{res.Body})
			if res.StatusCode == http.StatusOK {
				vol, err := ReadType[FlowifyVolume](res)
				require.NoError(t, err)
				require.Equal(t, test.InputVolume, vol)
			}
		})
	}
}

func Test_DeleteVolumeHTTPHandler(t *testing.T) {
	mux := gmux.NewRouter()
	client := NewMockVolumeClient()
	authz := MockVolumeAuthorization{Permissions: map[auth.Action]bool{}}
	rest.RegisterVolumeRoutes(mux.PathPrefix("/api/v1"), client, &authz)

	type testCase struct {
		Name         string
		ResError     error
		ExpectedBody []byte
		Path         string
		Access       bool
		Code         int
	}

	vol1 := FlowifyVolume{Uid: models.ComponentReference(uuid.MustParse("3a30874d-6e84-4340-858c-f53b2703da39")), Workspace: "sandbox-project-a", Volume: corev1.Volume{Name: "moc-volume-1"}}

	testcases := []testCase{
		{Name: "delete volume, ok",
			Path:     path.Join(vol1.Workspace, vol1.Uid.String()),
			ResError: nil,
			Access:   true,
			Code:     http.StatusOK},
		{Name: "delete volume, no permission",
			Path:     path.Join(vol1.Workspace, vol1.Uid.String()),
			ResError: nil,
			Access:   false,
			Code:     http.StatusUnauthorized},
		{Name: "delete volume, bad uid",
			Path:     path.Join(vol1.Workspace, vol1.Uid.String()+"_"),
			ResError: fmt.Errorf("bad uid"),
			Access:   true,
			Code:     http.StatusBadRequest},
	}

	for _, test := range testcases {
		t.Run(test.Name, func(t *testing.T) {
			log.Info("Test: ", test.Name)

			authz.Permissions[auth.Delete] = test.Access

			// set the temporary response on the mock-service
			fake := client.On("DeleteVolume", mock.Anything, mock.Anything).Return(test.ResError)
			// un-set the temporary response on the mock-service
			defer fake.Unset()

			url := "/api/v1/volumes/" + test.Path
			request := httptest.NewRequest(http.MethodDelete, url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, request)
			res := w.Result()

			defer res.Body.Close()

			require.Equal(t, test.Code, res.StatusCode, BodyStringer{res.Body})
		})
	}
}

// specify an explicit type for inference
// eg ReadType[int](...)
func ReadType[T any](r *http.Response) (T, error) {
	bytes := ResponseBodyBytes(r)
	var item T

	err := json.Unmarshal(bytes, &item)
	return item, err
}
