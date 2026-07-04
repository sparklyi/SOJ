package auth

import (
	"fmt"
	"strings"
)

type Role string

const (
	RoleUser  Role = "user"
	RoleAdmin Role = "admin"
	RoleRoot  Role = "root"
)

type Actor struct {
	UserID    int64
	Role      Role
	DeviceID  string
	RequestID string
}

func Anonymous(requestID string) Actor {
	return Actor{RequestID: requestID}
}

func ParseRole(value string) (Role, error) {
	switch Role(strings.ToLower(strings.TrimSpace(value))) {
	case RoleUser:
		return RoleUser, nil
	case RoleAdmin:
		return RoleAdmin, nil
	case RoleRoot:
		return RoleRoot, nil
	default:
		return "", fmt.Errorf("unknown role %q", value)
	}
}

func (a Actor) Authenticated() bool {
	return a.UserID > 0
}

func (a Actor) Admin() bool {
	return a.Role == RoleAdmin || a.Role == RoleRoot
}

func (a Actor) Root() bool {
	return a.Role == RoleRoot
}
