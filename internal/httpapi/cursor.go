package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// EncodeCursor serializes a cursor payload as an URL-safe opaque token.
func EncodeCursor(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// DecodeCursor decodes an URL-safe cursor token into value.
func DecodeCursor(raw string, value any) error {
	if strings.TrimSpace(raw) == "" {
		return errors.New("cursor is empty")
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, value)
}
