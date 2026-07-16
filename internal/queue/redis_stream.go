package queue

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// DefaultStreamMaxLen bounds retained request and result stream history.
	DefaultStreamMaxLen int64 = 100_000
	// DefaultDeadStreamMaxLen bounds retained dead-letter stream history.
	DefaultDeadStreamMaxLen int64 = 10_000
)

type RedisStreamConfig struct {
	Stream     string
	Group      string
	Consumer   string
	StartID    string
	MaxLen     int64
	DeadMaxLen int64
}

type RedisStreamQueue struct {
	client redis.UniversalClient
	cfg    RedisStreamConfig
}

func NewRedisStreamQueue(client redis.UniversalClient, cfg RedisStreamConfig) *RedisStreamQueue {
	if cfg.Stream == "" {
		cfg.Stream = "soj:judge:tasks"
	}
	if cfg.Group == "" {
		cfg.Group = "judge-workers"
	}
	if cfg.Consumer == "" {
		cfg.Consumer = "worker"
	}
	if cfg.StartID == "" {
		cfg.StartID = "$"
	}
	if cfg.MaxLen <= 0 {
		cfg.MaxLen = DefaultStreamMaxLen
	}
	if cfg.DeadMaxLen <= 0 {
		cfg.DeadMaxLen = DefaultDeadStreamMaxLen
	}
	return &RedisStreamQueue{client: client, cfg: cfg}
}

func (q *RedisStreamQueue) Ensure(ctx context.Context) error {
	err := q.client.XGroupCreateMkStream(ctx, q.cfg.Stream, q.cfg.Group, q.cfg.StartID).Err()
	if err == nil || strings.Contains(err.Error(), "BUSYGROUP") {
		return nil
	}
	return err
}

func (q *RedisStreamQueue) Ready(ctx context.Context) error {
	groups, err := q.client.XInfoGroups(ctx, q.cfg.Stream).Result()
	if err != nil {
		return err
	}
	if !redisStreamHasGroup(groups, q.cfg.Group) {
		return fmt.Errorf("redis stream %s missing consumer group %s", q.cfg.Stream, q.cfg.Group)
	}
	return nil
}

func (q *RedisStreamQueue) Stats(ctx context.Context) (QueueStats, error) {
	info, infoErr := q.client.XInfoStream(ctx, q.cfg.Stream).Result()
	pending, pendingErr := q.client.XPending(ctx, q.cfg.Stream, q.cfg.Group).Result()
	return redisQueueStatsFromRedis(info, infoErr, pending, pendingErr, time.Now())
}

func (q *RedisStreamQueue) Publish(ctx context.Context, taskID int64, payload []byte) (string, error) {
	id, err := q.client.XAdd(ctx, &redis.XAddArgs{
		Stream: q.cfg.Stream,
		MaxLen: q.cfg.MaxLen,
		Approx: true,
		Values: map[string]any{
			"task_id": strconv.FormatInt(taskID, 10),
			"payload": string(payload),
		},
	}).Result()
	if err != nil {
		return "", err
	}
	return id, nil
}

func (q *RedisStreamQueue) Consume(ctx context.Context, limit int, block time.Duration) ([]Message, error) {
	if limit <= 0 {
		limit = 1
	}
	streams, err := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    q.cfg.Group,
		Consumer: q.cfg.Consumer,
		Streams:  []string{q.cfg.Stream, ">"},
		Count:    int64(limit),
		Block:    block,
	}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return redisMessages(streams)
}

func (q *RedisStreamQueue) ClaimStale(ctx context.Context, minIdle time.Duration, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 1
	}
	messages, _, err := q.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   q.cfg.Stream,
		Group:    q.cfg.Group,
		Consumer: q.cfg.Consumer,
		MinIdle:  minIdle,
		Start:    "0-0",
		Count:    int64(limit),
	}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return xMessages(messages)
}

func (q *RedisStreamQueue) Ack(ctx context.Context, messageID string) error {
	return q.client.XAck(ctx, q.cfg.Stream, q.cfg.Group, messageID).Err()
}

func (q *RedisStreamQueue) DeadLetter(ctx context.Context, message Message, reason string) error {
	_, err := q.client.XAdd(ctx, &redis.XAddArgs{
		Stream: q.cfg.Stream + ":dead",
		MaxLen: q.cfg.DeadMaxLen,
		Approx: true,
		Values: map[string]any{
			"original_id": message.ID,
			"task_id":     strconv.FormatInt(message.TaskID, 10),
			"payload":     string(message.Payload),
			"attempts":    strconv.Itoa(message.Attempts),
			"reason":      reason,
		},
	}).Result()
	return err
}

func (q *RedisStreamQueue) Close() error {
	return q.client.Close()
}

func redisMessages(streams []redis.XStream) ([]Message, error) {
	var messages []Message
	for _, stream := range streams {
		items, err := xMessages(stream.Messages)
		if err != nil {
			return nil, err
		}
		messages = append(messages, items...)
	}
	return messages, nil
}

func xMessages(items []redis.XMessage) ([]Message, error) {
	messages := make([]Message, 0, len(items))
	for _, item := range items {
		message, err := xMessage(item)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, nil
}

func xMessage(item redis.XMessage) (Message, error) {
	taskID, err := strconv.ParseInt(fmt.Sprint(item.Values["task_id"]), 10, 64)
	if err != nil {
		return Message{}, fmt.Errorf("invalid task_id in stream message %s: %w", item.ID, err)
	}
	attempts := 0
	if value, ok := item.Values["attempts"]; ok {
		attempts, _ = strconv.Atoi(fmt.Sprint(value))
	}
	return Message{
		ID:       item.ID,
		TaskID:   taskID,
		Payload:  []byte(fmt.Sprint(item.Values["payload"])),
		Attempts: attempts,
	}, nil
}

func redisStreamHasGroup(groups []redis.XInfoGroup, name string) bool {
	for _, group := range groups {
		if group.Name == name {
			return true
		}
	}
	return false
}

func redisQueueStatsFromRedis(info *redis.XInfoStream, infoErr error, pending *redis.XPending, pendingErr error, now time.Time) (QueueStats, error) {
	var stats QueueStats
	if infoErr != nil && !redisQueueStatsMissing(infoErr) {
		return QueueStats{}, infoErr
	}
	if infoErr == nil && info != nil {
		stats.Depth = info.Length
	}
	if pendingErr != nil && !redisQueueStatsMissing(pendingErr) {
		return QueueStats{}, pendingErr
	}
	if pendingErr == nil && pending != nil {
		stats.Pending = pending.Count
		stats.OldestPendingAge = redisStreamIDAge(pending.Lower, now)
	}
	return stats, nil
}

func redisQueueStatsMissing(err error) bool {
	return errors.Is(err, redis.Nil) || strings.Contains(strings.ToUpper(err.Error()), "NOGROUP")
}

func redisStreamIDAge(id string, now time.Time) time.Duration {
	if id == "" {
		return 0
	}
	milliseconds, _, ok := strings.Cut(id, "-")
	if !ok {
		return 0
	}
	value, err := strconv.ParseInt(milliseconds, 10, 64)
	if err != nil {
		return 0
	}
	age := now.Sub(time.UnixMilli(value))
	if age < 0 {
		return 0
	}
	return age
}
