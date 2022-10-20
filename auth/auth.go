package auth

import (
	"context"
	"net/http"

	"github.com/equinor/flowify-workflows-server/pkg/workspace"
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
	Workspaces workspace.WorkspaceClient
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
	Volumes Subject = "volumes"
)

func (ra RoleAuthorizer) GetWorkspacePermissions(wsp string, usr user.User) (bool, bool, error) {
	wss, err := ra.Workspaces.ListWorkspaces(context.TODO(), usr)
	if err != nil {
		return false, false, errors.Wrap(err, "could not get workspace permissions")
	}

	for _, ws := range wss {
		if ws.Name == wsp {
			return ws.UserHasAccess(usr), ws.UserHasAdminAccess(usr), nil
		}
	}

	return false, false, nil
}

func (ra RoleAuthorizer) GetSecretPermissions(usr user.User, data any) (map[Action]bool, error) {
	p := make(map[Action]bool)

	workspace, ok := data.(string)
	if !ok {
		return map[Action]bool{}, errors.Errorf("could not decode the workspace variable")
	}

	userAccess, adminAccess, err := ra.GetWorkspacePermissions(workspace, usr)
	if err != nil {
		return map[Action]bool{}, errors.Wrap(err, "could not get secret permissions")
	}

	// this is where access levels map to actions.
	p[Read] = userAccess || adminAccess
	p[List] = userAccess || adminAccess
	p[Write] = adminAccess
	p[Delete] = adminAccess

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
