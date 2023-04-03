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
	s.HandleFunc("/workspaces/", WorkspacesUpdateHandler(k8sclient, namespace, wsClient)).Methods(http.MethodPut)
	s.HandleFunc("/workspaces/", WorkspacesDeleteHandler(k8sclient, namespace, wsClient)).Methods(http.MethodDelete)
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
				roles = append(roles, "ws-collaborator")
			}
			if ws.UserHasAdminAccess(usr) {
				roles = append(roles, "ws-owner")
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
		var creationData workspace.InputData
		err := json.NewDecoder(r.Body).Decode(&creationData)
		if err != nil {
			WriteResponse(w, http.StatusInternalServerError, nil, struct {
				Error string
			}{Error: fmt.Sprintf("error decoding the input data: %v\n", err)}, "workspace")
		}

		wsCreation := models.WorkspacesInputToCreateData(creationData, namespace)
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

func WorkspacesUpdateHandler(k8sclient kubernetes.Interface, namespace string, wsClient workspace.WorkspaceClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var updateData workspace.InputData
		err := json.NewDecoder(r.Body).Decode(&updateData)
		if err != nil {
			WriteResponse(w, http.StatusInternalServerError, nil, struct {
				Error string
			}{Error: fmt.Sprintf("error decoding the input data: %v\n", err)}, "workspace")
		}

		wsUpdate := models.WorkspacesInputToUpdateData(updateData, namespace)
		msg, err := wsClient.Update(k8sclient, wsUpdate)
		if err != nil {
			WriteResponse(w, http.StatusInternalServerError, nil, struct {
				Error string
			}{Error: fmt.Sprintf("error updating workspace: %v\n", err)}, "workspace")
		}

		WriteResponse(w, http.StatusOK, nil, struct {
			Workspace string
		}{
			Workspace: fmt.Sprintf("Success: %s", msg),
		}, "workspace")
	})
}

func WorkspacesDeleteHandler(k8sclient kubernetes.Interface, namespace string, wsClient workspace.WorkspaceClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var deleteData workspace.InputData
		err := json.NewDecoder(r.Body).Decode(&deleteData)
		if err != nil {
			WriteResponse(w, http.StatusInternalServerError, nil, struct {
				Error string
			}{Error: fmt.Sprintf("error decoding the input data: %v\n", err)}, "workspace")

		}
		msg, err := wsClient.Delete(k8sclient, namespace, deleteData.Name)
		if err != nil {
			WriteResponse(w, http.StatusInternalServerError, nil, struct {
				Error string
			}{Error: fmt.Sprintf("error deleteing: %v\n", err)}, "workspace")
		}
		WriteResponse(w, http.StatusOK, nil, struct {
			Workspace string
		}{
			Workspace: fmt.Sprintf("Success: %s", msg),
		}, "workspace")
	})
}
