package storage

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/equinor/flowify-workflows-server/models"
	"github.com/equinor/flowify-workflows-server/pkg/workspace"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

const (
	test_host              = "localhost"
	test_port              = 27017
	ext_mongo_hostname_env = "FLOWIFY_MONGO_ADDRESS"
	ext_mongo_port_env     = "FLOWIFY_MONGO_PORT"
	test_db_name           = "flowify-test"
)

func init() {
	if _, exists := os.LookupEnv(ext_mongo_hostname_env); !exists {
		os.Setenv(ext_mongo_hostname_env, test_host)
	}

	if _, exists := os.LookupEnv(ext_mongo_port_env); !exists {
		os.Setenv(ext_mongo_port_env, strconv.Itoa(test_port))
	}

	m := NewMongoClient()

	log.SetOutput(ioutil.Discard)
	log.Infof("Dropping db %s to make sure we're clean", test_db_name)

	m.Database(test_db_name).Drop(context.TODO())

}

// models.ComponentReference(uuid.MustParse("8c4f562a-a6b4-11ec-b909-0242ac120002"))

func makeComponent(meta *models.Metadata) models.Component {
	if meta == nil {
		meta = &models.Metadata{Name: "test-component",
			ModifiedBy: models.ModifiedBy{Oid: "0", Email: "test@author.com"},
			Timestamp:  time.Now().In(time.UTC).Truncate(time.Millisecond),
			Uid:        models.NewComponentReference()}
	}

	cmp := models.Component{
		ComponentBase: models.ComponentBase{Type: models.ComponentType("component"),
			Metadata: *meta},
		Implementation: models.Any{ImplementationBase: models.ImplementationBase{Type: "any"}}}

	return cmp
}

func makeWorkflow(meta *models.Metadata, workspace string) models.Workflow {
	if meta == nil {
		meta = &models.Metadata{Name: "test-component",
			ModifiedBy: models.ModifiedBy{Oid: "0", Email: "test@author.com"},
			Timestamp:  time.Now().In(time.UTC).Truncate(time.Millisecond),
			Uid:        models.NewComponentReference()}
	}

	cmp := makeComponent(meta)

	wf := models.Workflow{
		Metadata:  *meta,
		Component: cmp,
		Type:      "workflow",
		Workspace: workspace,
	}

	return wf
}

func makeJob(meta *models.Metadata, workspace string) models.Job {
	if meta == nil {
		meta = &models.Metadata{Name: "test-component",
			ModifiedBy: models.ModifiedBy{Oid: "0", Email: "test@author.com"},
			Timestamp:  time.Now().In(time.UTC).Truncate(time.Millisecond),
			Uid:        models.NewComponentReference()}
	}

	wf := makeWorkflow(meta, workspace)

	job := models.Job{
		Metadata: *meta,
		Workflow: wf,
		Type:     "job",
	}

	return job
}

func TestCreateComponent(t *testing.T) {

	cstorage := NewMongoStorageClient(NewMongoClient(), test_db_name)

	err := cstorage.CreateComponent(context.TODO(), makeComponent(nil))
	assert.Nil(t, err)
}

func TestGetComponent(t *testing.T) {
	cstorage := NewMongoStorageClient(NewMongoClient(), test_db_name)
	cmpv1 := makeComponent(nil)
	cmpv2 := cmpv1
	cmpv2.Description = "Second version of document"
	{
		// first add component to get
		err := cstorage.CreateComponent(context.TODO(), cmpv1)
		assert.Nil(t, err)
		// update component to create version 2
		err = cstorage.PutComponent(context.TODO(), cmpv2)
		assert.Nil(t, err)
	}
	cmp_cref := cmpv1.Metadata.Uid

	// test get latest component by cref
	cmp_out_latest, err := cstorage.GetComponent(context.TODO(), cmp_cref)
	assert.Nil(t, err)
	assert.Equal(t, cmpv2.Description, cmp_out_latest.Description)
	assert.Equal(t, models.VersionInit+1, cmp_out_latest.Version.Current)
	// test get latest component by crefversion without version specified
	cmp_out_latest, err = cstorage.GetComponent(context.TODO(), models.CRefVersion{Uid: cmp_cref})
	assert.Nil(t, err)
	assert.Equal(t, cmpv2.Description, cmp_out_latest.Description)
	assert.Equal(t, models.VersionInit+1, cmp_out_latest.Version.Current)

	// test get specific version of component
	cmp_out_first, err := cstorage.GetComponent(context.TODO(), models.CRefVersion{Uid: cmp_cref, Version: models.VersionInit})
	assert.Nil(t, err)
	assert.Equal(t, cmpv1.Description, cmp_out_first.Description)
	assert.Equal(t, models.VersionInit, cmp_out_first.Version.Current)

	// adding component to DB cause setting of tag "latest"
	assert.Equal(t, []string{}, cmp_out_first.Version.Tags)
	assert.Equal(t, []string{"latest"}, cmp_out_latest.Version.Tags)
}

func TestDeleteDocument(t *testing.T) {
	cstorage := NewMongoStorageClient(NewMongoClient(), test_db_name)
	NewMongoClient().Database(test_db_name).Drop(context.TODO())
	ws_test := "ws-test"
	authzCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, []workspace.Workspace{{Name: ws_test, HasAccess: true}})
	cmp := makeComponent(nil)
	{
		// first add component to get
		err := cstorage.CreateComponent(context.TODO(), cmp)
		assert.Nil(t, err)
		// add next version
		cmp.Description = "next version"
		err = cstorage.PutComponent(context.TODO(), cmp)
		assert.Nil(t, err)
	}
	wf := makeWorkflow(nil, ws_test)
	{
		err := cstorage.CreateWorkflow(authzCtx, wf)
		assert.Nil(t, err)
		// add next version
		wf.Description = "next version"
		err = cstorage.PutWorkflow(authzCtx, wf)
		assert.Nil(t, err)
	}
	jb := makeJob(nil, ws_test)
	{
		err := cstorage.CreateJob(authzCtx, jb)
		assert.Nil(t, err)
	}

	// cmp_v1, err := cstorage.GetComponent(context.TODO(), models.CRefVersion{Uid: cmp.Metadata.Uid, Version: models.VersionInit})
	// assert.Nil(t, err)
	cmp_v2, err := cstorage.GetComponent(context.TODO(), models.CRefVersion{Uid: cmp.Metadata.Uid, Version: models.VersionInit + 1})
	assert.Nil(t, err)
	wf_v1, err := cstorage.GetWorkflow(authzCtx, models.CRefVersion{Uid: wf.Metadata.Uid, Version: models.VersionInit})
	assert.Nil(t, err)
	wf_v2, err := cstorage.GetWorkflow(authzCtx, models.CRefVersion{Uid: wf.Metadata.Uid, Version: models.VersionInit + 1})
	assert.Nil(t, err)
	job, err := cstorage.GetJob(authzCtx, jb.Metadata.Uid)
	assert.Nil(t, err)

	type testCase struct {
		Name               string
		Kind               DocumentKind
		Id                 models.CRefVersion
		AuthzContext       context.Context
		ExpectedResult     models.CRefVersion
		ErrorExpected      bool
		ExpectedDbDocCount int
	}

	testCases := []testCase{
		{
			Name:               "Delete component, bad uid",
			Kind:               ComponentKind,
			Id:                 models.CRefVersion{},
			AuthzContext:       context.TODO(),
			ExpectedResult:     models.CRefVersion{},
			ErrorExpected:      true,
			ExpectedDbDocCount: 2,
		},
		{
			Name:               "Delete component v.2",
			Kind:               ComponentKind,
			Id:                 models.CRefVersion{Uid: cmp_v2.Uid, Version: cmp_v2.Version.Current},
			AuthzContext:       context.TODO(),
			ExpectedResult:     models.CRefVersion{Uid: cmp_v2.Uid, Version: cmp_v2.Version.Current},
			ErrorExpected:      false,
			ExpectedDbDocCount: 1,
		},
		{
			Name:               "Delete workflow, no access",
			Kind:               WorkflowKind,
			Id:                 models.CRefVersion{Uid: wf_v1.Uid, Version: wf_v1.Version.Current},
			AuthzContext:       context.WithValue(context.TODO(), workspace.WorkspaceKey, []workspace.Workspace{{Name: "test", HasAccess: true}}),
			ExpectedResult:     models.CRefVersion{},
			ErrorExpected:      true,
			ExpectedDbDocCount: 2,
		},
		{
			Name:               "Delete workflow v. 1",
			Kind:               WorkflowKind,
			Id:                 models.CRefVersion{Uid: wf_v1.Uid, Version: wf_v1.Version.Current},
			AuthzContext:       authzCtx,
			ExpectedResult:     models.CRefVersion{Uid: wf_v1.Uid, Version: wf_v1.Version.Current},
			ErrorExpected:      false,
			ExpectedDbDocCount: 1,
		},
		{
			Name:               "Delete workflow v. 2",
			Kind:               WorkflowKind,
			Id:                 models.CRefVersion{Uid: wf_v2.Uid, Version: wf_v2.Version.Current},
			AuthzContext:       authzCtx,
			ExpectedResult:     models.CRefVersion{Uid: wf_v2.Uid, Version: wf_v2.Version.Current},
			ErrorExpected:      false,
			ExpectedDbDocCount: 0,
		},
		{
			Name:               "Delete job, bad uid",
			Kind:               JobKind,
			Id:                 models.CRefVersion{},
			AuthzContext:       authzCtx,
			ExpectedResult:     models.CRefVersion{},
			ErrorExpected:      true,
			ExpectedDbDocCount: 1,
		},
		{
			Name:               "Delete job, no access",
			Kind:               JobKind,
			Id:                 models.CRefVersion{Uid: job.Uid},
			AuthzContext:       context.WithValue(context.TODO(), workspace.WorkspaceKey, []workspace.Workspace{{Name: "test", HasAccess: true}}),
			ExpectedResult:     models.CRefVersion{},
			ErrorExpected:      true,
			ExpectedDbDocCount: 1,
		},
		{
			Name:               "Delete job",
			Kind:               JobKind,
			Id:                 models.CRefVersion{Uid: job.Uid},
			AuthzContext:       authzCtx,
			ExpectedResult:     models.CRefVersion{Uid: job.Uid},
			ErrorExpected:      false,
			ExpectedDbDocCount: 0,
		},
	}

	for _, test := range testCases {
		t.Run(test.Name, func(t *testing.T) {
			result, err := cstorage.DeleteDocument(test.AuthzContext, test.Kind, test.Id)
			if test.ErrorExpected {
				assert.Error(t, err)
			} else {
				assert.Equal(t, test.ExpectedResult, result)
			}
			switch test.Kind {
			case ComponentKind:
				lst, err := cstorage.ListComponentsMetadata(authzCtx, Pagination{Limit: 10}, nil, nil)
				assert.Nil(t, err)
				assert.Equal(t, test.ExpectedDbDocCount, len(lst.Items))
			case WorkflowKind:
				lst, err := cstorage.ListWorkflowsMetadata(authzCtx, Pagination{Limit: 10}, nil, nil)
				assert.Nil(t, err)
				assert.Equal(t, test.ExpectedDbDocCount, len(lst.Items))
			case JobKind:
				lst, err := cstorage.ListJobsMetadata(authzCtx, Pagination{Limit: 10}, nil, nil)
				assert.Nil(t, err)
				assert.Equal(t, test.ExpectedDbDocCount, len(lst.Items))
			}
		})
	}
}

func TestDeleteComponentVersions(t *testing.T) {
	cstorage := NewMongoStorageClient(NewMongoClient(), test_db_name)
	cmp := makeComponent(nil)
	{
		// first add component to get
		err := cstorage.CreateComponent(context.TODO(), cmp)
		assert.Nil(t, err)
		// add next version
		cmp.Description = "next version"
		cstorage.PutComponent(context.TODO(), cmp)
	}

	cmp_v1, err := cstorage.GetComponent(context.TODO(), models.CRefVersion{Uid: cmp.Metadata.Uid, Version: models.VersionInit})
	assert.Nil(t, err)
	cmp_v2, err := cstorage.GetComponent(context.TODO(), models.CRefVersion{Uid: cmp.Metadata.Uid, Version: models.VersionInit + 1})
	assert.Nil(t, err)
	// adding component to DB cause setting of init version no.
	cmp.Version.Current = models.VersionInit
	cmp.Description = ""
	cmp.Version.Tags = []string{}
	assert.Equal(t, cmp, cmp_v1)

	badId := models.CRefVersion{}
	_, err = cstorage.DeleteDocument(context.TODO(), WorkflowKind, badId)
	assert.Error(t, err)

	idV2 := models.CRefVersion{Uid: cmp.Uid, Version: cmp_v2.Version.Current}
	cref, err := cstorage.DeleteDocument(context.TODO(), ComponentKind, idV2)
	assert.Nil(t, err)
	assert.Equal(t, idV2, cref)

	// First version should left in DB
	cmp1, err := cstorage.GetComponent(context.TODO(), cmp.Metadata.Uid)
	assert.Nil(t, err)
	assert.Equal(t, models.VersionInit, cmp1.Version.Current)
}

func TestListComponents(t *testing.T) {
	cstorage := NewMongoStorageClient(NewMongoClient(), test_db_name)
	Day := time.Duration(time.Hour * 24)
	Days := func(i int) time.Duration { return time.Duration(i) * Day }
	{
		// drop db to make sure that at the end DB will contain one components
		NewMongoClient().Database(test_db_name).Drop(context.TODO())
		// first add components to list
		for i := 0; i < 5; i++ {
			err := cstorage.CreateComponent(context.TODO(),
				makeComponent(&models.Metadata{Name: fmt.Sprintf("test-%d", i),
					ModifiedBy: models.ModifiedBy{Oid: "0", Email: "flow@flowify.io"}, Uid: models.NewComponentReference(),
					Timestamp: time.Now().Add(Days(i - 8)).UTC().Truncate(time.Second)}))
			assert.Nil(t, err)
		}
		for i := 5; i < 10; i++ {
			err := cstorage.CreateComponent(context.TODO(),
				makeComponent(&models.Metadata{Name: fmt.Sprintf("test-%d", i),
					ModifiedBy: models.ModifiedBy{Oid: "1", Email: "swirl@flowify.io"}, Uid: models.NewComponentReference(),
					Timestamp: time.Now().Add(Days(i - 8)).UTC().Truncate(time.Second)}))
			assert.Nil(t, err)
		}
		for i := 10; i < 15; i++ {
			err := cstorage.CreateComponent(context.TODO(),
				makeComponent(&models.Metadata{Name: fmt.Sprintf("test-%d", i),
					ModifiedBy: models.ModifiedBy{Oid: "2", Email: "snow@google.com"}, Uid: models.NewComponentReference(),
					Timestamp: time.Now().Add(Days(i - 8)).UTC().Truncate(time.Second)}))
			assert.Nil(t, err)
		}

	}

	var testCases = []struct {
		Name            string   // name
		Filters         []string // input
		Sorting         []string
		Pagination      Pagination
		ExpectedLength  int
		FirstItemAuthor models.ModifiedBy // the author of the first item in the returned list
	}{
		{"Empty filter100-0", nil, []string{"-timestamp"}, Pagination{Limit: 100, Skip: 0}, 15, models.ModifiedBy{Oid: "2", Email: "snow@google.com"}},
		{"Empty filter10-0", nil, []string{"+timestamp"}, Pagination{Limit: 10, Skip: 0}, 10, models.ModifiedBy{Oid: "0", Email: "flow@flowify.io"}},
		{"Empty filter10-5", nil, nil, Pagination{Limit: 10, Skip: 5}, 10, models.ModifiedBy{Oid: "1", Email: "swirl@flowify.io"}},
		{"Empty filter5-12", nil, nil, Pagination{Limit: 5, Skip: 12}, 3, models.ModifiedBy{Oid: "2", Email: "snow@google.com"}},
		{"flow@flowify.io", []string{"modifiedBy.email[==]=flow@flowify.io"}, nil, Pagination{Limit: 10, Skip: 0}, 5, models.ModifiedBy{Oid: "0", Email: "flow@flowify.io"}},
		{"swirl@flowify.io", []string{"modifiedBy.email[==]=swirl@flowify.io"}, nil, Pagination{Limit: 10, Skip: 0}, 5, models.ModifiedBy{Oid: "1", Email: "swirl@flowify.io"}},
		{"snow@google.com", []string{"modifiedBy.email[==]=snow@google.com"}, nil, Pagination{Limit: 10, Skip: 0}, 5, models.ModifiedBy{Oid: "2", Email: "snow@google.com"}},
		{"regexp search is case insensitive \\w@FLOWIFY.COM",
			[]string{"modifiedBy.email[search]=\\w@flowify.io"},
			nil, Pagination{Limit: 10, Skip: 0}, 10, models.ModifiedBy{Oid: "0", Email: "flow@flowify.io"}},
		{"flow&swirl has no intersection",
			[]string{"modifiedBy[==]=flow@flowify.io", "modifiedBy[==]=swirl@flowify.io"},
			nil, Pagination{Limit: 10, Skip: 0}, 0, models.ModifiedBy{}},
		{"before today",
			[]string{fmt.Sprintf("timestamp[<=]=%s", time.Now().UTC().Format(time.RFC3339))},
			nil, Pagination{Limit: 10, Skip: 0}, 9, models.ModifiedBy{Oid: "0", Email: "flow@flowify.io"}},
		{"after today",
			[]string{fmt.Sprintf("timestamp[>]=%s", time.Now().UTC().Format(time.RFC3339))},
			nil, Pagination{Limit: 10, Skip: 0}, 6, models.ModifiedBy{Oid: "1", Email: "swirl@flowify.io"}},
		{"after today, by swirl",
			[]string{fmt.Sprintf("timestamp[>]=%s", time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)),
				"modifiedBy.email[==]=swirl@flowify.io"},
			nil, Pagination{Limit: 10, Skip: 0}, 1, models.ModifiedBy{Oid: "1", Email: "swirl@flowify.io"}},
	}

	for _, test := range testCases {
		t.Run(test.Name, func(t *testing.T) {

			cmp_list, err := cstorage.ListComponentsMetadata(context.TODO(),
				test.Pagination, test.Filters, test.Sorting)
			assert.Nil(t, err)
			logrus.Info(cmp_list)
			assert.Equal(t, test.ExpectedLength, len(cmp_list.Items), test.Name, test.Filters)
			if test.Sorting != nil {
				assert.Equal(t, test.FirstItemAuthor, cmp_list.Items[0].ModifiedBy)
			}
			assert.GreaterOrEqual(t, test.Pagination.Limit, len(cmp_list.Items))
			assert.GreaterOrEqual(t, cmp_list.PageInfo.TotalNumber, len(cmp_list.Items))
		})
	}

}

func TestPatchComponent(t *testing.T) {
	NewMongoClient().Database(test_db_name).Drop(context.TODO())
	cstorage := NewMongoStorageClient(NewMongoClient(), test_db_name)
	cmpV1 := makeComponent(
		&models.Metadata{
			Name:       "test-component",
			ModifiedBy: models.ModifiedBy{Oid: "0", Email: "test@flowify.io"},
			Timestamp:  time.Now().In(time.UTC).Truncate(time.Millisecond),
			Uid:        models.NewComponentReference(),
			Version:    models.Version{Current: 1},
		},
	)
	err := cstorage.CreateComponent(context.TODO(), cmpV1)
	assert.NoError(t, err)
	err = cstorage.PutComponent(context.TODO(), cmpV1)
	assert.NoError(t, err)
	cmpV2, err := cstorage.GetComponent(context.TODO(), cmpV1.Uid)
	assert.NoError(t, err)

	type testCase struct {
		Name            string
		UpdatedDocument models.Component
		LastTimestamp   time.Time
		ExpectedResult  models.Component
		ExpectedError   bool
	}

	cmpV3 := cmpV2
	cmpV3.Metadata.Timestamp = time.Now().In(time.UTC).Truncate(time.Millisecond)
	cmpV3.Description = "new description"
	cmpV4 := cmpV3
	cmpV4.Name = "updated name"
	cmpV4.Version.Current = cmpV1.Version.Current
	testCases := []testCase{
		{
			Name:            "Successful patch",
			UpdatedDocument: cmpV3,
			LastTimestamp:   cmpV2.Timestamp,
			ExpectedResult:  cmpV3,
			ExpectedError:   false,
		},
		{
			Name:            "Newer document exists",
			UpdatedDocument: cmpV4,
			LastTimestamp:   cmpV2.Timestamp,
			ExpectedResult:  models.Component{},
			ExpectedError:   true,
		},
		{
			Name:            "Only last version can be patched",
			UpdatedDocument: cmpV4,
			LastTimestamp:   cmpV3.Timestamp,
			ExpectedResult:  models.Component{},
			ExpectedError:   true,
		},
	}

	for _, test := range testCases {
		t.Run(test.Name, func(t *testing.T) {
			log.Info("Test: ", test.Name)
			res, err := cstorage.PatchComponent(context.TODO(), test.UpdatedDocument, test.LastTimestamp)
			assert.Equal(t, test.ExpectedResult, res)
			if test.ExpectedError {
				assert.Error(t, err)
			}
		})
	}

}

// Workflows

func TestCreateWorkflow(t *testing.T) {

	type testCase struct {
		Name            string
		Workspace       string
		WorkspaceAccess []workspace.Workspace
		ExpectedError   error
	}

	testCases := []testCase{
		{Name: "No authz context",
			WorkspaceAccess: []workspace.Workspace{{}},
			Workspace:       "test",
			ExpectedError:   fmt.Errorf("user has no access to workspace (%s)", "test")},
		{Name: "Good authz context",
			WorkspaceAccess: []workspace.Workspace{{Name: "test", HasAccess: true}},
			Workspace:       "test",
			ExpectedError:   nil},
		{Name: "Authz context with name but no access",
			WorkspaceAccess: []workspace.Workspace{{Name: "test", HasAccess: false}},
			Workspace:       "test",
			ExpectedError:   fmt.Errorf("user has no access to workspace (%s)", "test")},
		{Name: "Authz context with similar name/access",
			WorkspaceAccess: []workspace.Workspace{{Name: "tes", HasAccess: true}},
			Workspace:       "test",
			ExpectedError:   fmt.Errorf("user has no access to workspace (%s)", "test")},
		{Name: "Authz context with similar name/access",
			WorkspaceAccess: []workspace.Workspace{{Name: "test ", HasAccess: true}},
			Workspace:       "test",
			ExpectedError:   fmt.Errorf("user has no access to workspace (%s)", "test")},
		{Name: "Authz context with subset name/access",
			WorkspaceAccess: []workspace.Workspace{{Name: "testt", HasAccess: true}},
			Workspace:       "test",
			ExpectedError:   fmt.Errorf("user has no access to workspace (%s)", "test")},
	}

	cstorage := NewMongoStorageClient(NewMongoClient(), test_db_name)

	for _, test := range testCases {
		t.Run(test.Name, func(t *testing.T) {
			log.Info("Test: ", test.Name, test.WorkspaceAccess, test.Workspace)

			authzCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, test.WorkspaceAccess)
			wf := models.Workflow{Metadata: models.Metadata{Name: test.Name}, Component: makeComponent(nil), Workspace: test.Workspace}
			err := cstorage.CreateWorkflow(authzCtx, wf)
			assert.Equal(t, err, test.ExpectedError)
		})
	}
}

func TestGetWorkflow(t *testing.T) {
	cstorage := NewMongoStorageClient(NewMongoClient(), test_db_name)

	wf := models.Workflow{
		Metadata:  models.Metadata{Name: "test-wf", Uid: models.NewComponentReference(), Version: models.Version{Current: models.VersionInit, Tags: []string{"latest"}}},
		Component: makeComponent(nil), Workspace: "test"}

	{
		// create context with access
		ws := []workspace.Workspace{{Name: "test", HasAccess: true}}
		authCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, ws)
		err := cstorage.CreateWorkflow(authCtx, wf)
		assert.Nil(t, err)
	}

	type testCase struct {
		Name            string
		WorkspaceAccess []workspace.Workspace
		ExpectedError   error
	}

	testCases := []testCase{
		{Name: "No authz context",
			WorkspaceAccess: []workspace.Workspace{{}},
			ExpectedError:   fmt.Errorf("user has no access to workspace (%s)", "test")},
		{Name: "Good authz context",
			WorkspaceAccess: []workspace.Workspace{{Name: "test", HasAccess: true}},
			ExpectedError:   nil},
		{Name: "Authz context with name but no access",
			WorkspaceAccess: []workspace.Workspace{{Name: "test", HasAccess: false}},
			ExpectedError:   fmt.Errorf("user has no access to workspace (%s)", "test")},
		{Name: "Authz context with similar name/access",
			WorkspaceAccess: []workspace.Workspace{{Name: "tes", HasAccess: true}},
			ExpectedError:   fmt.Errorf("user has no access to workspace (%s)", "test")},
	}

	for _, test := range testCases {
		t.Run(test.Name, func(t *testing.T) {
			authzCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, test.WorkspaceAccess)
			wf_out, err := cstorage.GetWorkflow(authzCtx, wf.Metadata.Uid)
			assert.Equal(t, err, test.ExpectedError)
			if err == nil {
				// make sure the roundtrip works when auth is good
				assert.Equal(t, wf, wf_out)
			}
		})
	}
}

func TestPutWorkflow(t *testing.T) {
	cstorage := NewMongoStorageClient(NewMongoClient(), test_db_name)

	wf := models.Workflow{Metadata: models.Metadata{Name: "test-wf", Uid: models.NewComponentReference()}, Component: makeComponent(nil), Workspace: "test"}

	{
		// create context with access
		ws := []workspace.Workspace{{Name: "test", HasAccess: true}}
		authCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, ws)
		err := cstorage.CreateWorkflow(authCtx, wf)
		assert.Nil(t, err)
	}

	type testCase struct {
		Name            string
		WorkspaceAccess []workspace.Workspace
		Workspace       string
		ExpectedFail    bool
		ExpectedError   error
	}

	testCases := []testCase{
		{Name: "No authz context",
			WorkspaceAccess: []workspace.Workspace{{}},
			Workspace:       "test",
			ExpectedFail:    true,
			ExpectedError:   errors.Wrap(fmt.Errorf("user has no access to workspace (%s)", "test"), "could not access workflow for storage")},
		{Name: "Good authz context",
			WorkspaceAccess: []workspace.Workspace{{Name: "test", HasAccess: true}},
			Workspace:       "test",
			ExpectedFail:    false,
			ExpectedError:   nil},
		{Name: "Good authz context, try moving ws",
			WorkspaceAccess: []workspace.Workspace{{Name: "test", HasAccess: true}},
			Workspace:       "test2",
			ExpectedFail:    true,
			ExpectedError:   fmt.Errorf("cannot move workflows from workspace (%s)", "test")},
		{Name: "Authz context with name but no access",
			WorkspaceAccess: []workspace.Workspace{{Name: "test", HasAccess: false}},
			Workspace:       "test",
			ExpectedFail:    true,
			ExpectedError:   errors.Wrap(fmt.Errorf("user has no access to workspace (%s)", "test"), "could not access workflow for storage")},
		{Name: "Authz context with similar name/access",
			WorkspaceAccess: []workspace.Workspace{{Name: "tes", HasAccess: true}},
			Workspace:       "test",
			ExpectedFail:    true,
			ExpectedError:   errors.Wrap(fmt.Errorf("user has no access to workspace (%s)", "test"), "could not access workflow for storage")},
	}

	for _, test := range testCases {
		t.Run(test.Name, func(t *testing.T) {
			twf := wf
			twf.Description = test.Name
			twf.Workspace = test.Workspace
			authzCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, test.WorkspaceAccess)
			err := cstorage.PutWorkflow(authzCtx, twf)
			if test.ExpectedFail {
				assert.NotNil(t, err)
				assert.EqualError(t, err, test.ExpectedError.Error(), test.Name)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestListWorkflows(t *testing.T) {
	cstorage := NewMongoStorageClient(NewMongoClient(), test_db_name)

	Day := time.Duration(time.Hour * 24)
	Days := func(i int) time.Duration { return time.Duration(i) * Day }
	{
		// drop db to make sure that at the end DB will contain one components
		NewMongoClient().Database(test_db_name).Drop(context.TODO())

		// first add components to list
		for i := 0; i < 5; i++ {
			authzCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, []workspace.Workspace{{Name: "test", HasAccess: true}})
			err := cstorage.CreateWorkflow(authzCtx,
				makeWorkflow(&models.Metadata{Name: fmt.Sprintf("test-%d", i),
					ModifiedBy: models.ModifiedBy{Oid: "0", Email: "flow@flowify.io"}, Uid: models.NewComponentReference(),
					Timestamp: time.Now().Add(Days(i - 8)).UTC().Truncate(time.Second)}, "test"))
			assert.Nil(t, err)
		}
		for i := 5; i < 10; i++ {
			authzCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, []workspace.Workspace{{Name: "test-2", HasAccess: true}})
			err := cstorage.CreateWorkflow(authzCtx,
				makeWorkflow(&models.Metadata{Name: fmt.Sprintf("test-%d", i),
					ModifiedBy: models.ModifiedBy{Oid: "1", Email: "swirl@flowify.io"}, Uid: models.NewComponentReference(),
					Timestamp: time.Now().Add(Days(i - 8)).UTC().Truncate(time.Second)}, "test-2"))
			assert.Nil(t, err)
		}
		for i := 10; i < 15; i++ {
			authzCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, []workspace.Workspace{{Name: "test-3", HasAccess: true}})
			err := cstorage.CreateWorkflow(authzCtx,
				makeWorkflow(&models.Metadata{Name: fmt.Sprintf("test-%d", i),
					ModifiedBy: models.ModifiedBy{Oid: "2", Email: "snow@google.com"}, Uid: models.NewComponentReference(),
					Timestamp: time.Now().Add(Days(i - 8)).UTC().Truncate(time.Second)}, "test-3"))
			assert.Nil(t, err)
		}

	}

	type testCase struct {
		Name                string
		Filters             []string
		Sorting             []string
		WorkspaceAccess     []workspace.Workspace
		ExpectedError       error
		ExpectedSize        int
		ExpectedFrontAuthor string
	}

	testCases := []testCase{
		{Name: "No authz context",
			Filters:             nil,
			Sorting:             nil,
			WorkspaceAccess:     nil,
			ExpectedError:       nil,
			ExpectedSize:        0,
			ExpectedFrontAuthor: "s"},
		{Name: "Everything sorted chronologically ascending",
			Filters:             nil,
			Sorting:             []string{"+timestamp"},
			WorkspaceAccess:     []workspace.Workspace{{Name: "test", HasAccess: true}, {Name: "test-2", HasAccess: true}},
			ExpectedError:       nil,
			ExpectedSize:        10,
			ExpectedFrontAuthor: "flow@flowify.io"},
		{Name: "Everything sorted chronologically descending",
			Filters:             nil,
			Sorting:             []string{"-timestamp"},
			WorkspaceAccess:     []workspace.Workspace{{Name: "test", HasAccess: true}, {Name: "test-2", HasAccess: true}},
			ExpectedError:       nil,
			ExpectedSize:        10,
			ExpectedFrontAuthor: "swirl@flowify.io"},
		{Name: "Good authz context but only search for test ws",
			Filters:             []string{"workspace[==]=test"},
			Sorting:             nil,
			WorkspaceAccess:     []workspace.Workspace{{Name: "test", HasAccess: true}, {Name: "test-2", HasAccess: true}},
			ExpectedError:       nil,
			ExpectedSize:        5,
			ExpectedFrontAuthor: "flow@flowify.io"},
		{Name: "Good authz context (not test-3), regexp for test*, descending",
			Filters:             []string{"workspace[search]=test.*"},
			Sorting:             []string{"-timestamp"},
			WorkspaceAccess:     []workspace.Workspace{{Name: "test", HasAccess: true}, {Name: "test-2", HasAccess: true}},
			ExpectedError:       nil,
			ExpectedSize:        10,
			ExpectedFrontAuthor: "swirl@flowify.io"},
		{Name: "Good authz context sort by author",
			Filters:             nil,
			Sorting:             []string{"+modifiedBy"},
			WorkspaceAccess:     []workspace.Workspace{{Name: "test", HasAccess: true}, {Name: "test-2", HasAccess: true}, {Name: "test-3", HasAccess: true}},
			ExpectedError:       nil,
			ExpectedSize:        15,
			ExpectedFrontAuthor: "flow@flowify.io"},
		{Name: "Authz context with name but no access",
			Filters:             nil,
			Sorting:             nil,
			WorkspaceAccess:     []workspace.Workspace{{Name: "test", HasAccess: false}},
			ExpectedError:       nil,
			ExpectedSize:        0,
			ExpectedFrontAuthor: ""},
		{Name: "Authz context with similar name/access",
			Filters:             []string{},
			Sorting:             nil,
			WorkspaceAccess:     []workspace.Workspace{{Name: "tes", HasAccess: true}},
			ExpectedError:       nil,
			ExpectedSize:        0,
			ExpectedFrontAuthor: "",
		},
	}

	for _, test := range testCases {
		t.Run(test.Name, func(t *testing.T) {
			authzCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, test.WorkspaceAccess)
			list, err := cstorage.ListWorkflowsMetadata(authzCtx, Pagination{20, 0}, test.Filters, test.Sorting)
			assert.Equal(t, err, test.ExpectedError)
			assert.Equal(t, test.ExpectedSize, len(list.Items))
			if len(list.Items) > 0 && test.Sorting != nil {
				assert.Equal(t, test.ExpectedFrontAuthor, list.Items[0].ModifiedBy.Email)
			}
			assert.GreaterOrEqual(t, list.PageInfo.TotalNumber, test.ExpectedSize)

		})
	}
}

// Jobs

func TestCreateJob(t *testing.T) {

	type testCase struct {
		Name            string
		Workspace       string
		WorkspaceAccess []workspace.Workspace
		ExpectedError   error
	}

	testCases := []testCase{
		{Name: "No authz context",
			WorkspaceAccess: []workspace.Workspace{{}},
			Workspace:       "test",
			ExpectedError:   fmt.Errorf("user has no access to workspace (%s)", "test")},
		{Name: "Good authz context",
			WorkspaceAccess: []workspace.Workspace{{Name: "test", HasAccess: true}},
			Workspace:       "test",
			ExpectedError:   nil},
		{Name: "Authz context with name but no access",
			WorkspaceAccess: []workspace.Workspace{{Name: "test", HasAccess: false}},
			Workspace:       "test",
			ExpectedError:   fmt.Errorf("user has no access to workspace (%s)", "test")},
		{Name: "Authz context with similar name/access",
			WorkspaceAccess: []workspace.Workspace{{Name: "tes", HasAccess: true}},
			Workspace:       "test",
			ExpectedError:   fmt.Errorf("user has no access to workspace (%s)", "test")},
		{Name: "Authz context with similar name/access",
			WorkspaceAccess: []workspace.Workspace{{Name: "test ", HasAccess: true}},
			Workspace:       "test",
			ExpectedError:   fmt.Errorf("user has no access to workspace (%s)", "test")},
		{Name: "Authz context with subset name/access",
			WorkspaceAccess: []workspace.Workspace{{Name: "testing", HasAccess: true}},
			Workspace:       "test",
			ExpectedError:   fmt.Errorf("user has no access to workspace (%s)", "test")},
	}

	cstorage := NewMongoStorageClient(NewMongoClient(), test_db_name)

	for _, test := range testCases {
		t.Run(test.Name, func(t *testing.T) {
			log.Info("Test: ", test.Name, test.WorkspaceAccess, test.Workspace)

			authzCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, test.WorkspaceAccess)
			job := models.Job{Metadata: models.Metadata{Name: "no uid"}, Workflow: models.Workflow{Metadata: models.Metadata{Name: test.Name}, Component: makeComponent(nil), Workspace: test.Workspace}}
			err := cstorage.CreateJob(authzCtx, job)
			assert.Equal(t, test.ExpectedError, err)
		})
	}
}

func TestGetJob(t *testing.T) {
	cstorage := NewMongoStorageClient(NewMongoClient(), test_db_name)

	id := models.NewComponentReference()
	name := "test-job-" + id.String()[:4]
	job := models.Job{Metadata: models.Metadata{Name: name, Uid: id}, Workflow: models.Workflow{Metadata: models.Metadata{Name: "test-wf", Uid: models.NewComponentReference()}, Component: makeComponent(nil), Workspace: "test"}}
	{
		// create context with access
		ws := []workspace.Workspace{{Name: "test", HasAccess: true}}
		authCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, ws)
		err := cstorage.CreateJob(authCtx, job)
		assert.Nil(t, err)
	}

	type testCase struct {
		Name            string
		WorkspaceAccess []workspace.Workspace
		ExpectedError   error
	}

	testCases := []testCase{
		{Name: "No authz context",
			WorkspaceAccess: []workspace.Workspace{{}},
			ExpectedError:   fmt.Errorf("user has no access to workspace (%s)", "test")},
		{Name: "Good authz context",
			WorkspaceAccess: []workspace.Workspace{{Name: "test", HasAccess: true}},
			ExpectedError:   nil},
		{Name: "Authz context with name but no access",
			WorkspaceAccess: []workspace.Workspace{{Name: "test", HasAccess: false}},
			ExpectedError:   fmt.Errorf("user has no access to workspace (%s)", "test")},
		{Name: "Authz context with similar name/access",
			WorkspaceAccess: []workspace.Workspace{{Name: "tes", HasAccess: true}},
			ExpectedError:   fmt.Errorf("user has no access to workspace (%s)", "test")},
	}

	for _, test := range testCases {
		t.Run(test.Name, func(t *testing.T) {
			authzCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, test.WorkspaceAccess)
			job_out, err := cstorage.GetJob(authzCtx, job.Metadata.Uid)
			assert.Equal(t, err, test.ExpectedError)
			if err == nil {
				// make sure the roundtrip works when auth is good
				assert.Equal(t, job, job_out)
			}
		})
	}
}

func TestListJobs(t *testing.T) {
	cstorage := NewMongoStorageClient(NewMongoClient(), test_db_name)
	Day := time.Duration(time.Hour * 24)
	Days := func(i int) time.Duration { return time.Duration(i) * Day }
	{
		// drop db to make sure that at the end DB will contain one components
		NewMongoClient().Database(test_db_name).Drop(context.TODO())

		// first add components to list
		for i := 0; i < 5; i++ {
			authzCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, []workspace.Workspace{{Name: "test", HasAccess: true}})
			err := cstorage.CreateJob(authzCtx,
				makeJob(&models.Metadata{Name: fmt.Sprintf("test-%d", i),
					ModifiedBy: models.ModifiedBy{Oid: "0", Email: "flow@flowify.io"}, Uid: models.NewComponentReference(),
					Timestamp: time.Now().Add(Days(i - 8)).UTC().Truncate(time.Second)}, "test"))
			assert.Nil(t, err)
		}
		for i := 5; i < 10; i++ {
			authzCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, []workspace.Workspace{{Name: "test-2", HasAccess: true}})
			err := cstorage.CreateJob(authzCtx,
				makeJob(&models.Metadata{Name: fmt.Sprintf("test-%d", i),
					ModifiedBy: models.ModifiedBy{Oid: "1", Email: "swirl@flowify.io"}, Uid: models.NewComponentReference(),
					Timestamp: time.Now().Add(Days(i - 8)).UTC().Truncate(time.Second)}, "test-2"))
			assert.Nil(t, err)
		}
		for i := 10; i < 15; i++ {
			authzCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, []workspace.Workspace{{Name: "test-3", HasAccess: true}})
			err := cstorage.CreateJob(authzCtx,
				makeJob(&models.Metadata{Name: fmt.Sprintf("test-%d", i),
					ModifiedBy: models.ModifiedBy{Oid: "0", Email: "snow@google.com"}, Uid: models.NewComponentReference(),
					Timestamp: time.Now().Add(Days(i - 8)).UTC().Truncate(time.Second)}, "test-3"))
			assert.Nil(t, err)
		}

	}

	type testCase struct {
		Name            string
		Filters         []string
		Sorting         []string
		Pagination      Pagination
		WorkspaceAccess []workspace.Workspace
		ExpectedError   error
		ExpectedSize    int
	}

	testCases := []testCase{
		{Name: "No authz context",
			Filters:         nil,
			Sorting:         nil,
			Pagination:      Pagination{10, 0},
			WorkspaceAccess: nil,
			ExpectedError:   nil,
			ExpectedSize:    0},
		{Name: "Good authz context",
			Filters:         nil,
			Sorting:         nil,
			Pagination:      Pagination{10, 0},
			WorkspaceAccess: []workspace.Workspace{{Name: "test", HasAccess: true}},
			ExpectedError:   nil,
			ExpectedSize:    5},
		{Name: "All authz contexts",
			Filters:         nil,
			Sorting:         nil,
			Pagination:      Pagination{20, 0},
			WorkspaceAccess: []workspace.Workspace{{Name: "test", HasAccess: true}, {Name: "test-2", HasAccess: true}, {Name: "test-3", HasAccess: true}},
			ExpectedError:   nil,
			ExpectedSize:    15},
		{Name: "All authz contexts, offset",
			Filters:         nil,
			Sorting:         nil,
			Pagination:      Pagination{10, 5},
			WorkspaceAccess: []workspace.Workspace{{Name: "test", HasAccess: true}, {Name: "test-2", HasAccess: true}, {Name: "test-3", HasAccess: true}},
			ExpectedError:   nil,
			ExpectedSize:    10},
		{Name: "Good authz context and filter",
			Filters:         []string{"workflow.workspace[==]=test-2"},
			Sorting:         nil,
			Pagination:      Pagination{10, 0},
			WorkspaceAccess: []workspace.Workspace{{Name: "test", HasAccess: true}, {Name: "test-2", HasAccess: true}},
			ExpectedError:   nil,
			ExpectedSize:    5},
		{Name: "Authz context with name but no access",
			Filters:         nil,
			Sorting:         nil,
			Pagination:      Pagination{10, 0},
			WorkspaceAccess: []workspace.Workspace{{Name: "test-2", HasAccess: false}},
			ExpectedError:   nil,
			ExpectedSize:    0},
		{Name: "Authz context with similar name/access",
			Filters:         []string{},
			Sorting:         nil,
			Pagination:      Pagination{10, 0},
			WorkspaceAccess: []workspace.Workspace{{Name: "tes", HasAccess: true}},
			ExpectedError:   nil,
			ExpectedSize:    0,
		},
	}

	for _, test := range testCases {
		t.Run(test.Name, func(t *testing.T) {
			log.Info("Test: ", test.Name, test.WorkspaceAccess, test.ExpectedError)
			authzCtx := context.WithValue(context.TODO(), workspace.WorkspaceKey, test.WorkspaceAccess)
			metaList, err := cstorage.ListJobsMetadata(authzCtx, test.Pagination, test.Filters, test.Sorting)
			assert.Equal(t, err, test.ExpectedError)
			assert.Equal(t, test.ExpectedSize, len(metaList.Items))
			assert.GreaterOrEqual(t, metaList.PageInfo.TotalNumber, test.ExpectedSize, test.Name)
			for _, j := range metaList.Items {
				assert.Contains(t, test.WorkspaceAccess, workspace.Workspace{Name: j.Workspace, HasAccess: true})
			}
		})
	}
}
