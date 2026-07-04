package auth

import "testing"

func TestParseRole(t *testing.T) {
	tests := map[string]Role{
		"user":  RoleUser,
		"admin": RoleAdmin,
		"root":  RoleRoot,
	}

	for input, want := range tests {
		got, err := ParseRole(input)
		if err != nil {
			t.Fatalf("ParseRole(%q) error = %v", input, err)
		}
		if got != want {
			t.Fatalf("ParseRole(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestActorRoleChecks(t *testing.T) {
	admin := Actor{UserID: 42, Role: RoleAdmin}

	if !admin.Authenticated() {
		t.Fatal("admin should be authenticated")
	}
	if !admin.Admin() {
		t.Fatal("admin should satisfy Admin")
	}
	if admin.Root() {
		t.Fatal("admin should not satisfy Root")
	}
}
