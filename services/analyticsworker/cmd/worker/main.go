package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Techshrr/GoJet/internal/analytics"
	"github.com/Techshrr/GoJet/internal/links"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	mysqlDSN := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	redisAddr := strings.TrimSpace(os.Getenv("GOJET_REDIS_ADDR"))
	if mysqlDSN == "" || redisAddr == "" {
		logger.Error("required configuration missing", "mysql_dsn_present", mysqlDSN != "", "redis_addr_present", redisAddr != "")
		os.Exit(1)
	}

	db, err := links.OpenMySQL(mysqlDSN)
	if err != nil {
		logger.Error("open mysql", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	redisDB := 0
	if raw := strings.TrimSpace(os.Getenv("GOJET_REDIS_DB")); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 0 {
			logger.Error("invalid GOJET_REDIS_DB")
			os.Exit(1)
		}
		redisDB = parsed
	}
	redisClient := links.NewRedisClient(redisAddr, os.Getenv("GOJET_REDIS_PASSWORD"), redisDB)
	defer redisClient.Close()

	group := strings.TrimSpace(os.Getenv("GOJET_ANALYTICS_WORKER_GROUP"))
	if group == "" {
		group = analytics.WorkerGroup
	}
	consumerName := strings.TrimSpace(os.Getenv("GOJET_ANALYTICS_WORKER_CONSUMER"))
	if consumerName == "" {
		hostname, hostErr := os.Hostname()
		if hostErr != nil || strings.TrimSpace(hostname) == "" {
			consumerName = "analyticsworker"
		} else {
			consumerName = hostname
		}
	}
	maxMessages := 0
	if raw := strings.TrimSpace(os.Getenv("GOJET_ANALYTICS_WORKER_MAX_MESSAGES")); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 0 {
			logger.Error("invalid GOJET_ANALYTICS_WORKER_MAX_MESSAGES")
			os.Exit(1)
		}
		maxMessages = parsed
	}

	store := analytics.NewStore(db)
	consumer, err := analytics.NewRedisStreamConsumer(redisClient, group, consumerName)
	if err != nil {
		logger.Error("configure analytics consumer", "error", err)
		os.Exit(1)
	}
	startupCtx, startupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := store.Ping(startupCtx); err != nil {
		startupCancel()
		logger.Error("mysql unavailable", "error", err)
		os.Exit(1)
	}
	if err := redisClient.Ping(startupCtx).Err(); err != nil {
		startupCancel()
		logger.Error("redis unavailable", "error", err)
		os.Exit(1)
	}
	if err := consumer.EnsureGroup(startupCtx); err != nil {
		startupCancel()
		logger.Error("ensure analytics consumer group", "error", err)
		os.Exit(1)
	}
	startupCancel()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger.Info("analyticsworker started", "stream", analytics.ClickStreamKey, "group", group, "consumer", consumerName)

	processed := 0
	for ctx.Err() == nil {
		messages, readErr := consumer.ReadPending(ctx, 100)
		if readErr != nil {
			logger.Error("read pending analytics events", "error", readErr)
			if !sleepContext(ctx, 250*time.Millisecond) {
				break
			}
			continue
		}
		if len(messages) == 0 {
			messages, readErr = consumer.Read(ctx, 100, 2*time.Second)
			if readErr != nil {
				if ctx.Err() != nil {
					break
				}
				logger.Error("read analytics events", "error", readErr)
				if !sleepContext(ctx, 250*time.Millisecond) {
					break
				}
				continue
			}
		}
		for _, message := range messages {
			inserted, persistErr := store.PersistConsumedEvent(ctx, message.Event, message.StreamID, time.Now().UTC())
			if persistErr != nil {
				logger.Error("persist analytics event", "event_id", message.Event.EventID, "stream_id", message.StreamID, "error", persistErr)
				continue
			}
			if ackErr := consumer.Ack(ctx, message.StreamID); ackErr != nil {
				logger.Error("ack analytics event", "event_id", message.Event.EventID, "stream_id", message.StreamID, "error", ackErr)
				continue
			}
			processed++
			logger.Info("analytics event processed", "event_id", message.Event.EventID, "stream_id", message.StreamID, "inserted", inserted, "link_id", message.Event.LinkID, "click_sequence", message.Event.ClickSequence)
			if maxMessages > 0 && processed >= maxMessages {
				logger.Info("analyticsworker message target reached", "processed", processed)
				return
			}
		}
	}
	logger.Info("analyticsworker stopped", "processed", processed)
}

func sleepContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
