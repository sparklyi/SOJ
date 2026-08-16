package auth

import (
	"fmt"
	"strings"
)

type Role string

const (
	RoleUser           Role = "user"
	RoleAuthor         Role = "author"
	RoleReviewer       Role = "reviewer"
	RoleOperator       Role = "operator"
	RoleAdmin          Role = "admin"
	RoleRoot           Role = "root"
	RoleContestStaff   Role = "contest_staff"
	RoleContestManager Role = "contest_manager"
	RoleContestJudge   Role = "contest_judge"
)

type Actor struct {
	UserID int64
	Roles  []Role
	// Role remains an in-memory single-role view for domain packages that have
	// not yet moved to Roles. It is never included in a JWT.
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
	case RoleAuthor:
		return RoleAuthor, nil
	case RoleReviewer:
		return RoleReviewer, nil
	case RoleOperator:
		return RoleOperator, nil
	case RoleAdmin:
		return RoleAdmin, nil
	case RoleRoot:
		return RoleRoot, nil
	case RoleContestStaff:
		return RoleContestStaff, nil
	case RoleContestManager:
		return RoleContestManager, nil
	case RoleContestJudge:
		return RoleContestJudge, nil
	default:
		return "", fmt.Errorf("unknown role %q", value)
	}
}

func IsGlobalRole(role Role) bool {
	switch role {
	case RoleUser, RoleAuthor, RoleReviewer, RoleOperator, RoleAdmin, RoleRoot:
		return true
	default:
		return false
	}
}

func IsContestRole(role Role) bool {
	switch role {
	case RoleContestStaff, RoleContestManager, RoleContestJudge:
		return true
	default:
		return false
	}
}

func (a Actor) Authenticated() bool {
	return a.UserID > 0
}

func (a Actor) HasRole(role Role) bool {
	for _, current := range a.Roles {
		if current == role {
			return true
		}
	}
	return a.Role == role
}

func (a Actor) Admin() bool {
	return a.HasRole(RoleAdmin) || a.HasRole(RoleRoot)
}

func (a Actor) Root() bool {
	return a.HasRole(RoleRoot)
}
