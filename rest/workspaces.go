package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gorilla/mux"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/equinor/flowify-workflows-server/pkg/workspace"
	"github.com/equinor/flowify-workflows-server/user"
)

func RegisterWorkspaceRoutes(r *mux.Route) {
	s := r.Subrouter()

	const intype = "application/json"
	const outtype = "application/json"

	s.Use(CheckContentHeaderMiddleware(intype))
	s.Use(CheckAcceptRequestHeaderMiddleware(outtype))
	s.Use(SetContentTypeMiddleware(outtype))

	s.HandleFunc("/workspaces/", WorkspacesListHandler()).Methods(http.MethodGet)
	s.HandleFunc("/workspaces/", WorkspacesCreateHandler()).Methods(http.MethodPost)
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

type WorkspaceCreateInputData struct {
	Name  string
	Roles []string
}

func WorkspacesCreateHandler() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var creationData WorkspaceCreateInputData
		err := json.NewDecoder(r.Body).Decode(&creationData)
		if err != nil {
			WriteResponse(w, http.StatusInternalServerError, nil, struct {
				Error string
			}{Error: fmt.Sprintf("error decoding the input data: %v\n", err)}, "workspace")
		}

		userHomeDir, err := os.UserHomeDir()
		if err != nil {
			WriteResponse(w, http.StatusInternalServerError, nil, struct {
				Error string
			}{Error: fmt.Sprintf("error getting user home dir: %v\n", err)}, "workspace")
		}
		kubeConfigPath := filepath.Join(userHomeDir, ".kube", "config")

		config, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
		if err != nil {
			WriteResponse(w, http.StatusInternalServerError, nil, struct {
				Error string
			}{Error: fmt.Sprintf("Error %s", err)}, "workspace")
		}

		clientset, err := kubernetes.NewForConfig(config)
		if err != nil {
			WriteResponse(w, http.StatusInternalServerError, nil, nil, "workspace")
		}

		nsName := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: creationData.Name,
			},
		}
		_, err = clientset.CoreV1().Namespaces().Create(context.Background(), nsName, metav1.CreateOptions{})
		if err != nil {
			WriteResponse(w, http.StatusInternalServerError, nil, struct {
				Error string
			}{Error: fmt.Sprintf("Error %s", err)}, "workspace")
		}

		var strBuffer bytes.Buffer
		strBuffer.WriteString("[")
		for _, role := range creationData.Roles {
			strBuffer.WriteString("\"")
			strBuffer.WriteString(role)
			strBuffer.WriteString("\", ")
		}
		roles := strBuffer.String()
		roles = roles[:len(roles)-2] + "]"

		cm := corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      creationData.Name,
				Namespace: "argo",
				Labels: map[string]string{
					"app.kubernetes.io/component": "workspace-config",
					"app.kubernetes.io/part-of":   "flowify",
				},
			},
			Data: map[string]string{"roles": roles, "projectName": creationData.Name},
		}

		CMOpt := metav1.CreateOptions{}
		_, err = clientset.CoreV1().ConfigMaps("argo").Create(r.Context(), &cm, CMOpt)
		if err != nil {
			WriteResponse(w, http.StatusInternalServerError, nil, struct {
				Error string
			}{Error: fmt.Sprintf("error creating configMap: %v\n", err)}, "workspace")
		}

		WriteResponse(w, http.StatusCreated, nil, struct {
			Workspace string
		}{
			Workspace: fmt.Sprintf("The Workspace has been created %s", creationData.Name),
		}, "workspace")
	})
}
