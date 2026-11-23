package stats

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// scriptedGetRedisHook answers every command with the scripted payload,
// standing in for a Redis GET reply without a socket.
type scriptedGetRedisHook struct {
	payload string
}

func (h scriptedGetRedisHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h scriptedGetRedisHook) ProcessHook(redis.ProcessHook) redis.ProcessHook {
	return func(_ context.Context, cmd redis.Cmder) error {
		if sc, ok := cmd.(*redis.StringCmd); ok {
			sc.SetVal(h.payload)
		}
		return nil
	}
}

func (h scriptedGetRedisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func newScriptedGetClient(t *testing.T, payload string) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	client.AddHook(scriptedGetRedisHook{payload: payload})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// TestRedisCacheGetTreatsNullEntryAsMiss covers
// https://github.com/sparklyi/SOJ/issues/161: a JSON `null` cache entry must
// be reported as a miss so SiteFacts falls back to Postgres, while a valid
// all-zero object must still be reported as a hit.
func TestRedisCacheGetTreatsNullEntryAsMiss(t *testing.T) {
	client := newScriptedGetClient(t, "null")
	cache := NewRedisCache(client, time.Minute)

	facts, found, err := cache.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	if found {
		t.Fatalf("Get() found = true for a null cache entry, want false (facts = %+v)", facts)
	}
}

// TestRedisCacheGetRejectsNonJSONWhitespaceBeforeNull ensures a vertical tab
// (not valid JSON whitespace) before "null" is not stripped, so the payload
// still fails to decode and the malformed-cache error path is preserved.
func TestRedisCacheGetRejectsNonJSONWhitespaceBeforeNull(t *testing.T) {
	client := newScriptedGetClient(t, "\vnull")
	cache := NewRedisCache(client, time.Minute)

	facts, found, err := cache.Get(context.Background())
	if err == nil {
		t.Fatalf("Get() returned no error for %q, want decode error (facts = %+v, found = %v)", "\vnull", facts, found)
	}
	if found {
		t.Fatalf("Get() found = true for %q, want false", "\vnull")
	}
}

// TestRedisCacheGetTreatsWhitespaceSurroundedNullAsMiss ensures JSON
// whitespace (spaces, tabs, CR, LF) around a null entry is still trimmed.
func TestRedisCacheGetTreatsWhitespaceSurroundedNullAsMiss(t *testing.T) {
	client := newScriptedGetClient(t, " \t\r\nnull\r\n\t ")
	cache := NewRedisCache(client, time.Minute)

	facts, found, err := cache.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	if found {
		t.Fatalf("Get() found = true for a whitespace-surrounded null cache entry, want false (facts = %+v)", facts)
	}
}

func TestRedisCacheGetKeepsValidAllZeroHit(t *testing.T) {
	client := newScriptedGetClient(t, `{"problems":0,"submissions":0,"languages":0}`)
	cache := NewRedisCache(client, time.Minute)

	facts, found, err := cache.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	if !found {
		t.Fatalf("Get() found = false for a valid all-zero object, want true")
	}
	if facts != (Facts{}) {
		t.Fatalf("facts = %+v, want zero value", facts)
	}
}
