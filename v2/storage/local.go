package storage

import (
	"context"
	"fmt"

	"github.com/equinor/flowify-workflows-server/models"

	"github.com/pkg/errors"
)

type LocalStorageClientImpl struct {
	c_store   map[models.ComponentReference]models.Component
	wf_store  map[models.ComponentReference]models.Workflow
	job_store map[models.ComponentReference]models.Job
}

func NewLocalNodeStorageClient() *LocalStorageClientImpl {
	return &LocalStorageClientImpl{c_store: make(map[models.ComponentReference]models.Component),
		wf_store: make(map[models.ComponentReference]models.Workflow)}
}

// Component storage impl

func (c *LocalStorageClientImpl) CreateComponent(ctx context.Context, node models.Component, workspace string) error {
	key := node.Metadata.Uid

	c.c_store[key] = node

	return nil
}

func (c *LocalStorageClientImpl) GetComponent(ctx context.Context, id models.ComponentReference) (models.Component, error) {
	v, ok := c.c_store[id]

	if !ok {
		return models.Component{}, errors.New(fmt.Sprintf("component %s not found", id))
	}

	return v, nil
}

func (c *LocalStorageClientImpl) ListComponentsMetadata(ctx context.Context, pagination Pagination, workspaceFilter []string) ([]models.Metadata, error) {
	res := []models.Metadata{}

	pos := 0
	i := 0
	for _, v := range c.c_store {
		if pos >= pagination.Skip && i < pagination.Limit {
			res = append(res, v.Metadata)
			i++
		}
		pos++
	}

	return res, nil
}

// Workflow storage impl

func (c *LocalStorageClientImpl) CreateWorkflow(ctx context.Context, node models.Workflow) error {
	key := node.Metadata.Uid

	c.wf_store[key] = node

	return nil
}

func (c *LocalStorageClientImpl) GetWorkflow(ctx context.Context, id models.ComponentReference) (models.Workflow, error) {
	v, ok := c.wf_store[id]

	if !ok {
		return models.Workflow{}, errors.New(fmt.Sprintf("component %s not found", id))
	}

	return v, nil
}

func (c *LocalStorageClientImpl) ListWorkflowsMetadata(ctx context.Context, pagination Pagination, workspaceFilter []string) ([]models.Metadata, error) {
	res := []models.Metadata{}

	pos := 0
	i := 0
	for _, v := range c.wf_store {
		if pos >= pagination.Skip && i < pagination.Limit {
			res = append(res, v.Metadata)
			i++
		}
		pos++
	}

	return res, nil
}

// jobs
func (c *LocalStorageClientImpl) GetJob(ctx context.Context, id models.ComponentReference) (models.Job, error) {
	v, ok := c.job_store[id]

	if !ok {
		return models.Job{}, errors.New(fmt.Sprintf("job %s not found", id))
	}

	return v, nil
}

func (c *LocalStorageClientImpl) CreateJob(ctx context.Context, node models.Job) error {
	key := node.Metadata.Uid

	c.job_store[key] = node

	return nil
}

func (c *LocalStorageClientImpl) ListJobsMetadata(ctx context.Context, pagination Pagination, workspaceFilter []string) ([]models.Metadata, error) {
	res := []models.Metadata{}

	pos := 0
	i := 0
	for _, v := range c.job_store {
		if pos >= pagination.Skip && i < pagination.Limit {
			res = append(res, v.Metadata)
			i++
		}
		pos++
	}

	return res, nil
}
