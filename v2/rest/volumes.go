package rest

import (
	"fmt"
	"net/http"
	"path"

	"github.com/equinor/flowify-workflows-server/models"
	"github.com/equinor/flowify-workflows-server/v2/storage"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

func RegisterVolumeRoutes(r *mux.Route, client storage.VolumeClient) {
	s := r.Subrouter()

	const intype = "application/json"
	const outtype = "application/json"

	s.Use(CheckContentHeaderMiddleware(intype))
	s.Use(CheckAcceptRequestHeaderMiddleware(outtype))
	s.Use(SetContentTypeMiddleware(outtype))

	s.HandleFunc("/volumes/{workspace}/", VolumesListHandler(client)).Methods(http.MethodGet)
	s.HandleFunc("/volumes/{workspace}/", VolumePostHandler(client)).Methods(http.MethodPost)
	s.HandleFunc("/volumes/{workspace}/{id}", VolumeGetHandler(client)).Methods(http.MethodGet)
	s.HandleFunc("/volumes/{workspace}/{id}", VolumePutHandler(client)).Methods(http.MethodPut)
	s.HandleFunc("/volumes/{workspace}/{id}", VolumeDeleteHandler(client)).Methods(http.MethodDelete)
}

func VolumesListHandler(vclient storage.VolumeClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const opId = "listVolumes"
		ws := mux.Vars(r)["workspace"]

		pagination, err := parsePaginationsOrDefault(r.URL.Query()["limit"], r.URL.Query()["offset"])
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "error parsing query parameters", err.Error()}, opId)
			return
		}

		// the endpoint doesnt do ws-filtering, need to inject here
		wsFilter := fmt.Sprintf("workspace[==]=%s", ws)
		list, err := vclient.ListVolumes(r.Context(), pagination, []string{wsFilter}, []string{})
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusInternalServerError, "could not list volumes in workspace", err.Error()}, opId)
			return
		}

		WriteResponse(w, http.StatusOK, nil, list, opId)
	})
}

func VolumeGetHandler(vclient storage.VolumeClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const opId = "getVolume"
		idString := mux.Vars(r)["id"]
		ws := mux.Vars(r)["workspace"]

		id, err := uuid.Parse(idString)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "error parsing id parameter", err.Error()}, opId)
			return
		}

		vol, err := vclient.GetVolume(r.Context(), models.ComponentReference(id))
		if err != nil {
			switch err {
			case storage.ErrNotFound:
				WriteErrorResponse(w, APIError{http.StatusNotFound, "could not get volume", err.Error()}, opId)
			default:
				WriteErrorResponse(w, APIError{http.StatusInternalServerError, "could not get volume", err.Error()}, opId)
			}
			return
		}

		if vol.Workspace != ws {
			WriteErrorResponse(w, APIError{http.StatusInternalServerError, "could not get volume", ""}, opId)
			return
		}

		WriteResponse(w, http.StatusOK, nil, vol, opId)
	})
}

func VolumeDeleteHandler(vclient storage.VolumeClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const opId = "deleteVolume"
		idString := mux.Vars(r)["id"]

		id, err := uuid.Parse(idString)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "error parsing id parameter", err.Error()}, opId)
			return
		}

		err = vclient.DeleteVolume(r.Context(), models.ComponentReference(id))
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusInternalServerError, "could not delete volume", err.Error()}, opId)
			return
		}

		WriteResponse(w, http.StatusOK, nil, nil, opId)
	})
}

func VolumePutHandler(vclient storage.VolumeClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const opId = "putVolume"
		idString := mux.Vars(r)["id"]
		ws := mux.Vars(r)["workspace"]

		id, err := uuid.Parse(idString)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "error parsing id parameter", err.Error()}, opId)
			return
		}

		var request models.FlowifyVolume
		if err := ReadBody(r, &request); err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "cannot read volume put request body", err.Error()}, opId)
			return
		}

		if request.Workspace != ws {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "request workspace does not match path parameter", ""}, opId)
			return
		}

		if request.Uid != models.ComponentReference(id) {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "request id required to match body", ""}, opId)
			return
		}

		err = vclient.PutVolume(r.Context(), request)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "could not put volume", err.Error()}, opId)
			return
		}

		// update
		WriteResponse(w, http.StatusNoContent, nil, nil, opId)
	})
}

func VolumePostHandler(vclient storage.VolumeClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const opId = "postVolume"
		ws := mux.Vars(r)["workspace"]

		var request models.FlowifyVolume
		if err := ReadBody(r, &request); err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "cannot read request", err.Error()}, opId)
			return
		}

		if !request.Uid.IsZero() {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "volume id required to be zero", ""}, opId)
			return
		}

		if request.Workspace != ws {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "volume workspace mismatch", ""}, opId)
			return
		}

		request.Uid = models.NewComponentReference()
		err := vclient.PutVolume(r.Context(), request)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "could not put volume", err.Error()}, opId)
			return
		}

		location := map[string]string{"Location": path.Join(r.URL.RequestURI(), request.Uid.String())}
		WriteResponse(w, http.StatusCreated, location, nil, opId)
	})
}
