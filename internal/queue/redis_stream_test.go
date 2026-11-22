package queue

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisMessageMappingRejectsInvalidTaskID(t *testing.T) {
	_, err := xMessage(redis.XMessage{ID: "1-0", Values: map[string]any{"task_id": "bad"}})
	if err == nil {
		t.Fatal("expected invalid task id error")
	}
}

func TestRedisMessageMappingPreservesPayloadAndAttempts(t *testing.T) {
	msg, err := xMessage(redis.XMessage{
		ID: "1-0",
		Values: map[string]any{
			"task_id":  "42",
			"payload":  `{"submission_id":7}`,
			"attempts": "2",
		},
	})
	if err != nil {
		t.Fatalf("xMessage returned error: %v", err)
	}
	if msg.TaskID != 42 || string(msg.Payload) != `{"submission_id":7}` || msg.Attempts != 2 {
		t.Fatalf("message = %+v", msg)
	}
}

func TestRedisStreamReadinessFindsConsumerGroup(t *testing.T) {
	groups := []redis.XInfoGroup{{Name: "judge-workers"}, {Name: "other"}}
	if !redisStreamHasGroup(groups, "judge-workers") {
		t.Fatalf("redisStreamHasGroup returned false for existing group")
	}
	if redisStreamHasGroup(groups, "missing") {
		t.Fatalf("redisStreamHasGroup returned true for missing group")
	}
}

func TestRedisStreamQueueImplementsStatsProvider(t *testing.T) {
	var _ QueueStatsProvider = (*RedisStreamQueue)(nil)
}

// TestRedisStreamQueueEnsure verifies idempotent consumer-group creation
// without a socket: every case issues exactly one XGROUP CREATE ... MKSTREAM
// command, a fresh group and a BUSYGROUP reply both succeed, and any other
// error reaches the caller unchanged.
func TestRedisStreamQueueEnsure(t *testing.T) {
	busyGroupErr := errors.New("BUSYGROUP Consumer Group name already exists")
	sentinelErr := errors.New("LOADING Redis is loading the dataset in memory")

	cases := []struct {
		name    string
		stream  string // empty uses "tasks"
		group   string // empty uses "workers"
		startID string // empty exercises the default "$"
		reply   error  // scripted command error; nil means the command succeeds
		wantErr error  // nil means Ensure must succeed
	}{
		{name: "default $ start ID, group created", reply: nil},
		{name: "default $ start ID, BUSYGROUP already initialized", reply: busyGroupErr},
		{name: "default $ start ID, unrelated error preserved", reply: sentinelErr, wantErr: sentinelErr},
		{name: "explicit 0-0 start ID, group created", startID: "0-0", reply: nil},
		{name: "explicit 0-0 start ID, BUSYGROUP already initialized", startID: "0-0", reply: busyGroupErr},
		{name: "explicit 0-0 start ID, unrelated error preserved", startID: "0-0", reply: sentinelErr, wantErr: sentinelErr},
		{name: "custom stream and group names", startID: "0-0", stream: "soj:results", group: "judge-workers", reply: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, hook := newScriptedRedisClient(t, tc.reply)
			stream, group := tc.stream, tc.group
			if stream == "" {
				stream = "tasks"
			}
			if group == "" {
				group = "workers"
			}
			q := NewRedisStreamQueue(client, RedisStreamConfig{
				Stream:  stream,
				Group:   group,
				StartID: tc.startID,
			})

			wantStart := tc.startID
			if wantStart == "" {
				wantStart = "$"
				if q.cfg.StartID != "$" {
					t.Fatalf("default StartID = %q, want %q", q.cfg.StartID, "$")
				}
			}

			err := q.Ensure(context.Background())
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Ensure() error = %v, want %v", err, tc.wantErr)
			}
			hook.requireSingleXGroupCreate(t, stream, group, wantStart)
		})
	}

	t.Run("repeated Ensure reissues XGROUP CREATE and stays successful", func(t *testing.T) {
		// First call creates the group, the second hits BUSYGROUP because the
		// group already exists; both calls must succeed and both commands must
		// be the same XGROUP CREATE ... MKSTREAM.
		client, hook := newScriptedRedisClientReplies(t, nil, busyGroupErr)
		q := NewRedisStreamQueue(client, RedisStreamConfig{Stream: "tasks", Group: "workers"})

		if err := q.Ensure(context.Background()); err != nil {
			t.Fatalf("first Ensure() error = %v, want nil", err)
		}
		if err := q.Ensure(context.Background()); err != nil {
			t.Fatalf("second Ensure() error = %v, want nil for BUSYGROUP", err)
		}
		hook.requireXGroupCreateSequence(t, "tasks", "workers", "$", 2)
	})
}

// scriptedRedisHook records every command and fails it with the scripted
// reply (nil succeeds), so queue code can be driven without a Redis socket.
// When several replies are given they are consumed one per command and the
// last one repeats.
type scriptedRedisHook struct {
	mu       sync.Mutex
	commands [][]any
	replies  []error
}

func newScriptedRedisClient(t *testing.T, reply error) (*redis.Client, *scriptedRedisHook) {
	t.Helper()
	return newScriptedRedisClientReplies(t, reply)
}

func newScriptedRedisClientReplies(t *testing.T, replies ...error) (*redis.Client, *scriptedRedisHook) {
	t.Helper()
	hook := &scriptedRedisHook{replies: replies}
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	client.AddHook(hook)
	t.Cleanup(func() { _ = client.Close() })
	return client, hook
}

func (h *scriptedRedisHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *scriptedRedisHook) ProcessHook(redis.ProcessHook) redis.ProcessHook {
	return func(_ context.Context, cmd redis.Cmder) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.commands = append(h.commands, append([]any(nil), cmd.Args()...))
		if len(h.replies) == 0 {
			return nil
		}
		reply := h.replies[0]
		if len(h.replies) > 1 {
			h.replies = h.replies[1:]
		}
		return reply
	}
}

func (h *scriptedRedisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func xGroupCreateArgs(stream, group, startID string) []any {
	return []any{"xgroup", "create", stream, group, startID, "mkstream"}
}

func (h *scriptedRedisHook) requireSingleXGroupCreate(t *testing.T, stream, group, startID string) {
	t.Helper()
	h.requireXGroupCreateSequence(t, stream, group, startID, 1)
}

func (h *scriptedRedisHook) requireXGroupCreateSequence(t *testing.T, stream, group, startID string, wantCount int) {
	t.Helper()
	want := xGroupCreateArgs(stream, group, startID)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.commands) != wantCount {
		t.Fatalf("recorded commands = %#v, want %d copies of %#v", h.commands, wantCount, want)
	}
	for i, got := range h.commands {
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("command %d = %#v, want %#v", i, got, want)
		}
	}
}

func TestRedisStreamQueueDefaultsRetentionLimits(t *testing.T) {
	q := NewRedisStreamQueue(nil, RedisStreamConfig{})
	if q.cfg.MaxLen != DefaultStreamMaxLen {
		t.Fatalf("MaxLen = %d, want %d", q.cfg.MaxLen, DefaultStreamMaxLen)
	}
	if q.cfg.DeadMaxLen != DefaultDeadStreamMaxLen {
		t.Fatalf("DeadMaxLen = %d, want %d", q.cfg.DeadMaxLen, DefaultDeadStreamMaxLen)
	}
}

func TestRedisStreamPublishUsesApproximateMaxLen(t *testing.T) {
	client, recorder := newRecordingRedisClient(t)
	q := NewRedisStreamQueue(client, RedisStreamConfig{Stream: "tasks", MaxLen: 50})

	if _, err := q.Publish(context.Background(), 42, []byte(`{"status":"queued"}`)); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	requireXAddRetention(t, recorder.singleCommand(t), "tasks", 50)
}

func TestRedisStreamDeadLetterUsesSeparateApproximateMaxLen(t *testing.T) {
	client, recorder := newRecordingRedisClient(t)
	q := NewRedisStreamQueue(client, RedisStreamConfig{
		Stream:     "tasks",
		MaxLen:     50,
		DeadMaxLen: 7,
	})

	if err := q.DeadLetter(context.Background(), Message{ID: "1-0", TaskID: 42, Payload: []byte(`{}`)}, "timeout"); err != nil {
		t.Fatalf("DeadLetter() error = %v", err)
	}

	requireXAddRetention(t, recorder.singleCommand(t), "tasks:dead", 7)
}

func TestRedisStreamStatsFromKnownRedisShape(t *testing.T) {
	now := time.UnixMilli(1_700_000_010_000)
	stats, err := redisQueueStatsFromRedis(
		&redis.XInfoStream{Length: 7},
		nil,
		&redis.XPending{Count: 3, Lower: "1700000000000-0"},
		nil,
		now,
	)
	if err != nil {
		t.Fatalf("redisQueueStatsFromRedis returned error: %v", err)
	}
	if stats.Depth != 7 || stats.Pending != 3 || stats.OldestPendingAge != 10*time.Second {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestRedisStreamStatsTreatMissingStreamOrGroupAsZero(t *testing.T) {
	stats, err := redisQueueStatsFromRedis(nil, redis.Nil, nil, redis.Nil, time.Now())
	if err != nil {
		t.Fatalf("redisQueueStatsFromRedis returned error: %v", err)
	}
	if stats.Depth != 0 || stats.Pending != 0 || stats.OldestPendingAge != 0 {
		t.Fatalf("stats = %+v, want zero values", stats)
	}
}

type redisCommandRecorder struct {
	commands [][]any
}

func newRecordingRedisClient(t *testing.T) (*redis.Client, *redisCommandRecorder) {
	t.Helper()
	recorder := &redisCommandRecorder{}
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	client.AddHook(recorder)
	t.Cleanup(func() { _ = client.Close() })
	return client, recorder
}

func (r *redisCommandRecorder) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (r *redisCommandRecorder) ProcessHook(_ redis.ProcessHook) redis.ProcessHook {
	return func(_ context.Context, cmd redis.Cmder) error {
		r.commands = append(r.commands, append([]any(nil), cmd.Args()...))
		return nil
	}
}

func (r *redisCommandRecorder) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func (r *redisCommandRecorder) singleCommand(t *testing.T) []any {
	t.Helper()
	if len(r.commands) != 1 {
		t.Fatalf("recorded commands = %#v, want exactly one", r.commands)
	}
	return r.commands[0]
}

func requireXAddRetention(t *testing.T, args []any, stream string, maxLen int64) {
	t.Helper()
	want := []any{"xadd", stream, "maxlen", "~", maxLen, "*"}
	if len(args) < len(want) || !reflect.DeepEqual(args[:len(want)], want) {
		t.Fatalf("XADD args = %#v, want prefix %#v", args, want)
	}
}

func TestRedisStreamStatsTreatMissingKeyAndNoGroupAsZero(t *testing.T) {
	missingKey := errors.New("ERR no such key")
	noGroup := errors.New("NOGROUP No such key 'tasks' or consumer group 'judge-workers'")
	for _, tc := range []struct {
		name       string
		info       *redis.XInfoStream
		infoErr    error
		pendingErr error
		wantDepth  int64
	}{
		{name: "stream and group missing", infoErr: missingKey, pendingErr: missingKey},
		{name: "stream missing, stream recreated without group", infoErr: missingKey, pendingErr: noGroup},
		{name: "stream present, group missing", info: &redis.XInfoStream{Length: 4}, pendingErr: noGroup, wantDepth: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stats, err := redisQueueStatsFromRedis(tc.info, tc.infoErr, nil, tc.pendingErr, time.UnixMilli(1_700_000_010_000))
			if err != nil {
				t.Fatalf("redisQueueStatsFromRedis returned error: %v", err)
			}
			if stats.Depth != tc.wantDepth || stats.Pending != 0 {
				t.Fatalf("stats = %+v, want depth %d and no pending", stats, tc.wantDepth)
			}
		})
	}
}

func TestRedisStreamStatsReportsUnrelatedRedisErrors(t *testing.T) {
	loadingErr := errors.New("LOADING Redis is loading the dataset in memory")
	if _, err := redisQueueStatsFromRedis(nil, loadingErr, nil, nil, time.Now()); !errors.Is(err, loadingErr) {
		t.Fatalf("info error = %v, want %v", err, loadingErr)
	}
	typeErr := errors.New("WRONGTYPE Operation against a key holding the wrong kind of value")
	if _, err := redisQueueStatsFromRedis(nil, nil, nil, typeErr, time.Now()); !errors.Is(err, typeErr) {
		t.Fatalf("pending error = %v, want %v", err, typeErr)
	}
	// Only a Redis missing-key reply counts. An error from elsewhere that happens
	// to mention a missing key stays visible.
	wrappedErr := errors.New("vault: no such key in store")
	if _, err := redisQueueStatsFromRedis(nil, wrappedErr, nil, nil, time.Now()); !errors.Is(err, wrappedErr) {
		t.Fatalf("info error = %v, want %v", err, wrappedErr)
	}
}

func TestRedisStreamQueueStatsReadMissingStreamAsEmpty(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	client.AddHook(missingKeyRedisHook{})
	t.Cleanup(func() { _ = client.Close() })
	q := NewRedisStreamQueue(client, RedisStreamConfig{Stream: "tasks", Group: "judge-workers"})

	stats, err := q.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if stats != (QueueStats{}) {
		t.Fatalf("Stats() = %+v, want zero values", stats)
	}
}

type missingKeyRedisHook struct{}

func (missingKeyRedisHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (missingKeyRedisHook) ProcessHook(redis.ProcessHook) redis.ProcessHook {
	return func(context.Context, redis.Cmder) error { return errors.New("ERR no such key") }
}

func (missingKeyRedisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
