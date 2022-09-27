package rest

import (
	"context"
	"net/http"
	"os"

	"github.com/equinor/flowify-workflows-server/pkg/workspace"
	"github.com/equinor/flowify-workflows-server/user"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
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

func GetWorkspaceAccess(ctx context.Context) []workspace.Workspace {
	val := ctx.Value(workspace.WorkspaceKey)

	if val == nil {
		return []workspace.Workspace{}
	} else {
		return val.([]workspace.Workspace)
	}
}

// This injects the workspace into the context and can be used to authorize users further down the stack
func NewAuthorizationContext(k8sclient kubernetes.Interface) mux.MiddlewareFunc {

	namespaceEnvVar := "FLOWIFY_K8S_NAMESPACE"
	val, exists := os.LookupEnv(namespaceEnvVar)

	if !exists {
		log.Warning("Environment variable '" + namespaceEnvVar + "' is not set. Defaulting to 'test'")
		val = "test"
	}

	client := workspace.NewWorkspaceClient(k8sclient, val)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ws, err := client.ListWorkspaces(r.Context(), user.GetUser(r.Context()))
			if err != nil {
				WriteErrorResponse(w, APIError{http.StatusInternalServerError, "error retrieving component", err.Error()}, "authzmiddleware")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), workspace.WorkspaceKey, ws)))
		})
	}
}

func WorkspacesListHandler() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws := GetWorkspaceAccess(r.Context())

		WriteResponse(w, http.StatusOK, nil, struct {
			Items []workspace.Workspace `json:"items"`
		}{Items: ws}, "workspace")
	})
}
