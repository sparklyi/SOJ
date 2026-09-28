package problem

import (
	"SOJ/internal/apperror"
	"SOJ/internal/auth"
	"SOJ/internal/authz"
)

// RBACProblemPolicy keeps permission checks in the problem domain while using
// the shared role-to-permission mapping as the source of truth.
type RBACProblemPolicy struct{}

func (RBACProblemPolicy) CanCreate(actor auth.Actor) error {
	return requireProblemPermission(actor, authz.PermissionProblemCreate)
}

// CanAccessAuthoring gates the authoring console entry. Reviewers and problem
// managers use the console without creating drafts themselves, so any member of
// authz.Combos()["problem.authoring.access"] is enough; an ordinary user has
// none of them.
func (RBACProblemPolicy) CanAccessAuthoring(actor auth.Actor) error {
	for _, permission := range authz.Combos()["problem.authoring.access"] {
		if hasProblemPermission(actor, permission) {
			return nil
		}
	}
	return problemForbidden("required problem permission is missing")
}

func (RBACProblemPolicy) CanEdit(actor auth.Actor, problem ProblemRecord) error {
	if hasProblemPermission(actor, authz.PermissionProblemManageAll) {
		return nil
	}
	if actor.UserID != problem.OwnerUserID {
		return problemForbidden("problem owner or problem.manage_all permission required")
	}
	return requireProblemPermission(actor, authz.PermissionProblemEditOwn)
}

func (RBACProblemPolicy) CanRejudge(actor auth.Actor) error {
	return requireProblemPermission(actor, authz.PermissionSubmissionRejudge)
}

func (RBACProblemPolicy) CanSubmitReview(actor auth.Actor, problem ProblemRecord) error {
	if hasProblemPermission(actor, authz.PermissionProblemManageAll) {
		return nil
	}
	if actor.UserID != problem.OwnerUserID {
		return problemForbidden("problem owner or problem.manage_all permission required")
	}
	return requireProblemPermission(actor, authz.PermissionProblemSubmitReview)
}

func (RBACProblemPolicy) CanDecideReview(actor auth.Actor, problem ProblemRecord) error {
	if hasProblemPermission(actor, authz.PermissionProblemManageAll) {
		return nil
	}
	if actor.UserID == problem.OwnerUserID {
		return apperror.Forbidden("problem.self_review_forbidden", "problem authors cannot review their own problems")
	}
	// authz.Combos()["problem.review.decide"] documents the full set
	// (review + publish, with manage_all as the bypass above). The two checks stay
	// sequential and AND-ed: a reviewer may only decide when they hold both.
	if err := requireProblemPermission(actor, authz.PermissionProblemReview); err != nil {
		return err
	}
	return requireProblemPermission(actor, authz.PermissionProblemPublish)
}

func (RBACProblemPolicy) CanViewReviewQueue(actor auth.Actor) error {
	for _, permission := range authz.Combos()["problem.review.queue"] {
		if hasProblemPermission(actor, permission) {
			return nil
		}
	}
	return problemForbidden("required problem permission is missing")
}

func (RBACProblemPolicy) CanViewReviewEvents(actor auth.Actor, problem ProblemRecord) error {
	if hasProblemPermission(actor, authz.PermissionProblemManageAll) || hasProblemPermission(actor, authz.PermissionProblemReview) {
		return nil
	}
	if actor.UserID == problem.OwnerUserID {
		return requireProblemPermission(actor, authz.PermissionProblemEditOwn)
	}
	return problemForbidden("problem owner, reviewer, or problem.manage_all permission required")
}

func (RBACProblemPolicy) CanRestore(actor auth.Actor) error {
	return requireProblemPermission(actor, authz.PermissionProblemManageAll)
}

func requireProblemPermission(actor auth.Actor, permission authz.Permission) error {
	if err := authz.Authorize(authz.NewSubject(actor), permission); err != nil {
		return problemForbidden("required problem permission is missing")
	}
	return nil
}

func hasProblemPermission(actor auth.Actor, permission authz.Permission) bool {
	return authz.Authorize(authz.NewSubject(actor), permission) == nil
}

func problemForbidden(message string) error {
	return apperror.Forbidden("problem.forbidden", message)
}
