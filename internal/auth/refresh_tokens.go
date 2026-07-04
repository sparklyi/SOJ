package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"time"
)

type RefreshTokenRecord struct {
	TokenHash string
	RevokedAt *time.Time
}

func NewRefreshToken() (token string, hash string, err error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(raw[:])
	return token, HashRefreshToken(token), nil
}

func HashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (r RefreshTokenRecord) Revoked() bool {
	return r.RevokedAt != nil
}

func (r *RefreshTokenRecord) Revoke() {
	now := time.Now().UTC()
	r.RevokedAt = &now
}
