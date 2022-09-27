package rest

import (
	"net/http"

	"github.com/equinor/flowify-workflows-server/user"
	"github.com/gorilla/mux"
)

func RegisterUserInfoRoutes(r *mux.Route) {
	s := r.Subrouter()

	const intype = "application/json"
	const outtype = "application/json"

	s.Use(CheckContentHeaderMiddleware(intype))
	s.Use(CheckAcceptRequestHeaderMiddleware(outtype))
	s.Use(SetContentTypeMiddleware(outtype))

	s.HandleFunc("/userinfo/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := user.GetUser(ctx)

		WriteResponse(w, http.StatusOK, nil, id, "userinfo")
	})).Methods(http.MethodGet)

}
