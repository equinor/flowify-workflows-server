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
	Authorize(subject Subject, action Action, user user.User, object any) (bool, error)
	// AuthorizePath(user user.User, )
}

type RoleAuthorizer struct {
	// map subject -> action -> required permssion
}

type Action string

const (
	Read   Action = "read"
	Write  Action = "write"
	Delete Action = "delete"
	List   Action = "list"
)

type Subject string

const (
	Secrets Subject = "secrets"
)

func (ra RoleAuthorizer) GetSecretPermissions(usr user.User, data any) (map[Action]bool, error) {
	p := make(map[Action]bool)

	workspace, ok := data.(string)
	if !ok {
		return map[Action]bool{}, errors.Errorf("could not decode the workspace variable")
	}

	p[Read] = user.UserHasRole(usr, user.Role(workspace)) ||
		user.UserHasRole(usr, user.Role(workspace+"-admin")) ||
		user.UserHasRole(usr, user.Role("admin"))
	p[List] = p[Read]
	p[Write] = user.UserHasRole(usr, user.Role(workspace+"-admin")) ||
		user.UserHasRole(usr, user.Role("admin"))
	p[Delete] = p[Write]

	return p, nil
}

func (ra RoleAuthorizer) GetPermissions(subject Subject, action Action, usr user.User, data any) (bool, error) {
	// start with no access
	switch subject {
	case Secrets:
		perms, err := ra.GetSecretPermissions(usr, data)
		if err != nil {
			return false, err
		}
		if p, ok := perms[action]; ok {
			return p, nil
		}
		return false, errors.Errorf("Rule %s:%s not found", subject, action)

	default:
		return false, errors.Errorf("no such subject '%s'", subject)
	}
}

func (ra RoleAuthorizer) Authorize(subject Subject, action Action, user user.User, object any) (bool, error) {
	p, err := ra.GetPermissions(subject, action, user, object)
	if err != nil {
		return false, errors.Wrapf(err, "could not authorize request for %s:%s", subject, action)
	}

	return p, nil
}
