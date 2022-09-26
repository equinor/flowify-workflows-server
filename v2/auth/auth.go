package auth

import (
	"net/http"

	"github.com/equinor/flowify-workflows-server/v2/user"
)

// an authclient either gives an error or an authenticated user
type AuthClient interface {
	Authenticate(r *http.Request) (user.User, error)
}

// the mock authenticator can be used for testing
type MockAuthenticator struct {
	User user.MockUser
}

func (m MockAuthenticator) Authenticate(r *http.Request) (user.User, error) {
	return m.User, nil
}
