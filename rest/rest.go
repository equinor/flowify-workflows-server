package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	argoclient "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
	"github.com/equinor/flowify-workflows-server/auth"
	"github.com/equinor/flowify-workflows-server/models"
	"github.com/equinor/flowify-workflows-server/pkg/secret"
	"github.com/equinor/flowify-workflows-server/pkg/workspace"
	"github.com/equinor/flowify-workflows-server/storage"
	"github.com/equinor/flowify-workflows-server/user"
	userpkg "github.com/equinor/flowify-workflows-server/user"

	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"k8s.io/client-go/kubernetes"
)

type APIError struct {
	Code    int    `json:"code"`
	Summary string `json:"summary"`
	Detail  string `json:"detail"`
}

const (
	// tries to validate all input according to spec
	validateInput bool = true

	// tries to validate output
	validateOutput bool = false
)

// try to read a json-marshalled type from the body of a request
func ReadBody(r *http.Request, item any) error {
	buf := new(bytes.Buffer)
	if err := func() error { // scope for defer and err
		_, err := buf.ReadFrom(r.Body)
		defer r.Body.Close()
		return err
	}(); err != nil {
		return err
	}

	if validateInput {
		itemType := reflect.ValueOf(item).Type()
		if err := models.ValidateDocument(buf.Bytes(), itemType); err != nil {
			switch err {
			case models.ErrNoSchemaFound:
				// not an error here, just continue
			default:
				return errors.Wrapf(err, "cannot unmarshal item %s", itemType)
			}
		}
	}

	if err := json.Unmarshal(buf.Bytes(), item); err != nil {
		return err
	}
	return nil
}

func WriteResponse(w http.ResponseWriter, status int, headers map[string]string, body any, tag string) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		WriteErrorResponse(w, APIError{http.StatusInternalServerError, fmt.Sprintf("cannot marshal response object %s", tag), err.Error()}, tag)
		return
	}

	if validateOutput && body != nil {
		// ALSO VALIDATE OUTPUT ACCORDING TO SPEC IF CONST IS SET IN PACKAGE
		itemType := reflect.ValueOf(body).Type()
		if err := models.ValidateDocument(bodyBytes, itemType); err != nil {
			switch err {
			case models.ErrNoSchemaFound:
				// not an error here, just continue
			default:
				WriteErrorResponse(w, APIError{http.StatusInternalServerError, fmt.Sprintf("%s does not validate", tag), err.Error()}, tag)
				return
			}
		}
	}

	// add headers
	for k, v := range headers {
		w.Header().Add(k, v)
	}
	w.WriteHeader(status)
	w.Write(bodyBytes)
}

// unwrap the return code from the error and write a normal response
func WriteErrorResponse(w http.ResponseWriter, apierr APIError, tag string) {
	WriteResponse(w, apierr.Code, nil, apierr, tag)
}

func RegisterRoutes(r *mux.Route,
	componentClient storage.ComponentClient,
	volumeClient storage.VolumeClient,
	secretClient secret.SecretClient,
	argoclient argoclient.Interface,
	k8sclient kubernetes.Interface,
	sec auth.AuthenticationClient,
	authz auth.AuthorizationClient,
	wsclient workspace.WorkspaceClient) {

	subrouter := r.Subrouter()

	// require authenticated context
	subrouter.Use(NewAuthenticationMiddleware(sec))
	subrouter.Use(NewAuthorizationContext(wsclient))

	RegisterOpenApiRoutes(subrouter.PathPrefix("/spec"))
	RegisterUserInfoRoutes(subrouter.PathPrefix(""))
	RegisterComponentRoutes(subrouter.PathPrefix(""), componentClient)
	RegisterWorkspaceRoutes(subrouter.PathPrefix(""))

	// the following handlers below will use the authorized context's WorkspaceAccess
	RegisterWorkflowRoutes(subrouter.PathPrefix(""), componentClient)
	RegisterJobRoutes(subrouter.PathPrefix(""), componentClient, argoclient)
	RegisterSecretRoutes(subrouter.PathPrefix(""), secretClient, authz)
	RegisterVolumeRoutes(subrouter.PathPrefix(""), volumeClient)

}

func RegisterOpenApiRoutes(r *mux.Route) {
	subrouter := r.Subrouter()
	sf := http.FileServer(http.FS(models.StaticSpec))
	subrouter.PathPrefix("/").Handler(http.StripPrefix("/api/v1/", sf))
}

func SetContentTypeMiddleware(mediatype string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Content-Type", mediatype)
			next.ServeHTTP(w, r)
		})
	}
}

func CheckContentHeaderMiddleware(contentType string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !(r.Method == "PUT" || r.Method == "POST" || r.Method == "PATCH") {
				next.ServeHTTP(w, r)
				return
			}

			ct := r.Header.Get("Content-Type")
			if i := strings.IndexRune(ct, ';'); i != -1 {
				ct = ct[0:i]
			}

			if ct == contentType {
				next.ServeHTTP(w, r)
				return
			}

			WriteErrorResponse(w, APIError{http.StatusUnsupportedMediaType, "Unsupported content type", fmt.Sprintf("Unsupported Content-Type header (%q): expecting %q", ct, contentType)}, "middleware")
		})
	}
}

func CheckAcceptRequestHeaderMiddleware(mediatype string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			val := r.Header.Get("Accept")

			if val != "" && val != "*/*" && val != mediatype {
				WriteErrorResponse(w, APIError{http.StatusNotAcceptable, "Accept media type not acceptable", fmt.Sprintf("requested Accept type %s is not available, expecting %s", val, mediatype)}, "middleware")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// This ensures that the context is authenticated, with the appropriate User-tokens
func NewAuthenticationMiddleware(sec auth.AuthenticationClient) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, err := sec.Authenticate(r)
			if err != nil {
				WriteErrorResponse(w, APIError{http.StatusBadRequest, "could not authenticate", err.Error()}, "authmiddleware")
				return
			}

			// continue with authenticated context
			next.ServeHTTP(w, r.WithContext(userpkg.UserContext(user, r.Context())))
		})
	}
}

// This injects the workspace into the context and can be used to authorize users further down the stack
func NewAuthorizationContext(wsclient workspace.WorkspaceClient) mux.MiddlewareFunc {

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ws, err := wsclient.ListWorkspaces(r.Context(), user.GetUser(r.Context()))
			if err != nil {
				WriteErrorResponse(w, APIError{http.StatusInternalServerError, "error retrieving component", err.Error()}, "authzmiddleware")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), workspace.WorkspaceKey, ws)))
		})
	}
}

func GetWorkspaceAccess(ctx context.Context) []workspace.Workspace {
	val := ctx.Value(workspace.WorkspaceKey)

	if val == nil {
		return []workspace.Workspace{}
	} else {
		return val.([]workspace.Workspace)
	}
}
