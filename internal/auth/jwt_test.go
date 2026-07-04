package auth

import (
	"testing"
	"time"
)

func TestJWTManagerIssueAndParseAccessToken(t *testing.T) {
	manager := NewJWTManager("test-secret", time.Hour)
	actor := Actor{UserID: 42, Role: RoleAdmin, DeviceID: "device-1"}

	token, err := manager.IssueAccessToken(actor)
	if err != nil {
		t.Fatalf("IssueAccessToken() error = %v", err)
	}

	parsed, err := manager.ParseAccessToken(token)
	if err != nil {
		t.Fatalf("ParseAccessToken() error = %v", err)
	}
	if parsed.UserID != actor.UserID || parsed.Role != actor.Role || parsed.DeviceID != actor.DeviceID {
		t.Fatalf("parsed actor = %+v, want %+v", parsed, actor)
	}
}

func TestJWTManagerRejectsExpiredToken(t *testing.T) {
	manager := NewJWTManager("test-secret", -time.Second)
	token, err := manager.IssueAccessToken(Actor{UserID: 42, Role: RoleUser, DeviceID: "device-1"})
	if err != nil {
		t.Fatalf("IssueAccessToken() error = %v", err)
	}

	if _, err := manager.ParseAccessToken(token); err == nil {
		t.Fatal("ParseAccessToken(expired) error = nil, want error")
	}
}
