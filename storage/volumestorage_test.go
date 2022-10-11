package storage_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/equinor/flowify-workflows-server/models"
	"github.com/equinor/flowify-workflows-server/pkg/workspace"
	"github.com/equinor/flowify-workflows-server/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestDeleteVolume(t *testing.T) {
	c, err := storage.NewMongoVolumeClientFromConfig(cfg, mclient)
	require.NoError(t, err)

	vol := models.FlowifyVolume{
		Workspace: "test",
		Uid:       models.NewComponentReference(),
		Volume:    corev1.Volume{Name: "test1"}}
	{
		// first add component to get
		// create context with access
		ws := []workspace.Workspace{{Name: "test", HasAccess: true}}
		authCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, ws)
		err := c.PutVolume(authCtx, vol)
		assert.Nil(t, err)
	}

	{
		ws := []workspace.Workspace{{Name: "test", HasAccess: true}}
		authCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, ws)
		vol_out, err := c.GetVolume(authCtx, vol.Uid)
		assert.Nil(t, err)
		assert.Equal(t, vol, vol_out)
	}

	badId := models.NewComponentReference()
	err = c.DeleteVolume(context.TODO(), badId)
	assert.ErrorContains(t, err, storage.ErrNotFound.Error())

	{
		// no access
		err = c.DeleteVolume(context.TODO(), vol.Uid)
		assert.ErrorContains(t, err, storage.ErrNoAccess.Error())

		// get access
		ws := []workspace.Workspace{{Name: vol.Workspace, HasAccess: true}}
		authCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, ws)
		err = c.DeleteVolume(authCtx, vol.Uid)
		assert.Nil(t, err)

		// make sure its gone (so supply auth context in case)
		_, err = c.GetVolume(authCtx, vol.Uid)
		assert.ErrorContains(t, err, storage.ErrNotFound.Error())
	}

	// try without context
	vol2, err := c.GetVolume(context.TODO(), vol.Uid)
	assert.ErrorContains(t, err, storage.ErrNotFound.Error())
	assert.Equal(t, models.FlowifyVolume{}, vol2, "should be empty")
}

func TestGetVolume(t *testing.T) {
	c, err := storage.NewMongoVolumeClientFromConfig(cfg, mclient)
	require.NoError(t, err)

	vol := models.FlowifyVolume{
		Workspace: "test",
		Uid:       models.NewComponentReference(),
		Volume:    corev1.Volume{Name: "test1"}}

	{
		// create context with access
		ws := []workspace.Workspace{{Name: "test", HasAccess: true}}
		authCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, ws)
		err := c.PutVolume(authCtx, vol)
		assert.Nil(t, err)
	}

	type testCase struct {
		Name            string
		CRef            models.ComponentReference
		WorkspaceAccess []workspace.Workspace
		ExpectedError   error
	}

	testCases := []testCase{
		{Name: "No authz context",
			CRef:            vol.Uid,
			WorkspaceAccess: []workspace.Workspace{{}},
			ExpectedError:   storage.ErrNoAccess},
		{Name: "Good authz context",
			CRef:            vol.Uid,
			WorkspaceAccess: []workspace.Workspace{{Name: "test", HasAccess: true}},
			ExpectedError:   nil},
		{Name: "Good authz context, bad ref",
			CRef:            models.NewComponentReference(),
			WorkspaceAccess: []workspace.Workspace{{Name: "test", HasAccess: true}},
			ExpectedError:   storage.ErrNotFound},
		{Name: "Authz context with name but no access",
			CRef:            vol.Uid,
			WorkspaceAccess: []workspace.Workspace{{Name: "test", HasAccess: false}},
			ExpectedError:   storage.ErrNoAccess},
		{Name: "Authz context with similar name/access",
			CRef:            vol.Uid,
			WorkspaceAccess: []workspace.Workspace{{Name: "tes", HasAccess: true}},
			ExpectedError:   storage.ErrNoAccess},
	}

	for _, test := range testCases {
		t.Run(test.Name, func(t *testing.T) {
			authzCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, test.WorkspaceAccess)
			vol_out, err := c.GetVolume(authzCtx, test.CRef)
			assert.Equal(t, err, test.ExpectedError)
			if err == nil {
				// make sure the roundtrip works when auth is good
				assert.Equal(t, vol, vol_out)
			}
		})
	}
}

func TestPutVolume(t *testing.T) {
	c, err := storage.NewMongoVolumeClientFromConfig(cfg, mclient)
	require.NoError(t, err)

	vol := models.FlowifyVolume{
		Workspace: "test",
		Uid:       models.NewComponentReference(),
		Volume:    corev1.Volume{Name: "test1-for-overwrite"}}
	{
		// create context with access
		ws := []workspace.Workspace{{Name: "test", HasAccess: true}}
		authCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, ws)
		err := c.PutVolume(authCtx, vol)
		assert.Nil(t, err)
	}

	type testCase struct {
		Name            string
		CRef            models.ComponentReference
		WorkspaceAccess []workspace.Workspace
		Workspace       string
		ExpectedFail    bool
		ExpectedError   error
	}

	testCases := []testCase{
		{Name: "No authz context",
			WorkspaceAccess: []workspace.Workspace{{}},
			CRef:            vol.Uid,
			Workspace:       "test",
			ExpectedFail:    true,
			ExpectedError:   storage.ErrNoAccess},
		{Name: "No authz context (explicit)",
			WorkspaceAccess: []workspace.Workspace{{Name: "test", HasAccess: false}},
			CRef:            vol.Uid,
			Workspace:       "test",
			ExpectedFail:    true,
			ExpectedError:   storage.ErrNoAccess},
		{Name: "Good authz context",
			WorkspaceAccess: []workspace.Workspace{{Name: "test", HasAccess: true}},
			CRef:            vol.Uid,
			Workspace:       "test",
			ExpectedFail:    false,
			ExpectedError:   nil},
		{Name: "Good authz context, try moving to unauth ws",
			WorkspaceAccess: []workspace.Workspace{{Name: "test", HasAccess: true}},
			CRef:            vol.Uid,
			Workspace:       "test2",
			ExpectedFail:    true,
			ExpectedError:   storage.ErrNoAccess},
		{Name: "Good authz context, try moving to new ws",
			WorkspaceAccess: []workspace.Workspace{{Name: "test", HasAccess: true}, {Name: "test2", HasAccess: true}},
			CRef:            vol.Uid,
			Workspace:       "test2",
			ExpectedFail:    false,
			ExpectedError:   nil},
		{Name: "Authz context with name but no access",
			WorkspaceAccess: []workspace.Workspace{{Name: "test", HasAccess: false}},
			CRef:            vol.Uid,
			Workspace:       "test",
			ExpectedFail:    true,
			ExpectedError:   storage.ErrNoAccess},
		{Name: "Authz context with similar name/access",
			WorkspaceAccess: []workspace.Workspace{{Name: "tes", HasAccess: true}},
			CRef:            vol.Uid,
			Workspace:       "test",
			ExpectedFail:    true,
			ExpectedError:   storage.ErrNoAccess},
	}

	for _, test := range testCases {
		t.Run(test.Name, func(t *testing.T) {
			tVol := vol
			tVol.Workspace = test.Workspace
			tVol.Uid = test.CRef
			authzCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, test.WorkspaceAccess)
			err := c.PutVolume(authzCtx, tVol)
			if test.ExpectedFail {
				assert.NotNil(t, err)
				assert.EqualError(t, test.ExpectedError, err.Error(), test.Name)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestListVolumes(t *testing.T) {
	c, err := storage.NewMongoVolumeClientFromConfig(cfg, mclient)
	require.NoError(t, err)

	{
		// drop db to make sure that at the end DB will contain one components
		mclient.Database(test_db_name).Drop(context.TODO())

		// first add components to list
		for i := 0; i < 5; i++ {
			authzCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, []workspace.Workspace{{Name: "ws-1", HasAccess: true}})
			err := c.PutVolume(authzCtx,
				models.FlowifyVolume{
					Uid:       models.NewComponentReference(),
					Workspace: "ws-1",
					Volume:    corev1.Volume{Name: fmt.Sprintf("test-%d", i)}})
			assert.Nil(t, err)
		}
		for i := 5; i < 10; i++ {
			authzCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, []workspace.Workspace{{Name: "ws-2", HasAccess: true}})
			err := c.PutVolume(authzCtx,
				models.FlowifyVolume{
					Uid:       models.NewComponentReference(),
					Workspace: "ws-2",
					Volume:    corev1.Volume{Name: fmt.Sprintf("test-%d", i)}})
			assert.Nil(t, err)
		}
		for i := 10; i < 15; i++ {
			authzCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, []workspace.Workspace{{Name: "ws-3", HasAccess: true}})
			err := c.PutVolume(authzCtx,
				models.FlowifyVolume{
					Uid:       models.NewComponentReference(),
					Workspace: "ws-3",
					Volume:    corev1.Volume{Name: fmt.Sprintf("test-%d", i)}})
			assert.Nil(t, err)
		}

	}

	type testCase struct {
		Name            string
		Filters         []string
		Sorting         []string
		WorkspaceAccess []workspace.Workspace
		ExpectedError   error
		ExpectedSize    int
	}

	testCases := []testCase{
		{Name: "No authz context",
			Filters:         nil,
			Sorting:         nil,
			WorkspaceAccess: nil,
			ExpectedError:   nil,
			ExpectedSize:    0,
		},
		{Name: "All auth",
			Filters: nil,
			Sorting: nil,
			WorkspaceAccess: []workspace.Workspace{
				{Name: "ws-1", HasAccess: true},
				{Name: "ws-2", HasAccess: true},
				{Name: "ws-3", HasAccess: true}},
			ExpectedError: nil,
			ExpectedSize:  15,
		},
		{Name: "All auth - filter ws2",
			Filters: []string{"workspace[==]=ws-2"},
			Sorting: nil,
			WorkspaceAccess: []workspace.Workspace{
				{Name: "ws-1", HasAccess: true},
				{Name: "ws-2", HasAccess: true},
				{Name: "ws-3", HasAccess: true}},
			ExpectedError: nil,
			ExpectedSize:  5,
		},
		{Name: "Limited auth",
			Filters: nil,
			Sorting: nil,
			WorkspaceAccess: []workspace.Workspace{
				{Name: "ws-2", HasAccess: true},
			},
			ExpectedError: nil,
			ExpectedSize:  5,
		},
	}

	for _, test := range testCases {
		t.Run(test.Name, func(t *testing.T) {
			authzCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, test.WorkspaceAccess)
			list, err := c.ListVolumes(authzCtx, storage.Pagination{20, 0}, test.Filters, test.Sorting)
			assert.Equal(t, err, test.ExpectedError)
			if err == nil {
			}
			assert.Equal(t, test.ExpectedSize, len(list.Items))
			assert.Equal(t, test.ExpectedSize, list.PageInfo.TotalNumber)

		})
	}
}
