package rest

import (
	"context"
	"fmt"
	"net/http"
	"path"

	"github.com/equinor/flowify-workflows-server/v2/models"
	"github.com/equinor/flowify-workflows-server/v2/storage"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

func RegisterWorkflowRoutes(r *mux.Route, componentClient storage.ComponentClient) {
	s := r.Subrouter()

	const intype = "application/json"
	const outtype = "application/json"

	s.Use(CheckContentHeaderMiddleware(intype))
	s.Use(CheckAcceptRequestHeaderMiddleware(outtype))
	s.Use(SetContentTypeMiddleware(outtype))

	s.HandleFunc("/workflows/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WorkflowPostHandler(w, r, componentClient)
	})).Methods(http.MethodPost)

	s.HandleFunc("/workflows/", WorkflowListHandler(componentClient)).Methods(http.MethodGet)
	s.HandleFunc("/workflows/{id}", WorkflowGetHandler(componentClient)).Methods(http.MethodGet)
	s.HandleFunc("/workflows/{id}/{version}", WorkflowGetHandler(componentClient)).Methods(http.MethodGet)
	s.HandleFunc("/workflows/{id}", WorkflowPutHandler(componentClient)).Methods(http.MethodPut)
	s.HandleFunc("/workflows/{id}", WorkflowPatchHandler(componentClient)).Methods(http.MethodPatch)
	s.HandleFunc("/workflows/{id}/versions/", WorkflowVersionListHandler(componentClient)).Methods(http.MethodGet)
	s.HandleFunc("/workflows/{id}/{version}", WorkflowDeleteHandler(componentClient)).Methods(http.MethodDelete)
}

func WorkflowListHandler(componentClient storage.ComponentClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ComponentLikeListHandler(w, r, componentClient, "workflow")
	})
}

func WorkflowVersionListHandler(client storage.ComponentClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ComponentVersionLikeListHandler(w, r, client, "workflow")
	})
}

func WorkflowGetHandler(componentClient storage.ComponentClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var uid interface{}
		id, err := getIdFromMuxerPath(r)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, err.Error(), ""}, "getWorkflow")
			return
		}
		version, err := getVersionNoFromMuxerPath(r)
		switch version {
		case -2:
			WriteErrorResponse(w, APIError{http.StatusBadRequest, err.Error(), ""}, "getWorkflow")
			return
		case -1:
			uid = id
		default:
			uid = models.CRefVersion{Uid: id, Version: version}
		}

		wf, err := componentClient.GetWorkflow(r.Context(), uid)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusInternalServerError, "error retrieving workflow", err.Error()}, "getWorkflow")
			return
		}

		WriteResponse(w, http.StatusOK, nil, wf, "workflow")
	})
}

func WorkflowDeleteHandler(componentClient storage.ComponentClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := getIdFromMuxerPath(r)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, err.Error(), ""}, "deleteWorkflow")
			return
		}
		version, err := getVersionNoFromMuxerPath(r)
		if version == -2 {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, err.Error(), ""}, "deleteWorkflow")
			return
		}
		uid := models.CRefVersion{Uid: id, Version: version}

		crefver, err := componentClient.DeleteDocument(r.Context(), storage.WorkflowKind, uid)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusInternalServerError, "error deleting workflow", err.Error()}, "deleteWorkflow")
			return
		}
		if crefver.IsZero() {
			WriteErrorResponse(w, APIError{http.StatusNotFound, "document not found", uid.String()}, "deleteWorkflow")
		}

		WriteResponse(w, http.StatusNoContent, nil, uid, "deleteWorkflow")
	})
}

func WorkflowPutHandler(componentClient storage.ComponentClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := models.WorkflowPostRequest{}
		if err := ReadBody(r, &request); err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "cannot read request", err.Error()}, "putWorkflow")
			return
		}

		id, err := uuid.Parse(mux.Vars(r)["id"])
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "could not parse {id} parameter in workflow query", ""}, "putWorkflow")
			return
		}

		if id != uuid.UUID(request.Workflow.Uid) {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "put request id-parameter does not match workflow uid", ""}, "putWorkflow")
			return

		}

		err = PutWorkflow(r.Context(), componentClient, request.Workflow)
		if err != nil {
			log.Error(errors.Wrapf(err, "cannot put %s", request.Workflow.Uid.String()).Error())
			WriteErrorResponse(w, APIError{http.StatusInternalServerError, "could not handle put request", ""}, "putWorkflow")
			return
		}

		// return empty body
		WriteResponse(w, http.StatusNoContent, map[string]string{"Location": path.Join(r.URL.RequestURI())}, nil, "putWorkflow")
	})
}

func WorkflowPatchHandler(componentClient storage.ComponentClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := models.WorkflowPostRequest{}
		if err := ReadBody(r, &request); err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "cannot read request", err.Error()}, "patchWorkflow")
			return
		}

		id, err := getIdFromMuxerPath(r)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, err.Error(), ""}, "patchWorkflow")
			return
		}
		if id != request.Workflow.Uid {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "patch request id-parameter does not match workflow uid", ""}, "patchWorkflow")
			return
		}

		wf, err := PatchWorkflow(r.Context(), componentClient, request.Workflow)
		if err != nil {
			if errors.Is(err, storage.ErrNewerDocumentExists) {
				WriteErrorResponse(w, APIError{http.StatusConflict, err.Error(), ""}, "patchWorkflow")
			}
			WriteErrorResponse(w, APIError{http.StatusInternalServerError, "could not handle patch request", ""}, "patchWorkflow")
			return
		}

		// return updated object
		WriteResponse(w, http.StatusOK, map[string]string{"Location": path.Join(r.URL.RequestURI())}, wf, "patchWorkflow")
	})
}

func WorkflowPostHandler(w http.ResponseWriter, r *http.Request, client storage.ComponentClient) {
	var request models.WorkflowPostRequest
	if err := ReadBody(r, &request); err != nil {
		WriteErrorResponse(w, APIError{http.StatusBadRequest, "cannot read request", err.Error()}, "postWorkflow")
		return
	}

	workflow, err := InitializeWorkflow(r.Context(), request.Workflow)
	if err != nil {
		log.Error(errors.Wrapf(err, "cannot create workflow").Error())

		// If this request cannot be served, there is very likely an issue with
		// the upstream services.
		WriteErrorResponse(w, APIError{http.StatusBadRequest, "error parsing request body", err.Error()}, "postWorkflow")
		if err != nil {
			log.Errorf("cannot write POST componentlike error response: %v", err)
		}
	}

	uid, err := StoreWorkflow(r.Context(), client, workflow)

	if err != nil {
		log.Error(errors.Wrapf(err, "cannot create workflow").Error())

		WriteErrorResponse(w, APIError{http.StatusInternalServerError, "error listing workflows", err.Error()}, "postWorkflow")
		return
	}

	location := map[string]string{"Location": path.Join(r.URL.RequestURI(), uid.String())}
	WriteResponse(w, http.StatusCreated, location, workflow, "workflow")
}

func InitializeWorkflow(ctx context.Context, workflow models.Workflow) (models.Workflow, error) {
	if err := InitializeMetadata(ctx, &workflow.Metadata); err != nil {
		return models.Workflow{}, errors.Wrap(err, "cannot initialize workflow")
	}

	return workflow, nil
}

func StoreWorkflow(ctx context.Context, client storage.ComponentClient, workflow models.Workflow) (models.ComponentReference, error) {
	if workflow.Metadata.Uid.IsZero() {
		return CRef(uuid.Nil), fmt.Errorf("cannot store a component without uid set")
	}

	err := client.CreateWorkflow(ctx, workflow)
	if err != nil {
		// don't leak an id if the create didn't go through
		return CRef(uuid.Nil), err
	}

	return workflow.Metadata.Uid, nil
}

func PutWorkflow(ctx context.Context, client storage.ComponentClient, workflow models.Workflow) error {
	if workflow.Metadata.Uid.IsZero() {
		return fmt.Errorf("cannot put a component without uid set")
	}

	TouchMetadata(ctx, &workflow.Metadata)
	err := client.PutWorkflow(ctx, workflow)
	if err != nil {
		// don't leak data if put didn't go through
		return errors.Wrap(err, "storage could not put component")
	}

	// put returns a 204 empty body
	return nil
}

func PatchWorkflow(ctx context.Context, client storage.ComponentClient, workflow models.Workflow) (models.Workflow, error) {
	oldTimestamp := workflow.Metadata.Timestamp
	TouchMetadata(ctx, &workflow.Metadata)
	wf, err := client.PatchWorkflow(ctx, workflow, oldTimestamp)
	if err != nil {
		if uerr := errors.Unwrap(err); errors.Is(uerr, storage.ErrNewerDocumentExists) {
			return models.Workflow{}, uerr
		}
		return models.Workflow{}, errors.Wrap(err, "storage could not patch component")
	}

	return wf, nil
}
