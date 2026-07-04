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

type RedisStreamConfig struct {
	Stream   string
	Group    string
	Consumer string
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
	return &RedisStreamQueue{client: client, cfg: cfg}
}

func (q *RedisStreamQueue) Ensure(ctx context.Context) error {
	err := q.client.XGroupCreateMkStream(ctx, q.cfg.Stream, q.cfg.Group, "$").Err()
	if err == nil || strings.Contains(err.Error(), "BUSYGROUP") {
		return nil
	}
	return err
}

func (q *RedisStreamQueue) Publish(ctx context.Context, taskID int64, payload []byte) (string, error) {
	id, err := q.client.XAdd(ctx, &redis.XAddArgs{
		Stream: q.cfg.Stream,
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
