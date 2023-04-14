package user

import (
	"context"
	"strings"
)

type ContextKey int

const (
	UserKey ContextKey = iota
)

const CustomRolePattern string = "--$"

type Role string

var ListGroupsCanCreateWorkspaces = [3]string{"admin", "developer-admin", "sandbox-developer"}
var ListGroupsCanSeeCustomRoles = [3]string{"admin", "developer-admin", "sandbox-developer"}

// tightly modelled on JWT: http://jwt.io
type User interface {
	GetUid() string
	GetName() string
	GetEmail() string
	GetRoles() []Role
}

func CanCreateWorkspaces(user User) bool {
	for _, role := range user.GetRoles() {
		for _, r := range ListGroupsCanCreateWorkspaces {
			if string(role) == r {
				return true
			}
		}
	}
	return false
}

func GetUser(ctx context.Context) User {
	val := ctx.Value(UserKey)

	if val == nil {
		return nil
	} else {
		return val.(User)
	}
}

func HasOwnership(u User, roles []Role) bool {
	var hasOwnership bool
	for _, r := range roles {
		if strings.Contains(string(r), CustomRolePattern) {
			cr := u.GetEmail() + CustomRolePattern + "owner"
			if string(r) == cr {
				hasOwnership = true
			}
		}
	}
	return hasOwnership
}

func UserHasRole(u User, entry Role) bool {
	for _, item := range u.GetRoles() {
		if entry == item {
			return true
		}
	}
	return false
}

func CanSeeCustomRoles(u User) bool {
	canSeeRoles := ListGroupsCanSeeCustomRoles
	for _, role := range u.GetRoles() {
		for _, r := range canSeeRoles {
			if string(role) == r {
				return true
			}
		}
	}
	return false
}

// adds the User to the given context
func UserContext(user User, ctx context.Context) context.Context {
	return context.WithValue(ctx, UserKey, user)
}

// a mock user for tests
type MockUser struct {
	Uid   string
	Name  string
	Email string
	Roles []Role
}

func (u MockUser) GetUid() string   { return u.Uid }
func (u MockUser) GetName() string  { return u.Name }
func (u MockUser) GetEmail() string { return u.Email }
func (u MockUser) GetRoles() []Role { return u.Roles }
