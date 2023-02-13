package rest

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"net/http"
	"os"
	"path/filepath"

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
	s.HandleFunc("/workspaces/", WorkspacesCreate()).Methods(http.MethodPost)
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

func WorkspacesCreate() http.HandlerFunc {

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
				Name: "my-new-namespace",
			},
		}

		namespace, err := clientset.CoreV1().Namespaces().Create(context.Background(), nsName, metav1.CreateOptions{})
		if err != nil {
			WriteResponse(w, http.StatusInternalServerError, nil, struct {
				Error error
			}{Error: err}, "workspace")
		}

		WriteResponse(w, http.StatusOK, nil, struct {
			NamespaceName string
		}{NamespaceName: namespace.Name}, "workspace")
	})
}
