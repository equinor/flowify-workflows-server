package rest

import (
	"net/http"

	"github.com/equinor/flowify-workflows-server/pkg/workspace"
	"github.com/gorilla/mux"
)

func RegisterWorkspaceRoutes(r *mux.Route) {
	s := r.Subrouter()

	const intype = "application/json"
	const outtype = "application/json"

	s.Use(CheckContentHeaderMiddleware(intype))
	s.Use(CheckAcceptRequestHeaderMiddleware(outtype))
	s.Use(SetContentTypeMiddleware(outtype))

	s.HandleFunc("/workspaces/", WorkspacesListHandler()).Methods(http.MethodGet)
}

func WorkspacesListHandler() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws := GetWorkspaceAccess(r.Context())

		WriteResponse(w, http.StatusOK, nil, struct {
			Items []workspace.Workspace `json:"items"`
		}{Items: ws}, "workspace")
	})
}
