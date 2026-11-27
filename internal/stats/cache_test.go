package stats

import (
	"context"
	"encoding/json"
	"errors"
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

// recordingSetRedisHook stands in for a Redis server: it records every command
// it sees and either acknowledges SET or fails with a scripted error.
type recordingSetRedisHook struct {
	cmds []redis.Cmder
	err  error
}

func (h *recordingSetRedisHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *recordingSetRedisHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(_ context.Context, cmd redis.Cmder) error {
		h.cmds = append(h.cmds, cmd)
		if h.err != nil {
			return h.err
		}
		if sc, ok := cmd.(*redis.StatusCmd); ok {
			sc.SetVal("OK")
		}
		return nil
	}
}

func (h *recordingSetRedisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

// newRecordingSetClient builds a client whose commands are answered by hook
// instead of a socket. MaxRetries -1 disables retries, so a scripted failure
// reaches the caller exactly once.
func newRecordingSetClient(t *testing.T, hook *recordingSetRedisHook) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0", MaxRetries: -1})
	client.AddHook(hook)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// TestRedisCacheSetWritesVersionedKeyWithExpiry covers
// https://github.com/sparklyi/SOJ/issues/309: Set must write the encoded facts
// under the current versioned key with the configured whole-second expiry, so
// the TTL fallback that repairs stale counts survives.
func TestRedisCacheSetWritesVersionedKeyWithExpiry(t *testing.T) {
	const ttl = 37 * time.Second
	hook := &recordingSetRedisHook{}
	cache := NewRedisCache(newRecordingSetClient(t, hook), ttl)
	want := Facts{Problems: 12, Submissions: 3456, Languages: 7}

	if err := cache.Set(context.Background(), want); err != nil {
		t.Fatalf("Set() returned error: %v", err)
	}
	if len(hook.cmds) != 1 {
		t.Fatalf("recorded %d commands, want exactly 1", len(hook.cmds))
	}
	cmd := hook.cmds[0]
	if got := cmd.Name(); got != "set" {
		t.Fatalf("command name = %q, want %q", got, "set")
	}
	args := cmd.Args()
	if got := args[1]; got != cacheKey {
		t.Fatalf("SET key = %v, want %q", got, cacheKey)
	}
	payload, ok := args[2].([]byte)
	if !ok {
		t.Fatalf("SET value type = %T, want []byte", args[2])
	}
	var got Facts
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("SET value is not JSON: %v (value = %q)", err, payload)
	}
	if got != want {
		t.Fatalf("SET value = %+v, want %+v", got, want)
	}
	wantSeconds := int64(ttl / time.Second)
	if len(args) != 5 || args[3] != "ex" || args[4] != wantSeconds {
		t.Fatalf("SET expiry args = %v, want [ex %d]", args[3:], wantSeconds)
	}
}

// TestRedisCacheSetPropagatesWriteError covers the failure path of
// https://github.com/sparklyi/SOJ/issues/309: a Redis write failure must reach
// the caller unchanged, so cache-aside can retry the refresh.
func TestRedisCacheSetPropagatesWriteError(t *testing.T) {
	sentinel := errors.New("redis SET failed")
	hook := &recordingSetRedisHook{err: sentinel}
	cache := NewRedisCache(newRecordingSetClient(t, hook), time.Minute)

	err := cache.Set(context.Background(), Facts{Problems: 1})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Set() error = %v, want %v", err, sentinel)
	}
}

// recordingGetRedisHook stands in for a Redis server: it records every command
// it sees and answers it with a scripted error, so the missing-key and
// command-failure paths can be exercised without a socket.
type recordingGetRedisHook struct {
	cmds []redis.Cmder
	err  error
}

func (h *recordingGetRedisHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *recordingGetRedisHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(_ context.Context, cmd redis.Cmder) error {
		h.cmds = append(h.cmds, cmd)
		return h.err
	}
}

func (h *recordingGetRedisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

// TestRedisCacheGetDistinguishesMissFromCommandFailure covers
// https://github.com/sparklyi/SOJ/issues/322: a missing key is a normal cache
// miss, while any other Redis error must reach the caller unchanged, and both
// cases still issue exactly one GET for the versioned key.
func TestRedisCacheGetDistinguishesMissFromCommandFailure(t *testing.T) {
	sentinel := errors.New("redis GET failed")
	tests := []struct {
		name    string
		reply   error
		wantErr error
	}{
		{name: "missing key", reply: redis.Nil},
		{name: "command failure", reply: sentinel, wantErr: sentinel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hook := &recordingGetRedisHook{err: tt.reply}
			client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0", MaxRetries: -1})
			client.AddHook(hook)
			t.Cleanup(func() { _ = client.Close() })
			cache := NewRedisCache(client, time.Minute)

			facts, found, err := cache.Get(context.Background())

			if found {
				t.Fatalf("Get() found = true, want false (facts = %+v)", facts)
			}
			if facts != (Facts{}) {
				t.Fatalf("Get() facts = %+v, want zero value", facts)
			}
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Get() error = %v, want nil for a missing key", err)
				}
			} else if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Get() error = %v, want %v", err, tt.wantErr)
			}

			if len(hook.cmds) != 1 {
				t.Fatalf("recorded %d commands, want exactly 1", len(hook.cmds))
			}
			cmd := hook.cmds[0]
			if got := cmd.Name(); got != "get" {
				t.Fatalf("command name = %q, want %q", got, "get")
			}
			if got := cmd.Args()[1]; got != cacheKey {
				t.Fatalf("GET key = %v, want %q", got, cacheKey)
			}
		})
	}
}
