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
	"github.com/equinor/flowify-workflows-server/v2/storage"
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

// Volume endpoints testing
func Test_ListVolumesHTTPHandler(t *testing.T) {
	mux := gmux.NewRouter()
	client := NewMockVolumeClient()
	RegisterVolumeRoutes(mux.PathPrefix("/api/v2"), client)

	type testCase struct {
		Name     string
		MockList models.FlowifyVolumeList
		Path     string
		Verb     string
		Code     int
	}
	vol1 := FlowifyVolume{Uid: models.ComponentReference(uuid.MustParse("3a30874d-6e84-4340-858c-f53b2703da39")), Workspace: "sandbox-project-a", Volume: corev1.Volume{Name: "moc-volume-1"}}

	testcases := []testCase{
		{Name: "list volumes, no ws access",
			Path:     "ws/",
			Verb:     http.MethodGet,
			MockList: models.FlowifyVolumeList{Items: []FlowifyVolume{}},
			Code:     http.StatusOK},
		{Name: "list volumes, sandbox access",
			Path: "sandbox-project-a/",
			Verb: http.MethodGet,
			MockList: models.FlowifyVolumeList{Items: []models.FlowifyVolume{vol1},
				PageInfo: models.PageInfo{TotalNumber: 1, Skip: 0, Limit: 20}},
			Code: http.StatusOK},
		{Name: "list volumes, from other sandbox",
			Path:     "sandbox-project-z/",
			Verb:     http.MethodGet,
			MockList: models.FlowifyVolumeList{Items: []FlowifyVolume{}},
			Code:     http.StatusOK},
	}

	for _, test := range testcases {
		t.Run(test.Name, func(t *testing.T) {
			log.Info("Test: ", test.Name)

			// set the temporary response on the mock-service
			fake := client.On("ListVolumes", mock.Anything).Return(test.MockList, nil)
			// un-set the temporary response on the mock-service
			defer fake.Unset()

			url := "/api/v2/volumes/" + test.Path
			request := httptest.NewRequest(test.Verb, url, nil)

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, request)
			res := w.Result()

			defer res.Body.Close()
			payload, err := ioutil.ReadAll(res.Body)
			require.NoError(t, err)
			require.Equal(t, test.Code, res.StatusCode, url)

			require.Equal(t, first(json.Marshal(test.MockList)), payload, test)
		})
	}
}

func Test_GetVolumeHTTPHandler(t *testing.T) {
	mux := gmux.NewRouter()
	client := NewMockVolumeClient()
	RegisterVolumeRoutes(mux.PathPrefix("/api/v2"), client)

	type testCase struct {
		Name         string
		ResVolume    FlowifyVolume
		ResError     error
		ExpectedBody []byte
		Path         string
		Code         int
	}

	vol1 := FlowifyVolume{Uid: models.ComponentReference(uuid.MustParse("3a30874d-6e84-4340-858c-f53b2703da39")), Workspace: "sandbox-project-a", Volume: corev1.Volume{Name: "moc-volume-1"}}

	testcases := []testCase{
		{Name: "get volume, simulate wrong sandbox access",
			Path:      "sandbox-project-z/" + vol1.Uid.String(),
			ResVolume: FlowifyVolume{},
			ResError:  storage.ErrNotFound,
			Code:      http.StatusNotFound},
		{Name: "get volume, with access",
			Path:      path.Join(vol1.Workspace, vol1.Uid.String()),
			ResVolume: vol1,
			ResError:  nil,
			Code:      http.StatusOK},
	}

	for _, test := range testcases {
		t.Run(test.Name, func(t *testing.T) {
			log.Info("Test: ", test.Name)

			// set the temporary response on the mock-service
			fake := client.On("GetVolume", mock.Anything, mock.Anything).Return(test.ResVolume, test.ResError)
			// un-set the temporary response on the mock-service
			defer fake.Unset()

			url := "/api/v2/volumes/" + test.Path
			request := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, request)
			res := w.Result()

			defer res.Body.Close()
			payload, err := ioutil.ReadAll(res.Body)
			require.NoError(t, err)
			require.Equal(t, test.Code, res.StatusCode, url)

			if test.ResError == nil {
				expectedBody := first(json.Marshal((test.ResVolume)))
				require.Equal(t, expectedBody, payload, test)
			}
		})
	}
}

func Test_PostVolumeHTTPHandler(t *testing.T) {
	mux := gmux.NewRouter()
	client := NewMockVolumeClient()
	RegisterVolumeRoutes(mux.PathPrefix("/api/v2"), client)

	type testCase struct {
		Name         string
		InputVolume  FlowifyVolume
		ResError     error
		ExpectedBody []byte
		Path         string
		Verb         string
		Code         int
	}

	vol0 := FlowifyVolume{Uid: models.ComponentReference{}, Workspace: "sandbox-project-a", Volume: corev1.Volume{Name: "moc-volume-1"}}
	vol1 := FlowifyVolume{Uid: models.ComponentReference(uuid.MustParse("3a30874d-6e84-4340-858c-f53b2703da39")), Workspace: "sandbox-project-a", Volume: corev1.Volume{Name: "moc-volume-1"}}

	testcases := []testCase{
		{Name: "put volume, no access",
			Path:        path.Join(vol1.Workspace, vol1.Uid.String()),
			Verb:        http.MethodPut,
			InputVolume: vol1,
			ResError:    storage.ErrNoAccess,
			Code:        http.StatusBadRequest},
		{Name: "put volume, ws mismatch",
			Path:        path.Join(vol1.Workspace+"X", vol1.Uid.String()),
			Verb:        http.MethodPut,
			InputVolume: vol1,
			ResError:    storage.ErrNoAccess,
			Code:        http.StatusBadRequest},
		{Name: "put volume, ok",
			Path:        path.Join(vol1.Workspace, vol1.Uid.String()),
			Verb:        http.MethodPut,
			InputVolume: vol1,
			ResError:    nil,
			Code:        http.StatusNoContent},
		{Name: "post volume, no access",
			Path:        path.Join(vol0.Workspace) + "/",
			Verb:        http.MethodPost,
			InputVolume: vol0,
			ResError:    fmt.Errorf("no access"),
			Code:        http.StatusBadRequest},
		{Name: "post empty volume, ok access",
			Path:        "ws/",
			Verb:        http.MethodPost,
			InputVolume: FlowifyVolume{},
			ResError:    nil,
			Code:        http.StatusBadRequest},
		{Name: "post volume, ok access",
			Path:        path.Join(vol0.Workspace) + "/",
			Verb:        http.MethodPost,
			InputVolume: vol0,
			ResError:    nil,
			Code:        http.StatusCreated},
		{Name: "post volume, error has uid set",
			Path:        path.Join(vol1.Workspace) + "/",
			Verb:        http.MethodPost,
			InputVolume: vol1,
			ResError:    nil,
			Code:        http.StatusBadRequest},
	}

	for _, test := range testcases {
		t.Run(test.Name, func(t *testing.T) {
			log.Info("Test: ", test.Name)

			// set the temporary response on the mock-service
			fake := client.On("PutVolume", mock.Anything, mock.Anything).Return(test.ResError)
			// un-set the temporary response on the mock-service at end of scope
			defer fake.Unset()

			url := "/api/v2/volumes/" + test.Path
			request := httptest.NewRequest(test.Verb, url, bytes.NewReader(first(json.Marshal(test.InputVolume))))
			request.Header.Add("Content-Type", "application/json")

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, request)
			res := w.Result()

			defer res.Body.Close()
			payload, err := ioutil.ReadAll(res.Body)
			require.NoError(t, err)
			require.Equal(t, test.Code, res.StatusCode, url, string(payload))

		})
	}
}

func Test_DeleteVolumeHTTPHandler(t *testing.T) {
	mux := gmux.NewRouter()
	client := NewMockVolumeClient()
	RegisterVolumeRoutes(mux.PathPrefix("/api/v2"), client)

	type testCase struct {
		Name         string
		ResError     error
		ExpectedBody []byte
		Path         string
		Code         int
	}

	vol1 := FlowifyVolume{Uid: models.ComponentReference(uuid.MustParse("3a30874d-6e84-4340-858c-f53b2703da39")), Workspace: "sandbox-project-a", Volume: corev1.Volume{Name: "moc-volume-1"}}

	testcases := []testCase{
		{Name: "delete volume, ok",
			Path:     path.Join(vol1.Workspace, vol1.Uid.String()),
			ResError: nil,
			Code:     http.StatusOK},
		{Name: "delete volume, bad uid",
			Path:     path.Join(vol1.Workspace, vol1.Uid.String()+"_"),
			ResError: fmt.Errorf("bad uid"),
			Code:     http.StatusBadRequest},
	}

	for _, test := range testcases {
		t.Run(test.Name, func(t *testing.T) {
			log.Info("Test: ", test.Name)

			// set the temporary response on the mock-service
			fake := client.On("DeleteVolume", mock.Anything, mock.Anything).Return(test.ResError)
			// un-set the temporary response on the mock-service
			defer fake.Unset()

			url := "/api/v2/volumes/" + test.Path
			request := httptest.NewRequest(http.MethodDelete, url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, request)
			res := w.Result()

			defer res.Body.Close()
			_, err := ioutil.ReadAll(res.Body)
			require.NoError(t, err)
			require.Equal(t, test.Code, res.StatusCode, url)

		})
	}
}

func first[T1 any, T2 any](arg1 T1, arg2 T2) T1 { return arg1 }
