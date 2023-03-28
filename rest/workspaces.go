package rest

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"k8s.io/client-go/kubernetes"
	"net/http"

	"github.com/equinor/flowify-workflows-server/models"
	"github.com/equinor/flowify-workflows-server/pkg/workspace"
	"github.com/equinor/flowify-workflows-server/user"
)

func RegisterWorkspaceRoutes(r *mux.Route, k8sclient kubernetes.Interface, namespace string, wsClient workspace.WorkspaceClient) {
	s := r.Subrouter()

	const intype = "application/json"
	const outtype = "application/json"

	s.Use(CheckContentHeaderMiddleware(intype))
	s.Use(CheckAcceptRequestHeaderMiddleware(outtype))
	s.Use(SetContentTypeMiddleware(outtype))

	s.HandleFunc("/workspaces/", WorkspacesListHandler()).Methods(http.MethodGet)
	s.HandleFunc("/workspaces/", WorkspacesCreateHandler(k8sclient, namespace, wsClient)).Methods(http.MethodPost)
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

func WorkspacesCreateHandler(k8sclient kubernetes.Interface, namespace string, wsClient workspace.WorkspaceClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var creationData workspace.CreateInputData
		err := json.NewDecoder(r.Body).Decode(&creationData)
		if err != nil {
			WriteResponse(w, http.StatusInternalServerError, nil, struct {
				Error string
			}{Error: fmt.Sprintf("error decoding the input data: %v\n", err)}, "workspace")
		}

		wsCreation := models.WorkspacesInputToCreationData(creationData, namespace)
		msg, err := wsClient.Create(k8sclient, wsCreation)
		if err != nil {
			WriteResponse(w, http.StatusInternalServerError, nil, struct {
				Error string
			}{Error: fmt.Sprintf("error creating workspace: %v\n", err)}, "workspace")
		}

		WriteResponse(w, http.StatusCreated, nil, struct {
			Workspace string
		}{
			Workspace: fmt.Sprintf("Success: %s", msg),
		}, "workspace")
	})
}
