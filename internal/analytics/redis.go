package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisStreamPublisher struct {
	client *redis.Client
}

type RedisStreamConsumer struct {
	client   *redis.Client
	group    string
	consumer string
}

type StreamMessage struct {
	StreamID string
	Event    Event
}

func NewRedisStreamPublisher(client *redis.Client) *RedisStreamPublisher {
	return &RedisStreamPublisher{client: client}
}

func NewRedisStreamConsumer(client *redis.Client, group, consumer string) (*RedisStreamConsumer, error) {
	group = strings.TrimSpace(group)
	consumer = strings.TrimSpace(consumer)
	if group == "" || consumer == "" || len(group) > 128 || len(consumer) > 128 {
		return nil, ErrInvalidEvent
	}
	return &RedisStreamConsumer{client: client, group: group, consumer: consumer}, nil
}

func (p *RedisStreamPublisher) Ping(ctx context.Context) error {
	return p.client.Ping(ctx).Err()
}

func (p *RedisStreamPublisher) Publish(ctx context.Context, event Event) (string, error) {
	if err := ValidateEvent(event); err != nil {
		return "", err
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return "", err
	}
	return p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: ClickStreamKey,
		Values: map[string]any{
			"event_id": event.EventID,
			"payload":  string(raw),
		},
	}).Result()
}

func (c *RedisStreamConsumer) EnsureGroup(ctx context.Context) error {
	err := c.client.XGroupCreateMkStream(ctx, ClickStreamKey, c.group, "0").Err()
	if err == nil || strings.Contains(err.Error(), "BUSYGROUP") {
		return nil
	}
	return err
}

func (c *RedisStreamConsumer) ReadPending(ctx context.Context, count int64) ([]StreamMessage, error) {
	return c.read(ctx, "0", count, 0)
}

func (c *RedisStreamConsumer) Read(ctx context.Context, count int64, block time.Duration) ([]StreamMessage, error) {
	return c.read(ctx, ">", count, block)
}

func (c *RedisStreamConsumer) read(ctx context.Context, id string, count int64, block time.Duration) ([]StreamMessage, error) {
	if count <= 0 || count > 1000 || block < 0 || (id != "0" && id != ">") {
		return nil, ErrInvalidEvent
	}
	streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    c.group,
		Consumer: c.consumer,
		Streams:  []string{ClickStreamKey, id},
		Count:    count,
		Block:    block,
	}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]StreamMessage, 0, count)
	for _, stream := range streams {
		for _, message := range stream.Messages {
			decoded, decodeErr := decodeMessage(message)
			if decodeErr != nil {
				return nil, decodeErr
			}
			out = append(out, decoded)
		}
	}
	return out, nil
}

func (c *RedisStreamConsumer) Ack(ctx context.Context, streamID string) error {
	streamID = strings.TrimSpace(streamID)
	if streamID == "" {
		return ErrInvalidEvent
	}
	return c.client.XAck(ctx, ClickStreamKey, c.group, streamID).Err()
}

func decodeMessage(message redis.XMessage) (StreamMessage, error) {
	payloadValue, ok := message.Values["payload"]
	if !ok {
		return StreamMessage{}, fmt.Errorf("analytics stream message %s missing payload", message.ID)
	}
	payload, ok := payloadValue.(string)
	if !ok {
		payload = fmt.Sprint(payloadValue)
	}
	var event Event
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return StreamMessage{}, fmt.Errorf("decode analytics stream message %s: %w", message.ID, err)
	}
	if err := ValidateEvent(event); err != nil {
		return StreamMessage{}, fmt.Errorf("validate analytics stream message %s: %w", message.ID, err)
	}
	if rawID, exists := message.Values["event_id"]; exists && fmt.Sprint(rawID) != event.EventID {
		return StreamMessage{}, fmt.Errorf("analytics stream event identity mismatch")
	}
	return StreamMessage{StreamID: message.ID, Event: event}, nil
}
