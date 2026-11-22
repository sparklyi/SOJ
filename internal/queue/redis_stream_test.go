package queue

import (
	"context"
	"errors"
	"reflect"
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
