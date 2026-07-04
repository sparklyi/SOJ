package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type JWTManager struct {
	secret []byte
	ttl    time.Duration
}

type accessClaims struct {
	UserID   int64  `json:"user_id"`
	Role     string `json:"role"`
	DeviceID string `json:"device_id"`
	jwt.RegisteredClaims
}

func NewJWTManager(secret string, ttl time.Duration) *JWTManager {
	return &JWTManager{secret: []byte(secret), ttl: ttl}
}

func (m *JWTManager) IssueAccessToken(actor Actor) (string, error) {
	if m == nil || len(m.secret) == 0 {
		return "", errors.New("jwt secret is required")
	}
	if !actor.Authenticated() {
		return "", errors.New("actor is not authenticated")
	}
	now := time.Now().UTC()
	claims := accessClaims{
		UserID:   actor.UserID,
		Role:     string(actor.Role),
		DeviceID: actor.DeviceID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

func (m *JWTManager) ParseAccessToken(tokenString string) (Actor, error) {
	if m == nil || len(m.secret) == 0 {
		return Actor{}, errors.New("jwt secret is required")
	}
	var claims accessClaims
	token, err := jwt.ParseWithClaims(tokenString, &claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected jwt signing method")
		}
		return m.secret, nil
	})
	if err != nil {
		return Actor{}, err
	}
	if !token.Valid {
		return Actor{}, errors.New("invalid access token")
	}
	role, err := ParseRole(claims.Role)
	if err != nil {
		return Actor{}, err
	}
	if claims.UserID <= 0 {
		return Actor{}, errors.New("invalid user_id claim")
	}
	return Actor{UserID: claims.UserID, Role: role, DeviceID: claims.DeviceID}, nil
}
