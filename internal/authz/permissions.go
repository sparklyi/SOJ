package authz

import (
	"sort"

	"SOJ/internal/auth"
)

type Role = auth.Role

const (
	RoleUser           = auth.RoleUser
	RoleAuthor         = auth.RoleAuthor
	RoleReviewer       = auth.RoleReviewer
	RoleOperator       = auth.RoleOperator
	RoleAdmin          = auth.RoleAdmin
	RoleRoot           = auth.RoleRoot
	RoleContestStaff   = auth.RoleContestStaff
	RoleContestManager = auth.RoleContestManager
	RoleContestJudge   = auth.RoleContestJudge
)

type Permission string

const (
	PermissionProblemRead         Permission = "problem.read"
	PermissionProblemCreate       Permission = "problem.create"
	PermissionProblemEditOwn      Permission = "problem.edit_own"
	PermissionProblemTestcaseOwn  Permission = "problem.testcase.manage_own"
	PermissionProblemCheckOwn     Permission = "problem.check_own"
	PermissionProblemSubmitReview Permission = "problem.submit_review"
	PermissionProblemReview       Permission = "problem.review"
	PermissionProblemPublish      Permission = "problem.publish"
	PermissionProblemManageAll    Permission = "problem.manage_all"
	PermissionSubmissionCreate    Permission = "submission.create"
	PermissionSubmissionReadOwn   Permission = "submission.read_own"
	PermissionSubmissionRejudge   Permission = "submission.rejudge"
	PermissionContestJoin         Permission = "contest.join"
	PermissionContestRead         Permission = "contest.read"
	PermissionContestManage       Permission = "contest.manage"
	PermissionContestJudge        Permission = "contest.judge"
	PermissionContestManageAll    Permission = "contest.manage_all"
	PermissionJudgeInspect        Permission = "judge.inspect"
	PermissionUserManage          Permission = "user.manage"
	PermissionRoleGrant           Permission = "role.grant"
	PermissionRoleRevoke          Permission = "role.revoke"
	// PermissionSystemManage is reserved. No HTTP endpoint consumes it yet; it is
	// intentionally kept in AllPermissions so full-access roles already carry it
	// once the system administration surface ships. Do not hand it to a
	// non-full-access role.
	PermissionSystemManage Permission = "system.manage"
)

var rolePermissions = map[Role][]Permission{
	RoleUser: {
		PermissionProblemRead,
		PermissionSubmissionCreate,
		PermissionSubmissionReadOwn,
		PermissionContestJoin,
	},
	RoleAuthor: {
		PermissionProblemCreate,
		PermissionProblemEditOwn,
		PermissionProblemTestcaseOwn,
		PermissionProblemCheckOwn,
		PermissionProblemSubmitReview,
	},
	RoleReviewer: {
		PermissionProblemReview,
		PermissionProblemPublish,
	},
	RoleContestStaff: {
		PermissionContestRead,
	},
	RoleContestManager: {
		PermissionContestRead,
		PermissionContestManage,
	},
	RoleContestJudge: {
		PermissionContestRead,
		PermissionContestJudge,
	},
	RoleOperator: {
		PermissionSubmissionRejudge,
		PermissionJudgeInspect,
	},
}

// fullAccessRoles hold every known permission. Admin and root are both
// intentionally full-access: the difference between them is operational
// (root is the break-glass account) rather than a difference in permission
// set, so admin must not be described by a partial rolePermissions entry.
var fullAccessRoles = []Role{
	RoleAdmin,
	RoleRoot,
}

func IsFullAccessRole(role Role) bool {
	for _, full := range fullAccessRoles {
		if role == full {
			return true
		}
	}
	return false
}

var allPermissions = []Permission{
	PermissionContestJoin,
	PermissionContestJudge,
	PermissionContestManage,
	PermissionContestManageAll,
	PermissionContestRead,
	PermissionJudgeInspect,
	PermissionProblemCheckOwn,
	PermissionProblemCreate,
	PermissionProblemEditOwn,
	PermissionProblemManageAll,
	PermissionProblemPublish,
	PermissionProblemRead,
	PermissionProblemReview,
	PermissionProblemSubmitReview,
	PermissionProblemTestcaseOwn,
	PermissionRoleGrant,
	PermissionRoleRevoke,
	PermissionSubmissionCreate,
	PermissionSubmissionReadOwn,
	PermissionSubmissionRejudge,
	PermissionSystemManage,
	PermissionUserManage,
}

func AllPermissions() []Permission {
	return append([]Permission(nil), allPermissions...)
}

func RolePermissions(role Role) []Permission {
	if IsFullAccessRole(role) {
		return AllPermissions()
	}
	return append([]Permission(nil), rolePermissions[role]...)
}

func PermissionsForRoles(roles []Role) []Permission {
	seen := make(map[Permission]struct{})
	for _, role := range roles {
		for _, permission := range RolePermissions(role) {
			seen[permission] = struct{}{}
		}
	}
	permissions := make([]Permission, 0, len(seen))
	for permission := range seen {
		permissions = append(permissions, permission)
	}
	sort.Slice(permissions, func(i, j int) bool { return permissions[i] < permissions[j] })
	return permissions
}
