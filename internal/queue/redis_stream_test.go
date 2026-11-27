package queue

import (
	"context"
	"errors"
	"fmt"
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

func TestRedisStreamQueueReadiness(t *testing.T) {
	const (
		stream = "soj:custom:tasks"
		group  = "custom-workers"
	)

	sentinelErr := errors.New("ERR XINFO GROUPS failed")

	cases := []struct {
		name        string
		groups      []redis.XInfoGroup
		replyErr    error
		wantErr     error
		wantMissing bool
	}{
		{
			name: "configured group exists",
			groups: []redis.XInfoGroup{
				{Name: "other-workers"},
				{Name: group},
			},
		},
		{
			name: "configured group missing",
			groups: []redis.XInfoGroup{
				{Name: "other-workers"},
				{Name: "another-group"},
			},
			wantMissing: true,
		},
		{
			name:     "Redis command fails",
			replyErr: sentinelErr,
			wantErr:  sentinelErr,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, recorder := newRecordingRedisClient(t)

			recorder.groups = tc.groups
			recorder.err = tc.replyErr

			q := NewRedisStreamQueue(client, RedisStreamConfig{
				Stream: stream,
				Group:  group,
			})

			err := q.Ready(context.Background())

			if tc.wantMissing {
				want := fmt.Sprintf(
					"redis stream %s missing consumer group %s",
					stream, group,
				)

				if err == nil || err.Error() != want {
					t.Fatalf("Ready() error = %v, want %q", err, want)
				}
			} else if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Ready() error = %v, want %v", err, tc.wantErr)
			}

			got := recorder.singleCommand(t)
			want := []any{"xinfo", "groups", stream}

			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Ready() command = %#v, want %#v", got, want)
			}
		})
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

func TestRedisStreamQueueAckTargetsConfiguredStreamAndGroup(t *testing.T) {
	const (
		stream    = "soj:custom:results"
		group     = "result-consumers"
		messageID = "1700000000123-4"
	)
	client, hook := newScriptedRedisClient(t, nil)
	q := NewRedisStreamQueue(client, RedisStreamConfig{Stream: stream, Group: group})

	if err := q.Ack(context.Background(), messageID); err != nil {
		t.Fatalf("Ack() error = %v, want nil", err)
	}

	hook.mu.Lock()
	defer hook.mu.Unlock()
	want := [][]any{{"xack", stream, group, messageID}}
	if !reflect.DeepEqual(hook.commands, want) {
		t.Fatalf("Ack() commands = %#v, want %#v", hook.commands, want)
	}
}

func TestRedisStreamQueueAckPropagatesCommandError(t *testing.T) {
	sentinelErr := errors.New("ERR XACK failed")
	client, _ := newScriptedRedisClient(t, sentinelErr)
	q := NewRedisStreamQueue(client, RedisStreamConfig{
		Stream: "soj:custom:results",
		Group:  "result-consumers",
	})

	if err := q.Ack(context.Background(), "1700000000123-4"); !errors.Is(err, sentinelErr) {
		t.Fatalf("Ack() error = %v, want %v", err, sentinelErr)
	}
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

func TestRedisStreamPublishPreservesPayloadAndEntryID(t *testing.T) {
	client, recorder := newRecordingRedisClient(t)

	q := NewRedisStreamQueue(client, RedisStreamConfig{
		Stream: "judge:custom",
		MaxLen: 50,
	})

	taskID := int64(5_000_000_000)
	payload := []byte(`{"message":"Привет"}`)
	recorder.entryID = "1700000000000-1"

	id, err := q.Publish(context.Background(), taskID, payload)

	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	if id != recorder.entryID {
		t.Fatalf("Publish() ID = %q, want %q", id, recorder.entryID)
	}

	args := recorder.singleCommand(t)
	requireXAddRetention(t, args, "judge:custom", 50)

	if len(args) != 10 {
		t.Fatalf("unexpected XADD arguments: %#v", args)
	}

	got := make(map[string]any)

	for i := 6; i < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			t.Fatalf("unexpected field name: %#v", args[i])
		}
		got[key] = args[i+1]
	}

	want := map[string]any{
		"task_id": "5000000000",
		"payload": string(payload),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("XADD fields = %#v, want %#v", got, want)
	}
}

func TestRedisStreamPublishReturnsWriteError(t *testing.T) {
	client, recorder := newRecordingRedisClient(t)

	q := NewRedisStreamQueue(client, RedisStreamConfig{
		Stream: "judge:custom",
		MaxLen: 50,
	})

	writeErr := errors.New("redis write failed")
	recorder.err = writeErr

	id, err := q.Publish(
		context.Background(),
		42,
		[]byte(`{"status":"queued"}`),
	)

	if id != "" {
		t.Fatalf("Publish() ID = %q, want empty string", id)
	}

	if !errors.Is(err, writeErr) {
		t.Fatalf("Publish() error = %v, want %v", err, writeErr)
	}

	requireXAddRetention(t, recorder.singleCommand(t), "judge:custom", 50)
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

func TestRedisStreamDeadLetterPreservesMessageFields(t *testing.T) {
	client, recorder := newRecordingRedisClient(t)
	q := NewRedisStreamQueue(client, RedisStreamConfig{
		Stream:     "tasks",
		MaxLen:     50,
		DeadMaxLen: 7,
	})
	payload := []byte("payload-очередь")
	reason := "timed out: 超时"
	message := Message{ID: "9-1", TaskID: 42, Payload: payload, Attempts: 3}

	if err := q.DeadLetter(context.Background(), message, reason); err != nil {
		t.Fatalf("DeadLetter() error = %v", err)
	}

	got := xaddFieldValues(t, recorder.singleCommand(t), "tasks:dead", 7)
	want := map[string]string{
		"original_id": "9-1",
		"task_id":     "42",
		"payload":     string(payload),
		"attempts":    "3",
		"reason":      reason,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("XADD fields = %#v, want %#v", got, want)
	}
}

func TestRedisStreamDeadLetterReturnsRecorderWriteError(t *testing.T) {
	sentinelErr := errors.New("sentinel dead-letter write")
	client, recorder := newRecordingRedisClient(t)
	recorder.err = sentinelErr
	q := NewRedisStreamQueue(client, RedisStreamConfig{Stream: "tasks", DeadMaxLen: 7})

	err := q.DeadLetter(context.Background(), Message{ID: "9-1", TaskID: 42, Payload: []byte("payload-очередь"), Attempts: 3}, "timed out: 超时")
	if !errors.Is(err, sentinelErr) {
		t.Fatalf("DeadLetter() error = %v, want %v", err, sentinelErr)
	}
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

func TestRedisStreamStatsClampUnusablePendingTimestamps(t *testing.T) {
	now := time.UnixMilli(1_700_000_010_000)
	future := now.Add(5 * time.Second).UnixMilli()

	cases := []struct {
		name  string
		lower string
	}{
		{name: "empty id", lower: ""},
		{name: "no millisecond separator", lower: "5"},
		{name: "nonnumeric timestamp", lower: "not-a-number-0"},
		{name: "exactly at now", lower: fmt.Sprintf("%d-0", now.UnixMilli())},
		{name: "later than now", lower: fmt.Sprintf("%d-0", future)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stats, err := redisQueueStatsFromRedis(
				&redis.XInfoStream{Length: 9},
				nil,
				&redis.XPending{Count: 4, Lower: tc.lower},
				nil,
				now,
			)
			if err != nil {
				t.Fatalf("redisQueueStatsFromRedis returned error: %v", err)
			}
			if stats.Depth != 9 || stats.Pending != 4 || stats.OldestPendingAge != 0 {
				t.Fatalf("stats = %+v, want depth 9, pending 4 and zero oldest pending age", stats)
			}
		})
	}
}

type redisCommandRecorder struct {
	commands [][]any
	err      error
	entryID  string
	groups   []redis.XInfoGroup
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

		if r.err != nil {
			return r.err
		}

		if cmd.Name() == "xadd" {
			if xadd, ok := cmd.(*redis.StringCmd); ok {
				xadd.SetVal(r.entryID)
			}
		}

		if groups, ok := cmd.(*redis.XInfoGroupsCmd); ok {
			groups.SetVal(r.groups)
		}

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

func xaddFieldValues(t *testing.T, args []any, stream string, maxLen int64) map[string]string {
	t.Helper()
	requireXAddRetention(t, args, stream, maxLen)
	tail := args[6:]
	if len(tail)%2 != 0 {
		t.Fatalf("XADD field args = %#v, want an even key/value count", tail)
	}
	got := make(map[string]string, len(tail)/2)
	for i := 0; i < len(tail); i += 2 {
		key, ok := tail[i].(string)
		if !ok {
			t.Fatalf("XADD field name = %#v, want string", tail[i])
		}
		if _, exists := got[key]; exists {
			t.Fatalf("XADD duplicate field %q in %#v", key, tail)
		}
		value, ok := tail[i+1].(string)
		if !ok {
			t.Fatalf("XADD field %q value = %#v, want string", key, tail[i+1])
		}
		got[key] = value
	}
	return got
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

func (missingKeyRedisHook) ProcessPipelineHook(redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(_ context.Context, cmds []redis.Cmder) error {
		for _, cmd := range cmds {
			cmd.SetErr(errors.New("ERR no such key"))
		}
		return nil
	}
}

// scriptedAutoClaimHook answers XAUTOCLAIM commands from a fixed script, one
// step per command, and records the start ID each command was sent with.
type scriptedAutoClaimHook struct {
	mu     sync.Mutex
	steps  []autoClaimStep
	starts []string
}

type autoClaimStep struct {
	messages []redis.XMessage
	next     string
	err      error
}

func (h *scriptedAutoClaimHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *scriptedAutoClaimHook) ProcessHook(redis.ProcessHook) redis.ProcessHook {
	return func(_ context.Context, cmd redis.Cmder) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		args := cmd.Args()
		if len(args) < 6 || fmt.Sprint(args[0]) != "xautoclaim" {
			return nil
		}
		h.starts = append(h.starts, fmt.Sprint(args[5]))
		if len(h.steps) == 0 {
			return fmt.Errorf("unexpected XAUTOCLAIM with no scripted reply")
		}
		step := h.steps[0]
		h.steps = h.steps[1:]
		autoClaim, ok := cmd.(*redis.XAutoClaimCmd)
		if !ok {
			return fmt.Errorf("unexpected command type %T", cmd)
		}
		if step.err != nil {
			autoClaim.SetErr(step.err)
			return step.err
		}
		autoClaim.SetVal(step.messages, step.next)
		return nil
	}
}

func (h *scriptedAutoClaimHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

// TestRedisStreamClaimStaleResumesFromScanCursor drives ClaimStale through an
// empty page with a nonzero cursor, a failed call, and a successful page that
// returns the terminal "0-0" cursor. Each call must start where the previous
// successful page left off, and a failed call must retry from the same cursor.
func TestRedisStreamClaimStaleResumesFromScanCursor(t *testing.T) {
	sentinelErr := errors.New("LOADING Redis is loading the dataset in memory")
	hook := &scriptedAutoClaimHook{steps: []autoClaimStep{
		{next: "5-0"},
		{err: sentinelErr},
		{messages: []redis.XMessage{{ID: "7-0", Values: map[string]any{"task_id": "42", "payload": `{}`, "attempts": "1"}}}, next: "0-0"},
		{next: "0-0"},
	}}
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	client.AddHook(hook)
	t.Cleanup(func() { _ = client.Close() })
	q := NewRedisStreamQueue(client, RedisStreamConfig{Stream: "tasks", Group: "workers", Consumer: "c1"})

	ctx := context.Background()
	if msgs, err := q.ClaimStale(ctx, time.Minute, 1); err != nil || len(msgs) != 0 {
		t.Fatalf("first ClaimStale() = %v, %v; want empty page", msgs, err)
	}
	if _, err := q.ClaimStale(ctx, time.Minute, 1); !errors.Is(err, sentinelErr) {
		t.Fatalf("second ClaimStale() error = %v, want %v", err, sentinelErr)
	}
	msgs, err := q.ClaimStale(ctx, time.Minute, 1)
	if err != nil || len(msgs) != 1 || msgs[0].ID != "7-0" {
		t.Fatalf("third ClaimStale() = %+v, %v; want message 7-0", msgs, err)
	}
	if _, err := q.ClaimStale(ctx, time.Minute, 1); err != nil {
		t.Fatalf("fourth ClaimStale() error = %v", err)
	}

	want := []string{"0-0", "5-0", "5-0", "0-0"}
	hook.mu.Lock()
	defer hook.mu.Unlock()
	if !reflect.DeepEqual(hook.starts, want) {
		t.Fatalf("XAUTOCLAIM start IDs = %q, want %q", hook.starts, want)
	}
}

func TestRedisStreamClaimStaleKeepsCursorWhenMessageConversionFails(t *testing.T) {
	hook := &scriptedAutoClaimHook{steps: []autoClaimStep{
		{messages: []redis.XMessage{{ID: "1-0", Values: map[string]any{"task_id": "bad"}}}, next: "5-0"},
		{messages: []redis.XMessage{{ID: "2-0", Values: map[string]any{"task_id": "42", "payload": `{}`}}}, next: "0-0"},
	}}
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	client.AddHook(hook)
	t.Cleanup(func() { _ = client.Close() })
	q := NewRedisStreamQueue(client, RedisStreamConfig{Stream: "tasks", Group: "workers", Consumer: "c1"})

	ctx := context.Background()
	if _, err := q.ClaimStale(ctx, time.Minute, 1); err == nil {
		t.Fatal("ClaimStale() error = nil, want invalid task_id error")
	}
	msgs, err := q.ClaimStale(ctx, time.Minute, 1)
	if err != nil || len(msgs) != 1 || msgs[0].ID != "2-0" {
		t.Fatalf("second ClaimStale() = %+v, %v; want message 2-0", msgs, err)
	}

	hook.mu.Lock()
	defer hook.mu.Unlock()
	want := []string{"0-0", "0-0"}
	if !reflect.DeepEqual(hook.starts, want) {
		t.Fatalf("XAUTOCLAIM start IDs = %q, want %q", hook.starts, want)
	}
}

func TestRedisStreamQueueStatsUsesPipeline(t *testing.T) {
	pendingTimestamp := time.Now().Add(-10 * time.Second).UnixMilli()
	pendingTime := time.UnixMilli(pendingTimestamp)

	hook := &redisStatsPipelineHook{
		info: &redis.XInfoStream{Length: 7},
		pending: &redis.XPending{
			Count: 3,
			Lower: fmt.Sprintf("%d-0", pendingTimestamp),
		},
	}

	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	client.AddHook(hook)
	t.Cleanup(func() { _ = client.Close() })

	q := NewRedisStreamQueue(client, RedisStreamConfig{
		Stream: "tasks",
		Group:  "workers",
	})

	before := time.Now()
	stats, err := q.Stats(context.Background())
	after := time.Now()

	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}

	if stats.Depth != 7 || stats.Pending != 3 {
		t.Fatalf("Stats() = %+v, want depth 7 and pending 3", stats)
	}

	minAge := before.Sub(pendingTime)
	maxAge := after.Sub(pendingTime)

	if stats.OldestPendingAge < minAge || stats.OldestPendingAge > maxAge {
		t.Fatalf(
			"OldestPendingAge = %v, want between %v and %v",
			stats.OldestPendingAge,
			minAge,
			maxAge,
		)
	}

	if len(hook.commands) != 2 {
		t.Fatalf("pipeline commands = %#v, want exactly 2", hook.commands)
	}
	if hook.pipelineCalls != 1 {
		t.Fatalf("pipeline hook calls = %d, want exactly 1", hook.pipelineCalls)
	}
	want := [][]any{
		{"xinfo", "stream", "tasks"},
		{"xpending", "tasks", "workers"},
	}

	if !reflect.DeepEqual(hook.commands, want) {
		t.Fatalf("pipeline commands = %#v, want %#v", hook.commands, want)
	}
}

func TestRedisMessagesMapping(t *testing.T) {
	makeMessage := func(id, taskID, payload, attempts string) redis.XMessage {
		return redis.XMessage{
			ID: id,
			Values: map[string]any{
				"task_id":  taskID,
				"payload":  payload,
				"attempts": attempts,
			},
		}
	}

	t.Run("single stream preserves all messages in order", func(t *testing.T) {
		streams := []redis.XStream{
			{Messages: []redis.XMessage{
				makeMessage("1-0", "1", "first", "0"),
				makeMessage("2-0", "2", "second", "1"),
				makeMessage("3-0", "3", "third", "2"),
			}},
		}

		messages, err := redisMessages(streams)
		if err != nil {
			t.Fatalf("redisMessages returned error: %v", err)
		}

		want := []Message{
			{ID: "1-0", TaskID: 1, Payload: []byte("first"), Attempts: 0},
			{ID: "2-0", TaskID: 2, Payload: []byte("second"), Attempts: 1},
			{ID: "3-0", TaskID: 3, Payload: []byte("third"), Attempts: 2},
		}

		if !reflect.DeepEqual(messages, want) {
			t.Fatalf("messages = %+v, want %+v", messages, want)
		}
	})

	t.Run("no streams", func(t *testing.T) {
		messages, err := redisMessages(nil)
		if err != nil {
			t.Fatalf("redisMessages returned error: %v", err)
		}
		if messages != nil {
			t.Fatalf("messages = %+v, want nil", messages)
		}
	})

	t.Run("empty stream", func(t *testing.T) {
		messages, err := redisMessages([]redis.XStream{{Messages: nil}})
		if err != nil {
			t.Fatalf("redisMessages returned error: %v", err)
		}
		if messages != nil {
			t.Fatalf("messages = %+v, want nil", messages)
		}
	})

	t.Run("preserves order across streams", func(t *testing.T) {
		streams := []redis.XStream{
			{Messages: []redis.XMessage{
				makeMessage("1-0", "1", "first", "0"),
				makeMessage("2-0", "2", "second", "1"),
			}},
			{Messages: []redis.XMessage{
				makeMessage("3-0", "3", "third", "2"),
			}},
		}

		messages, err := redisMessages(streams)
		if err != nil {
			t.Fatalf("redisMessages returned error: %v", err)
		}

		want := []Message{
			{ID: "1-0", TaskID: 1, Payload: []byte("first"), Attempts: 0},
			{ID: "2-0", TaskID: 2, Payload: []byte("second"), Attempts: 1},
			{ID: "3-0", TaskID: 3, Payload: []byte("third"), Attempts: 2},
		}

		if !reflect.DeepEqual(messages, want) {
			t.Fatalf("messages = %+v, want %+v", messages, want)
		}
	})

	t.Run("conversion failure after valid message", func(t *testing.T) {
		streams := []redis.XStream{
			{Messages: []redis.XMessage{
				makeMessage("1-0", "1", "valid", "0"),
				makeMessage("2-0", "bad", "invalid", "0"),
			}},
		}

		messages, err := redisMessages(streams)
		if err == nil {
			t.Fatal("redisMessages error = nil, want conversion error")
		}
		if messages != nil {
			t.Fatalf("messages = %+v, want nil on conversion error", messages)
		}
	})
}
func BenchmarkRedisMessages(b *testing.B) {
	benchmarks := []struct {
		name  string
		count int
	}{
		{name: "single_message", count: 1},
		{name: "batch_100_messages", count: 100},
	}

	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			items := make([]redis.XMessage, benchmark.count)

			for i := range items {
				items[i] = redis.XMessage{
					ID: "1-0",
					Values: map[string]any{
						"task_id":  "123",
						"payload":  "benchmark payload",
						"attempts": "1",
					},
				}
			}

			streams := []redis.XStream{
				{Messages: items},
			}

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, err := redisMessages(streams)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

type redisStatsPipelineHook struct {
	commands      [][]any
	pipelineCalls int
	info          *redis.XInfoStream
	pending       *redis.XPending
	infoErr       error
	pendingErr    error
	pipelineErr   error
}

func (h *redisStatsPipelineHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (h *redisStatsPipelineHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return next
}

func (h *redisStatsPipelineHook) ProcessPipelineHook(redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(_ context.Context, cmds []redis.Cmder) error {
		h.pipelineCalls++

		for _, cmd := range cmds {
			h.commands = append(h.commands, append([]any(nil), cmd.Args()...))

			switch c := cmd.(type) {
			case *redis.XInfoStreamCmd:
				c.SetVal(h.info)
				c.SetErr(h.infoErr)
			case *redis.XPendingCmd:
				c.SetVal(h.pending)
				c.SetErr(h.pendingErr)
			default:
				return fmt.Errorf("unexpected pipeline command type %T", cmd)
			}
		}
		return h.pipelineErr
	}
}
func TestRedisStreamQueueStatsReturnsPipelineError(t *testing.T) {
	pipelineErr := errors.New("redis: connection reset")
	hook := &redisStatsPipelineHook{
		pipelineErr: pipelineErr,
	}

	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	client.AddHook(hook)
	t.Cleanup(func() { _ = client.Close() })

	q := NewRedisStreamQueue(client, RedisStreamConfig{
		Stream: "tasks",
		Group:  "workers",
	})

	_, err := q.Stats(context.Background())
	if !errors.Is(err, pipelineErr) {
		t.Fatalf("expected pipeline error %v, got %v", pipelineErr, err)
	}
}
func TestRedisStreamQueueStatsPreservesContextCancellation(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	t.Cleanup(func() { _ = client.Close() })

	q := NewRedisStreamQueue(client, RedisStreamConfig{
		Stream: "tasks",
		Group:  "workers",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := q.Stats(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Stats() error = %v, want %v", err, context.Canceled)
	}
}

func TestRedisStreamQueueStatsDoesNotHidePipelineErrors(t *testing.T) {
	missingErr := errors.New("ERR no such key")
	actionableErr := errors.New("LOADING Redis is loading the dataset")

	cases := []struct {
		name       string
		infoErr    error
		pendingErr error
	}{
		{
			name:       "missing stream and actionable pending error",
			infoErr:    missingErr,
			pendingErr: actionableErr,
		},
		{
			name:       "actionable stream error and missing group",
			infoErr:    actionableErr,
			pendingErr: errors.New("NOGROUP No such key"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hook := &redisStatsPipelineHook{
				infoErr:     tc.infoErr,
				pendingErr:  tc.pendingErr,
				pipelineErr: tc.infoErr,
			}

			client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
			client.AddHook(hook)
			t.Cleanup(func() { _ = client.Close() })

			q := NewRedisStreamQueue(client, RedisStreamConfig{
				Stream: "tasks",
				Group:  "workers",
			})

			_, err := q.Stats(context.Background())

			if !errors.Is(err, actionableErr) {
				t.Fatalf("Stats() error = %v, want actionable error %v", err, actionableErr)
			}
		})
	}
}
