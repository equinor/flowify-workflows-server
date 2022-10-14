package auth

import (
	"context"
	"net/http"

	"github.com/equinor/flowify-workflows-server/user"
	"github.com/pkg/errors"
)

// an authclient either gives an error or an authenticated user
type AuthenticationClient interface {
	Authenticate(r *http.Request) (user.User, error)
}

// the mock authenticator can be used for testing
type MockAuthenticator struct {
	User user.MockUser
}

func (m MockAuthenticator) Authenticate(r *http.Request) (user.User, error) {
	return m.User, nil
}

type ContextKey = int

const (
	AuthorizationKey ContextKey = iota
)

type Permission struct {
	Read    bool
	Write   bool
	Delete  bool
	Execute bool
}

// Compares two permissions ('req', 'given') to see if 'given' has (at least) the required permissions
func HasPermission(req Permission, given Permission) bool {
	if req.Read && !given.Read {
		return false
	}
	if req.Write && !given.Write {
		return false
	}
	if req.Delete && !given.Delete {
		return false
	}
	if req.Execute && !given.Execute {
		return false
	}
	return true
}

type AuthorizeFunc = func(user user.User, obj string) (Permission, error)

type Authorization struct {
	Action     string
	Authorized bool
}

func GetAuthorization(ctx context.Context) *Authorization {
	val := ctx.Value(AuthorizationKey)

	if val == nil {
		return nil
	} else {
		return val.(*Authorization)
	}
}

type AuthorizationClient interface {
	Authorize(subject string, action string, req Permission, user user.User, object any) (bool, error)
	// AuthorizePath(user user.User, )
}

type RoleAuthorizer struct {
	// map subject -> action -> required permssion
}

func (ra RoleAuthorizer) GetSecretPermissions(subject string, action string, usr user.User, data any) (Permission, error) {
	if subject != "secret" {
		return Permission{}, errors.Errorf("subject not 'secret'")
	}

	p := Permission{}
	var err error
	switch action {
	case "read":
		{
			if workspace, ok := data.(string); ok {
				if user.UserHasRole(usr, user.Role(workspace)) || user.UserHasRole(usr, user.Role(workspace+"-admin")) || user.UserHasRole(usr, user.Role("admin")) {
					p.Read = true
				}
			}
		}
	case "write":
		{
			if workspace, ok := data.(string); ok {
				if user.UserHasRole(usr, user.Role(workspace+"-admin")) || user.UserHasRole(usr, user.Role("admin")) {
					p.Write = true
				}
			}
		}
	case "delete":
		{
			if workspace, ok := data.(string); ok {
				if user.UserHasRole(usr, user.Role(workspace+"-admin")) || user.UserHasRole(usr, user.Role("admin")) {
					p.Write = true
				}
			}
		}
	}

	return p, err
}

func (ra RoleAuthorizer) GetPermissions(subject string, action string, usr user.User, data any) (Permission, error) {
	// start with no access
	switch subject {
	case "secret":
		return ra.GetSecretPermissions(subject, action, usr, data)
	default:
		return Permission{}, errors.Errorf("no such subject '%s'", subject)
	}
}

func (ra RoleAuthorizer) Authorize(subject string, action string, req Permission, user user.User, object any) (bool, error) {
	p, err := ra.GetPermissions(subject, action, user, object)
	if err != nil {
		return false, errors.Wrapf(err, "could not authorize request for %s:%s", subject, action)
	}

	return HasPermission(req, p), nil
}
