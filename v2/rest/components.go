package rest

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/equinor/flowify-workflows-server/v2/models"
	"github.com/equinor/flowify-workflows-server/v2/storage"
	"github.com/equinor/flowify-workflows-server/v2/user"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

func getIdFromMuxerPath(r *http.Request) (models.ComponentReference, error) {
	idString, ok := mux.Vars(r)["id"]
	if !ok {
		return models.ComponentReference{}, errors.Errorf("no {id} parameter in component query")
	}
	id, err := uuid.Parse(idString)
	if err != nil {
		return models.ComponentReference{}, errors.Wrapf(err, "error parsing id parameter")
	}
	return models.ComponentReference(id), nil
}

func getVersionNoFromMuxerPath(r *http.Request) (models.VersionNumber, error) {
	// if version parameter not in query, return error and VersionNumber equal to -1
	// if cannot convert version to Versionnumber, return error and VersionNumber equal to -2
	verString, ok := mux.Vars(r)["version"]
	if !ok {
		return models.VersionNumber(-1), errors.Errorf("no {version} parameter in component query")
	}
	version, err := strconv.Atoi((verString))
	if err != nil {
		return models.VersionNumber(-2), errors.Wrapf(err, "error parsing version parameter")
	}
	return models.VersionNumber(version), nil
}

type ComponentPostRequest struct {
	Component models.Component     `json:"component"`
	Options   ComponentPostOptions `json:"options,omitempty"`
}

type ComponentPostOptions struct {
	// TODO: Added for forward compatibility
}

func RegisterComponentRoutes(r *mux.Route, componentClient storage.ComponentClient) {
	subrouter := r.Subrouter()

	const intype = "application/json"
	const outtype = "application/json"

	subrouter.Use(CheckContentHeaderMiddleware(intype))
	subrouter.Use(CheckAcceptRequestHeaderMiddleware(outtype))
	subrouter.Use(SetContentTypeMiddleware(outtype))

	subrouter.HandleFunc("/components/", ComponentListHandler(componentClient)).Methods(http.MethodGet)
	subrouter.HandleFunc("/components/", ComponentPostHandler(componentClient)).Methods(http.MethodPost)
	subrouter.HandleFunc("/components/{id}", ComponentGetHandler(componentClient)).Methods(http.MethodGet)
	subrouter.HandleFunc("/components/{id}/{version}", ComponentGetHandler(componentClient)).Methods(http.MethodGet)
	subrouter.HandleFunc("/components/{id}", ComponentPutHandler(componentClient)).Methods(http.MethodPut)
	subrouter.HandleFunc("/components/{id}", ComponentPatchHandler(componentClient)).Methods(http.MethodPatch)
	subrouter.HandleFunc("/components/{id}/versions/", ComponentVersionListHandler(componentClient)).Methods(http.MethodGet)
	subrouter.HandleFunc("/components/{id}/{version}", ComponentDeleteHandler(componentClient)).Methods(http.MethodDelete)
}

func ComponentListHandler(componentClient storage.ComponentClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ComponentLikeListHandler(w, r, componentClient, "component")
	})
}

func ComponentVersionListHandler(client storage.ComponentClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ComponentVersionLikeListHandler(w, r, client, "component")
	})
}

func ComponentVersionLikeListHandler(w http.ResponseWriter, r *http.Request, client storage.ComponentClient, kind string) {
	tag := fmt.Sprintf("get%sVersions", strings.Title(kind))
	pagination, err := parsePaginationsOrDefault(r.URL.Query()["limit"], r.URL.Query()["offset"])
	if err != nil {
		WriteErrorResponse(w, APIError{http.StatusBadRequest, "error parsing query parameters", err.Error()}, tag)
		return
	}
	id, err := getIdFromMuxerPath(r)
	if err != nil {
		WriteErrorResponse(w, APIError{http.StatusBadRequest, err.Error(), ""}, tag)
		return
	}

	var versionLikes interface{}

	switch kind {
	case "component":
		versionLikes, err = client.ListComponentVersionsMetadata(r.Context(), models.ComponentReference(id), pagination, r.URL.Query()["sort"])
	case "workflow":
		versionLikes, err = client.ListWorkflowVersionsMetadata(r.Context(), models.ComponentReference(id), pagination, r.URL.Query()["sort"])
	default:
		WriteErrorResponse(w, APIError{http.StatusInternalServerError, "error geting component versions", fmt.Sprintf("Versions list method not implemented for kind: %s", kind)}, tag)
	}

	if err != nil {
		WriteErrorResponse(w, APIError{http.StatusInternalServerError, "error retrieving component versions", err.Error()}, tag)
		return
	}

	WriteResponse(w, http.StatusOK, nil, versionLikes, tag)
}

func ComponentGetHandler(componentClient storage.ComponentClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid := models.CRefVersion{}
		id, err := getIdFromMuxerPath(r)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, err.Error(), ""}, "getComponent")
			return
		}
		uid.Uid = id
		version, err := getVersionNoFromMuxerPath(r)
		if version == -2 {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, err.Error(), ""}, "getComponent")
			return
		}
		if version > -1 {
			uid.Version = version
		}

		cmp, err := componentClient.GetComponent(r.Context(), uid)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusInternalServerError, "error retrieving component", err.Error()},
				"getComponent")
			return
		}

		WriteResponse(w, http.StatusOK, nil, cmp, "getComponent")
	})
}

func ComponentDeleteHandler(componentClient storage.ComponentClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := getIdFromMuxerPath(r)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, err.Error(), ""}, "deleteComponent")
			return
		}
		version, err := getVersionNoFromMuxerPath(r)
		if version == -2 {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, err.Error(), ""}, "deleteComponent")
			return
		}
		uid := models.CRefVersion{Uid: id, Version: version}

		crefver, err := componentClient.DeleteDocument(r.Context(), storage.ComponentKind, uid)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusInternalServerError, "error deleting component", err.Error()}, "deleteComponent")
			return
		}
		if crefver.IsZero() {
			WriteErrorResponse(w, APIError{http.StatusNotFound, "document not found", uid.String()}, "deleteComponent")
		}

		WriteResponse(w, http.StatusNoContent, nil, uid, "deleteComponent")
	})
}

func parsePaginationOrDefault(limitString string, offsetString string) (storage.Pagination, error) {
	limit := 10
	offset := 0
	if limitString != "" {
		l, err := strconv.Atoi(limitString)
		if err != nil {
			return storage.Pagination{}, errors.Wrapf(err, "could not parse 'limit' query parameter into non-negative integer")
		}
		if l < 0 {
			return storage.Pagination{}, fmt.Errorf("could not parse 'limit' query parameter (%s) into non-negative integer", limitString)
		}

		limit = l
	}
	if offsetString != "" {
		o, err := strconv.Atoi(offsetString)
		if err != nil {
			return storage.Pagination{}, errors.Wrapf(err, "could not parse 'offset' query parameter into non-negative integer")
		}
		if o < 0 {
			return storage.Pagination{}, fmt.Errorf("could not parse 'offset' query parameter (%s) into non-negative integer", offsetString)
		}
		offset = o
	}

	return storage.Pagination{Limit: limit, Skip: offset}, nil
}

func parsePaginationsOrDefault(limits []string, offsets []string) (storage.Pagination, error) {
	var limit string
	if len(limits) > 0 {
		limit = limits[0]
	}
	var offset string
	if len(offsets) > 0 {
		offset = offsets[0]
	}

	return parsePaginationOrDefault(limit, offset)
}

func ComponentLikeListHandler(w http.ResponseWriter, r *http.Request, client storage.ComponentClient, kind string) {
	pagination, err := parsePaginationsOrDefault(r.URL.Query()["limit"], r.URL.Query()["offset"])
	if err != nil {
		WriteErrorResponse(w, APIError{http.StatusBadRequest, "error parsing query parameters", err.Error()}, kind+"ListHandler")
		return
	}

	var componentLikes interface{}
	switch kind {
	case "component":
		componentLikes, err = client.ListComponentsMetadata(r.Context(), pagination, r.URL.Query()["filter"], r.URL.Query()["sort"])
	case "workflow":
		componentLikes, err = client.ListWorkflowsMetadata(r.Context(), pagination, r.URL.Query()["filter"], r.URL.Query()["sort"])
	case "job":
		componentLikes, err = client.ListJobsMetadata(r.Context(), pagination, r.URL.Query()["filter"], r.URL.Query()["sort"])
	}

	if err != nil {
		WriteErrorResponse(w, APIError{http.StatusInternalServerError, fmt.Sprintf("error listing %ss", kind), err.Error()}, kind)
		return
	}

	WriteResponse(w, http.StatusOK, nil, componentLikes, kind)
}

func ComponentPostHandler(componentClient storage.ComponentClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ComponentCreateHandler(w, r, componentClient)
	})
}

func ComponentCreateHandler(w http.ResponseWriter, r *http.Request, client storage.ComponentClient) {
	var request models.ComponentPostRequest
	if err := ReadBody(r, &request); err != nil {
		WriteErrorResponse(w, APIError{http.StatusBadRequest, "cannot read request", err.Error()}, "createComponent")
		return
	}

	component, err := InitializeComponent(r.Context(), request.Component)
	if err != nil {
		log.Error(errors.Wrapf(err, "cannot create component").Error())
		WriteErrorResponse(w, APIError{http.StatusInternalServerError, "cannot create component", err.Error()}, "createComponent")
		return
	}

	uid, err := StoreComponent(r.Context(), client, component)

	if err != nil {
		log.Error(errors.Wrapf(err, "cannot create component").Error())
		WriteErrorResponse(w, APIError{http.StatusInternalServerError, "cannot store component", err.Error()}, "createComponent")
		return
	}

	// return updated object
	location := map[string]string{"Location": path.Join(r.URL.RequestURI(), uid.String())}
	WriteResponse(w, http.StatusCreated, location, component, "Component")
}

func ComponentPutHandler(componentClient storage.ComponentClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := models.ComponentPostRequest{}
		if err := ReadBody(r, &request); err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "cannot read request", err.Error()}, "putComponent")
			return
		}

		id, err := uuid.Parse(mux.Vars(r)["id"])
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "could not parse {id} parameter in component query", ""}, "putComponent")
			return
		}

		if id != uuid.UUID(request.Component.Uid) {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "put request id-parameter does not match component uid", ""}, "putComponent")
			return

		}

		err = PutComponent(r.Context(), componentClient, request.Component)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusInternalServerError, "could not handle put request", ""}, "putComponent")
			return
		}

		// return empty body
		WriteResponse(w, http.StatusNoContent, map[string]string{"Location": path.Join(r.URL.RequestURI())}, nil, "putComponent")
	})
}

func ComponentPatchHandler(componentClient storage.ComponentClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := models.ComponentPostRequest{}
		if err := ReadBody(r, &request); err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "cannot read request", err.Error()}, "patchComponent")
			return
		}

		id, err := getIdFromMuxerPath(r)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, err.Error(), ""}, "patchComponent")
			return
		}
		if id != request.Component.Uid {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "patch request id-parameter does not match component uid", ""}, "patchComponent")
			return
		}

		cmp, err := PatchComponent(r.Context(), componentClient, request.Component)
		if err != nil {
			if errors.Is(err, storage.ErrNewerDocumentExists) {
				WriteErrorResponse(w, APIError{http.StatusConflict, err.Error(), ""}, "patchComponent")
				return
			}
			WriteErrorResponse(w, APIError{http.StatusInternalServerError, "could not handle patch request", ""}, "patchComponent")
			return
		}

		// return updated object
		WriteResponse(w, http.StatusOK, map[string]string{"Location": path.Join(r.URL.RequestURI())}, cmp, "patchComponent")
	})
}

func InitializeComponent(ctx context.Context, component models.Component) (models.Component, error) {
	if err := InitializeMetadata(ctx, &component.Metadata); err != nil {
		return models.Component{}, errors.Wrap(err, "cannot initialize component")
	}

	if component.Implementation == nil {
		return models.Component{}, fmt.Errorf("cannot create a component with nil implementation")
	}

	return component, nil
}

func InitializeMetadata(ctx context.Context, meta *models.Metadata) error {
	if !meta.Uid.IsZero() {
		return fmt.Errorf("cannot initialize Metadata when uid (%s) is set", meta.Uid)
	}

	id := models.NewComponentReference()
	meta.Uid = id
	TouchMetadata(ctx, meta)

	return nil
}

func TouchMetadata(ctx context.Context, meta *models.Metadata) {
	user := user.GetUser(ctx)
	if user == nil {
		log.Warn("updating metadata without user in context")
		return
	}
	meta.ModifiedBy = models.ModifiedBy{Oid: user.GetUid(), Email: user.GetEmail()}
	// in order to be equal to mongo-roundtrip data we need to truncate timestamps
	// 	https://www.mongodb.com/docs/manual/reference/bson-types/#timestamps
	meta.Timestamp = time.Now().In(time.UTC).Truncate(time.Millisecond)
}

func StoreComponent(ctx context.Context, client storage.ComponentClient, component models.Component) (models.ComponentReference, error) {
	if component.Metadata.Uid.IsZero() {
		return [16]byte{}, fmt.Errorf("cannot store a component without uid set")
	}
	if component.Implementation == nil {
		return [16]byte{}, fmt.Errorf("cannot store a component with nil implementation")
	}

	err := client.CreateComponent(ctx, component)
	if err != nil {
		// don't leak an id if the create didn't go through
		return [16]byte{}, err
	}

	return component.Metadata.Uid, nil
}

func PutComponent(ctx context.Context, client storage.ComponentClient, component models.Component) error {
	if component.Metadata.Uid.IsZero() {
		return fmt.Errorf("cannot put a component without uid set")
	}
	TouchMetadata(ctx, &component.Metadata)
	err := client.PutComponent(ctx, component)
	if err != nil {
		// don't leak data if put didn't go through
		return errors.Wrap(err, "storage could not put component")
	}

	// put returns a 204 empty body
	return nil
}

func PatchComponent(ctx context.Context, client storage.ComponentClient, component models.Component) (models.Component, error) {
	// update
	oldTimestamp := component.Metadata.Timestamp
	TouchMetadata(ctx, &component.Metadata)
	cmp, err := client.PatchComponent(ctx, component, oldTimestamp)
	if err != nil {
		if uerr := errors.Unwrap(err); errors.Is(uerr, storage.ErrNewerDocumentExists) {
			return models.Component{}, uerr
		}
		return models.Component{}, errors.Wrap(err, "storage could not patch component")
	}

	return cmp, nil
}
