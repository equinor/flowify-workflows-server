package user

import (
	"context"
)

type ContextKey int

const (
	UserKey ContextKey = iota
)

type Role string

// tightly modelled on JWT: http://jwt.io
type User interface {
	GetUid() string
	GetName() string
	GetEmail() string
	GetRoles() []Role
}

func GetUser(ctx context.Context) User {
	val := ctx.Value(UserKey)

	if val == nil {
		return nil
	} else {
		return val.(User)
	}
}

func UserHasRole(u User, entry Role) bool {
	for _, item := range u.GetRoles() {
		if entry == item {
			return true
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
