package problem

import (
	"SOJ/internal/auth"
	"SOJ/internal/authz"
)

// seededPermissions mirrors the phase-2 role_permissions migration seed. Unit
// tests cannot read the database, so they state the mapping explicitly;
// full-access roles expand to the whole directory exactly like the resolver.
// This is test-only scaffolding: production resolves permissions from the DB.
func seededPermissions(roles ...auth.Role) []authz.Permission {
	for _, role := range roles {
		if authz.IsFullAccessRole(role) {
			return authz.AllPermissions()
		}
	}
	permissions := make([]authz.Permission, 0)
	for _, role := range roles {
		permissions = append(permissions, seededRolePermissions[role]...)
	}
	return permissions
}

var seededRolePermissions = map[auth.Role][]authz.Permission{
	auth.RoleUser:           {},
	auth.RoleAuthor:         {authz.PermissionProblemCreate, authz.PermissionProblemEditOwn, authz.PermissionProblemSubmitReview},
	auth.RoleReviewer:       {authz.PermissionProblemReview, authz.PermissionProblemPublish},
	auth.RoleOperator:       {authz.PermissionSubmissionRejudge, authz.PermissionJudgeInspect},
	auth.RoleContestStaff:   {authz.PermissionContestRead},
	auth.RoleContestManager: {authz.PermissionContestRead, authz.PermissionContestManage},
	auth.RoleContestJudge:   {authz.PermissionContestRead, authz.PermissionContestJudge},
}
