package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/equinor/flowify-workflows-server/models"
)

type Pagination struct {
	Limit int
	Skip  int
}

type ComponentClient interface {
	ListComponentsMetadata(ctx context.Context, pagination Pagination, filters []string, sorts []string) (models.MetadataList, error)
	ListComponentVersionsMetadata(ctx context.Context, id models.ComponentReference, pagination Pagination, sorts []string) (models.MetadataList, error)
	GetComponent(ctx context.Context, id interface{}) (models.Component, error)
	CreateComponent(ctx context.Context, node models.Component) error
	PutComponent(ctx context.Context, node models.Component) error
	PatchComponent(ctx context.Context, node models.Component, oldTimestamp time.Time) (models.Component, error)

	ListWorkflowsMetadata(ctx context.Context, pagination Pagination, filter []string, sorts []string) (models.MetadataWorkspaceList, error)
	ListWorkflowVersionsMetadata(ctx context.Context, id models.ComponentReference, pagination Pagination, sorts []string) (models.MetadataWorkspaceList, error)
	GetWorkflow(ctx context.Context, id interface{}) (models.Workflow, error)
	CreateWorkflow(ctx context.Context, node models.Workflow) error
	PutWorkflow(ctx context.Context, node models.Workflow) error
	PatchWorkflow(ctx context.Context, node models.Workflow, oldTimestamp time.Time) (models.Workflow, error)

	ListJobsMetadata(ctx context.Context, pagination Pagination, filter []string, sorts []string) (models.MetadataWorkspaceList, error)
	GetJob(ctx context.Context, id models.ComponentReference) (models.Job, error)
	CreateJob(ctx context.Context, node models.Job) error

	DeleteDocument(ctx context.Context, kind DocumentKind, id models.CRefVersion) (models.CRefVersion, error)
}

var (
	ErrNotFound            = fmt.Errorf("not found")
	ErrNoAccess            = fmt.Errorf("no access")
	ErrNewerDocumentExists = fmt.Errorf("newer document exists")
)

type VolumeClient interface {
	ListVolumes(ctx context.Context, pagination Pagination, filters []string, sorts []string) (models.FlowifyVolumeList, error)
	GetVolume(ctx context.Context, id models.ComponentReference) (models.FlowifyVolume, error)
	PutVolume(ctx context.Context, vol models.FlowifyVolume) error
	DeleteVolume(ctx context.Context, id models.ComponentReference) error
}
