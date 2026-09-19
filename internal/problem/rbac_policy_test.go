package problem

import (
	"testing"

	"SOJ/internal/auth"
)

func TestRBACProblemPolicyCanCreate(t *testing.T) {
	policy := RBACProblemPolicy{}
	cases := []struct {
		name string
		role auth.Role
		want bool
	}{
		{name: "admin", role: auth.RoleAdmin, want: true},
		{name: "root", role: auth.RoleRoot, want: true},
		{name: "author", role: auth.RoleAuthor, want: true},
		{name: "user", role: auth.RoleUser, want: false},
		{name: "reviewer", role: auth.RoleReviewer, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actor := auth.Actor{UserID: 11, Roles: []auth.Role{tc.role}}
			err := policy.CanCreate(actor)
			if (err == nil) != tc.want {
				t.Fatalf("CanCreate(%s) error = %v, want allowed = %v", tc.role, err, tc.want)
			}
		})
	}
}

func TestRBACProblemPolicyFullAccessRolesBypassOwnership(t *testing.T) {
	policy := RBACProblemPolicy{}
	other := ProblemRecord{ID: 1, OwnerUserID: 99}

	for _, role := range []auth.Role{auth.RoleAdmin, auth.RoleRoot} {
		actor := auth.Actor{UserID: 11, Roles: []auth.Role{role}}
		if err := policy.CanEdit(actor, other); err != nil {
			t.Fatalf("%s CanEdit error = %v", role, err)
		}
		if err := policy.CanSubmitReview(actor, other); err != nil {
			t.Fatalf("%s CanSubmitReview error = %v", role, err)
		}
		if err := policy.CanDecideReview(actor, other); err != nil {
			t.Fatalf("%s CanDecideReview error = %v", role, err)
		}
		if err := policy.CanViewReviewQueue(actor); err != nil {
			t.Fatalf("%s CanViewReviewQueue error = %v", role, err)
		}
		if err := policy.CanViewReviewEvents(actor, other); err != nil {
			t.Fatalf("%s CanViewReviewEvents error = %v", role, err)
		}
	}
}

func TestRBACProblemPolicyAuthorIsScopedToOwnProblems(t *testing.T) {
	policy := RBACProblemPolicy{}
	own := ProblemRecord{ID: 1, OwnerUserID: 11}
	other := ProblemRecord{ID: 2, OwnerUserID: 99}
	author := auth.Actor{UserID: 11, Roles: []auth.Role{auth.RoleAuthor}}

	if err := policy.CanEdit(author, own); err != nil {
		t.Fatalf("author CanEdit(own) error = %v", err)
	}
	if err := policy.CanEdit(author, other); err == nil {
		t.Fatal("author CanEdit(other) allowed, want forbidden")
	}
	if err := policy.CanSubmitReview(author, own); err != nil {
		t.Fatalf("author CanSubmitReview(own) error = %v", err)
	}
	if err := policy.CanViewReviewQueue(author); err == nil {
		t.Fatal("author CanViewReviewQueue allowed, want forbidden")
	}
	if err := policy.CanViewReviewEvents(author, own); err != nil {
		t.Fatalf("author CanViewReviewEvents(own) error = %v", err)
	}
	if err := policy.CanViewReviewEvents(author, other); err == nil {
		t.Fatal("author CanViewReviewEvents(other) allowed, want forbidden")
	}
}

func TestRBACProblemPolicyReviewerCannotDecideOwnProblem(t *testing.T) {
	policy := RBACProblemPolicy{}
	problem := ProblemRecord{ID: 1, OwnerUserID: 11}

	self := auth.Actor{UserID: 11, Roles: []auth.Role{auth.RoleReviewer, auth.RoleAuthor}}
	if err := policy.CanDecideReview(self, problem); err == nil {
		t.Fatal("self review allowed, want forbidden")
	}

	peer := auth.Actor{UserID: 12, Roles: []auth.Role{auth.RoleReviewer}}
	if err := policy.CanDecideReview(peer, problem); err != nil {
		t.Fatalf("reviewer CanDecideReview error = %v", err)
	}
	if err := policy.CanViewReviewQueue(peer); err != nil {
		t.Fatalf("reviewer CanViewReviewQueue error = %v", err)
	}
}

func TestRBACProblemPolicyOperatorCannotAuthorProblems(t *testing.T) {
	policy := RBACProblemPolicy{}
	operator := auth.Actor{UserID: 11, Roles: []auth.Role{auth.RoleOperator}}
	if err := policy.CanRejudge(operator); err != nil {
		t.Fatalf("operator CanRejudge error = %v", err)
	}
	if err := policy.CanCreate(operator); err == nil {
		t.Fatal("operator CanCreate allowed, want forbidden")
	}
	if err := policy.CanViewReviewQueue(operator); err == nil {
		t.Fatal("operator CanViewReviewQueue allowed, want forbidden")
	}
}

func TestRBACProblemPolicyAnonymousIsRejected(t *testing.T) {
	policy := RBACProblemPolicy{}
	anonymous := auth.Anonymous("req-1")
	if err := policy.CanCreate(anonymous); err == nil {
		t.Fatal("anonymous CanCreate allowed, want forbidden")
	}
	if err := policy.CanRejudge(anonymous); err == nil {
		t.Fatal("anonymous CanRejudge allowed, want forbidden")
	}
	if err := policy.CanViewReviewQueue(anonymous); err == nil {
		t.Fatal("anonymous CanViewReviewQueue allowed, want forbidden")
	}
}
