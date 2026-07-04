package auth

import (
	"testing"
)

func TestNewRefreshTokenHashesOnlyServerSide(t *testing.T) {
	token, hash, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken() error = %v", err)
	}
	if token == "" || hash == "" {
		t.Fatalf("token = %q hash = %q, want non-empty", token, hash)
	}
	if token == hash {
		t.Fatal("refresh token hash equals plaintext token")
	}
	if got := HashRefreshToken(token); got != hash {
		t.Fatalf("HashRefreshToken(token) = %q, want %q", got, hash)
	}
}

func TestRefreshTokenRecordRevocation(t *testing.T) {
	record := RefreshTokenRecord{TokenHash: "hash"}
	if record.Revoked() {
		t.Fatal("new record is revoked")
	}
	record.Revoke()
	if !record.Revoked() {
		t.Fatal("record not revoked after Revoke")
	}
}
