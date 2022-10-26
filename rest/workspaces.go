package rest

import (
	"net/http"

	"github.com/equinor/flowify-workflows-server/pkg/workspace"
	"github.com/equinor/flowify-workflows-server/user"
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
		wss := GetWorkspaceAccess(r.Context())
		lst := []workspace.WorkspaceGetRequest{}
		usr := user.GetUser(r.Context())
		for _, ws := range wss {
			wsgr := workspace.WorkspaceGetRequest{Name: ws.Name, Description: ws.Description}
			roles := []string{}
			if ws.UserHasAccess(usr) {
				roles = append(roles, "user")
			}
			if ws.UserHasAdminAccess(usr) {
				roles = append(roles, "admin")
			}
			wsgr.Roles = roles
			lst = append(lst, wsgr)
		}

		WriteResponse(w, http.StatusOK, nil, struct {
			Items []workspace.WorkspaceGetRequest `json:"items"`
		}{Items: lst}, "workspace")
	})
}
