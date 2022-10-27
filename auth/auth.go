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

type AccessLevel struct {
	User  bool
	Admin bool
}

func (ra RoleAuthorizer) GetWorkspacePermissions(wsp string, usr user.User) (AccessLevel, error) {
	wss := ra.Workspaces.ListWorkspaces()

	for _, ws := range wss {
		var al AccessLevel
		if ws.Name == wsp {
			al.User = ws.UserHasAccess(usr)
			al.Admin = ws.UserHasAdminAccess(usr)
			return al, nil
		}
	}

	return AccessLevel{}, nil
}

func (ra RoleAuthorizer) GetSecretPermissions(usr user.User, data any) (map[Action]bool, error) {
	p := make(map[Action]bool)

	workspace, ok := data.(string)
	if !ok {
		return map[Action]bool{}, errors.Errorf("could not decode the workspace variable")
	}

	al, err := ra.GetWorkspacePermissions(workspace, usr)
	if err != nil {
		return map[Action]bool{}, errors.Wrap(err, "could not get secret permissions")
	}

	// this is where access levels map to actions.
	p[Read] = al.User || al.Admin
	p[List] = al.User || al.Admin
	p[Write] = al.Admin
	p[Delete] = al.Admin

	return p, nil
}

func (ra RoleAuthorizer) GetVolumePermissions(usr user.User, data any) (map[Action]bool, error) {
	p := make(map[Action]bool)

	workspace, ok := data.(string)
	if !ok {
		return map[Action]bool{}, errors.Errorf("could not decode the workspace variable")
	}

	al, err := ra.GetWorkspacePermissions(workspace, usr)
	if err != nil {
		return map[Action]bool{}, errors.Wrap(err, "could not get secret permissions")
	}

	// this is where access levels map to actions.
	p[Read] = al.User || al.Admin
	p[List] = al.Admin || al.User
	p[Write] = al.Admin
	p[Delete] = al.Admin

	return p, nil
}

func (ra RoleAuthorizer) GetPermissions(subject Subject, action Action, usr user.User, data any) (bool, error) {
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
	case Volumes:
		perms, err := ra.GetVolumePermissions(usr, data)
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
