package submission

import (
	"SOJ/internal/auth"
	"SOJ/internal/authz"
)

// canInspectSubmissions reports whether the actor holds judge.inspect, the
// permission that lets an operator or full-access role read any submission,
// its source, and its judge diagnostics. It replaces the former actor.Admin()
// checks; ownership and contest-staff fallbacks stay separate at each call site.
func canInspectSubmissions(actor auth.Actor) bool {
	return authz.Authorize(authz.NewSubject(actor), authz.PermissionJudgeInspect) == nil
}

// canRejudge reports whether the actor holds submission.rejudge, which lets the
// rejudge batch list show every batch rather than only the actor's own.
func canRejudge(actor auth.Actor) bool {
	return authz.Authorize(authz.NewSubject(actor), authz.PermissionSubmissionRejudge) == nil
}
