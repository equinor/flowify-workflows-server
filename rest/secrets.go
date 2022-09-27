package rest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/equinor/flowify-workflows-server/pkg/secret"
	"github.com/gorilla/mux"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
)

type SecretField struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func RegisterSecretRoutes(r *mux.Route, k8sinterface kubernetes.Interface) {
	s := r.Subrouter()

	const intype = "application/json"
	const outtype = "application/json"

	secretClient := secret.NewSecretClient(k8sinterface)

	s.Use(CheckContentHeaderMiddleware(intype))
	s.Use(CheckAcceptRequestHeaderMiddleware(outtype))
	s.Use(SetContentTypeMiddleware(outtype))

	s.HandleFunc("/secrets/{workspace}/", SecretListHandler(secretClient)).Methods(http.MethodGet)
	s.HandleFunc("/secrets/{workspace}/{key}", SecretPutHandler(secretClient)).Methods(http.MethodPut)
	s.HandleFunc("/secrets/{workspace}/{key}", SecretDeleteHandler(secretClient)).Methods(http.MethodGet)
	// no get handler, secrets not readable
	// s.HandleFunc("/secrets/{workspace}/{key}", SecretGetHandler(secretClient)).Methods(http.MethodGet)
}

func SecretListHandler(client secret.SecretClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workspace := mux.Vars(r)["workspace"]
		keys, err := client.ListAvailableKeys(r.Context(), workspace)
		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusInternalServerError, "error listing secrets", err.Error()}, "listSecrets")
			return
		}

		WriteResponse(w, http.StatusOK, nil, struct {
			Items []string `json:"items"`
		}{Items: keys}, "secrets")
	})
}

func SecretDeleteHandler(client secret.SecretClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workspace := mux.Vars(r)["workspace"]
		keyName := mux.Vars(r)["key"]
		err := client.DeleteSecretKey(r.Context(), workspace, keyName)

		if err != nil {
			if k8serrors.IsNotFound(err) {
				WriteErrorResponse(w, APIError{http.StatusNotFound, "could not delete secret", err.Error()}, "deleteSecret")
				return
			} else {
				WriteErrorResponse(w, APIError{http.StatusInternalServerError, "could not delete secret", err.Error()}, "deleteSecret")
				return
			}
		}

		WriteResponse(w, http.StatusNoContent, nil, nil, "deleteSecret")
	})
}

func SecretPutHandler(client secret.SecretClient) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workspace := mux.Vars(r)["workspace"]
		key := mux.Vars(r)["key"]

		// read secrets to add from request
		buf := new(bytes.Buffer)
		buf.ReadFrom(r.Body)

		var secret SecretField
		err := json.Unmarshal(buf.Bytes(), &secret)

		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "could not unmarshal secret", err.Error()}, "putSecret")
			return
		}

		if secret.Key != key {
			WriteErrorResponse(w, APIError{http.StatusBadRequest, "secret key URL mismatch", fmt.Sprintf("%s vs %s", key, secret.Key)}, "putSecret")
			return
		}

		// list available keys
		keys, err := client.ListAvailableKeys(r.Context(), workspace)

		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusInternalServerError, "could not list secrets", err.Error()}, "putSecret")
			return
		}

		// compare to discern create/update
		create := true
		for _, k := range keys {
			if secret.Key == k {
				create = false
				break
			}
		}

		err = client.AddSecretKey(r.Context(), workspace, secret.Key, secret.Value)

		if err != nil {
			WriteErrorResponse(w, APIError{http.StatusInternalServerError, "could not put secret", err.Error()}, "putSecret")
			return
		}

		if create {
			// create a new secret
			w.Header().Add("Location", r.URL.RequestURI())
			WriteResponse(w, http.StatusCreated, map[string]string{"Location": r.URL.RequestURI()}, buf.Bytes(), "putSecret")
		} else {
			// update
			WriteResponse(w, http.StatusNoContent, nil, nil, "putSecret")

		}
	})
}
