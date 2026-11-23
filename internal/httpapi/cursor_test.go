package httpapi

import (
	"encoding/base64"
	"testing"
	"time"
)

type testCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        int64     `json:"id"`
}

func TestCursorRoundTripPreservesSeekBoundary(t *testing.T) {
	want := testCursor{
		CreatedAt: time.Date(2026, 7, 20, 10, 30, 0, 123000000, time.UTC),
		ID:        42,
	}
	token, err := EncodeCursor(want)
	if err != nil {
		t.Fatalf("EncodeCursor returned error: %v", err)
	}
	var got testCursor
	if err := DecodeCursor(token, &got); err != nil {
		t.Fatalf("DecodeCursor returned error: %v", err)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || got.ID != want.ID {
		t.Fatalf("decoded cursor = %+v, want %+v", got, want)
	}
}

func TestEncodeCursorRejectsUnsupportedPayload(t *testing.T) {
	if _, err := EncodeCursor(make(chan int)); err == nil {
		t.Fatal("EncodeCursor accepted a channel payload")
	}
}

func TestDecodeCursorRejectsEmptyAndMalformedTokens(t *testing.T) {
	for _, raw := range []string{"", "   ", "not-base64"} {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			if err := DecodeCursor(raw, &testCursor{}); err == nil {
				t.Fatalf("DecodeCursor(%q) returned nil error", raw)
			}
		})
	}
}

func TestDecodeCursorRejectsWrongJSONShape(t *testing.T) {
	token := base64.RawURLEncoding.EncodeToString([]byte(`[]`))
	if err := DecodeCursor(token, &testCursor{}); err == nil {
		t.Fatal("DecodeCursor accepted a JSON array for a struct destination")
	}
}
