package authz

import (
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

// Permission is the site permission code. It is an alias for auth.Permission so
// an Actor can carry the resolved set without authz importing itself into auth.
type Permission = auth.Permission

const (
	// PermissionAuditRead gates the audit console's read surface.
	PermissionAuditRead Permission = "audit.read"

	PermissionProblemCreate       Permission = "problem.create"
	PermissionProblemEditOwn      Permission = "problem.edit_own"
	PermissionProblemSubmitReview Permission = "problem.submit_review"
	PermissionProblemReview       Permission = "problem.review"
	PermissionProblemPublish      Permission = "problem.publish"
	PermissionProblemManageAll    Permission = "problem.manage_all"

	PermissionSubmissionRejudge Permission = "submission.rejudge"

	PermissionContestCreate    Permission = "contest.create"
	PermissionContestRead      Permission = "contest.read"
	PermissionContestManage    Permission = "contest.manage"
	PermissionContestJudge     Permission = "contest.judge"
	PermissionContestManageAll Permission = "contest.manage_all"

	PermissionJudgeInspect Permission = "judge.inspect"

	PermissionUserManage Permission = "user.manage"
	PermissionRoleGrant  Permission = "role.grant"
	PermissionRoleRevoke Permission = "role.revoke"

	// PermissionRolePermissionManage gates the phase-2 role permission matrix.
	// It is full-access only: a role that could grant itself permissions would
	// defeat the matrix.
	PermissionRolePermissionManage Permission = "role.permission.manage"

	// PermissionSystemManage gates language catalog administration. Audit reads
	// have their own permission (audit.read), so this name now describes its
	// only consumer.
	PermissionSystemManage Permission = "system.manage"
)

// fullAccessRoles hold every known permission. Admin and root are both
// intentionally full-access: the difference between them is operational
// (root is the break-glass account) rather than a difference in permission
// set, so admin must not be described by a partial role_permissions row set.
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
	PermissionAuditRead,
	PermissionContestCreate,
	PermissionContestJudge,
	PermissionContestManage,
	PermissionContestManageAll,
	PermissionContestRead,
	PermissionJudgeInspect,
	PermissionProblemCreate,
	PermissionProblemEditOwn,
	PermissionProblemManageAll,
	PermissionProblemPublish,
	PermissionProblemReview,
	PermissionProblemSubmitReview,
	PermissionRoleGrant,
	PermissionRolePermissionManage,
	PermissionRoleRevoke,
	PermissionSubmissionRejudge,
	PermissionSystemManage,
	PermissionUserManage,
}

func AllPermissions() []Permission {
	return append([]Permission(nil), allPermissions...)
}

// PermissionScope records whether a permission applies site-wide or only to a
// contest scope. It is directory metadata consumed by the web gate matrix; the
// backend itself does not enforce scope from this field.
type PermissionScope string

const (
	ScopeGlobal  PermissionScope = "global"
	ScopeContest PermissionScope = "contest"
)

// PermissionSpec is one row of the exported permission directory. Consumer names
// the production gate that actually reads the permission, so a zero-consumer
// permission is a visible defect rather than a surprise.
type PermissionSpec struct {
	Code      Permission
	Scope     PermissionScope
	Delegable bool   // false = admin/root only; the phase-2 matrix cannot grant it
	Consumer  string // real gate, must be non-empty
}

// permissionConsumers maps every permission to the production call site that
// authorizes on it. See docs/authz.md for the same table in prose.
var permissionConsumers = map[Permission]string{
	PermissionAuditRead:            "audit.Service.ListEvents",
	PermissionContestCreate:        "contest.requireContestCreator",
	PermissionContestJudge:         "contest.AuthorizeContestRejudge",
	PermissionContestManage:        "contest.canManageContest",
	PermissionContestManageAll:     "contest.requireContestCreator/canManageAllContests",
	PermissionContestRead:          "contest.ContestReader",
	PermissionJudgeInspect:         "submission.SubmissionReader (任意提交/diagnostics) + submission.RunService.GetRun",
	PermissionProblemCreate:        "problem.RBACProblemPolicy.CanCreate",
	PermissionProblemEditOwn:       "problem.RBACProblemPolicy.CanEdit",
	PermissionProblemManageAll:     "problem.RBACProblemPolicy (bypass) + problem.canReadProblem",
	PermissionProblemPublish:       "problem.RBACProblemPolicy.CanDecideReview",
	PermissionProblemReview:        "problem.RBACProblemPolicy.CanViewReviewQueue/CanDecideReview/CanViewReviewEvents",
	PermissionProblemSubmitReview:  "problem.RBACProblemPolicy.CanSubmitReview",
	PermissionRoleGrant:            "user.Service.GrantRole",
	PermissionRolePermissionManage: "user.RolePermissionService.Matrix/Replace",
	PermissionRoleRevoke:           "user.Service.RevokeRole",
	PermissionSubmissionRejudge:    "problem.RBACProblemPolicy.CanRejudge + submission.RejudgeService",
	PermissionSystemManage:         "submission.LanguageService (语言管理)",
	PermissionUserManage:           "user.Service.ListUsers/ListUsersByCursor/UpdateUser",
}

// nonDelegablePermissions are the full-access-only permissions. They stay in the
// directory so full-access roles carry them, but the phase-2 matrix must not
// hand them to a scoped role.
var nonDelegablePermissions = map[Permission]struct{}{
	PermissionAuditRead:            {},
	PermissionRoleGrant:            {},
	PermissionRolePermissionManage: {},
	PermissionRoleRevoke:           {},
	PermissionSystemManage:         {},
	PermissionUserManage:           {},
}

// contestScopedPermissions can only be held by contest roles. contest.create
// and contest.manage_all stay global: creating a contest and administering
// every contest are site-wide capabilities with no single contest context.
var contestScopedPermissions = map[Permission]struct{}{
	PermissionContestRead:   {},
	PermissionContestManage: {},
	PermissionContestJudge:  {},
}

func permissionScope(code Permission) PermissionScope {
	if _, ok := contestScopedPermissions[code]; ok {
		return ScopeContest
	}
	return ScopeGlobal
}

// Catalog returns a copy of the permission directory in allPermissions order.
func Catalog() []PermissionSpec {
	catalog := make([]PermissionSpec, 0, len(allPermissions))
	for _, code := range allPermissions {
		_, nonDelegable := nonDelegablePermissions[code]
		catalog = append(catalog, PermissionSpec{
			Code:      code,
			Scope:     permissionScope(code),
			Delegable: !nonDelegable,
			Consumer:  permissionConsumers[code],
		})
	}
	return catalog
}

// Lookup returns the directory entry for a permission code. The second result
// is false when the code is not part of the directory.
func Lookup(code Permission) (PermissionSpec, bool) {
	for _, spec := range Catalog() {
		if spec.Code == code {
			return spec, true
		}
	}
	return PermissionSpec{}, false
}

// permissionCombos are the named permission combinations shared with SOJ-web
// (lib/auth/gates.ts). They are not permission points: each combination is an
// "any of" set unless its consumer documents a stricter rule.
var permissionCombos = map[string][]Permission{
	"problem.authoring.access": {
		PermissionProblemCreate,
		PermissionProblemReview,
		PermissionProblemManageAll,
	},
	"problem.review.queue": {
		PermissionProblemReview,
		PermissionProblemManageAll,
	},
	"problem.review.decide": {
		PermissionProblemReview,
		PermissionProblemPublish,
		PermissionProblemManageAll,
	},
}

// Combos returns a copy of the named permission combinations shared with SOJ-web
// (lib/auth/gates.ts). Keys are stable; each repo locks its own copy by test.
func Combos() map[string][]Permission {
	combos := make(map[string][]Permission, len(permissionCombos))
	for name, permissions := range permissionCombos {
		combos[name] = append([]Permission(nil), permissions...)
	}
	return combos
}
