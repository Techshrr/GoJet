package main

import (
	"context"
	"fmt"
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

	store := analytics.NewStore(db)
	publisher := analytics.NewRedisStreamPublisher(redisClient)
	startupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := store.Ping(startupCtx); err != nil {
		cancel()
		logger.Error("mysql unavailable", "error", err)
		os.Exit(1)
	}
	if err := publisher.Ping(startupCtx); err != nil {
		cancel()
		logger.Error("redis unavailable", "error", err)
		os.Exit(1)
	}
	cancel()

	repair := os.Getenv("GOJET_ANALYTICS_RECONCILE_REPAIR") == "1"
	once := os.Getenv("GOJET_ANALYTICS_RECONCILER_ONCE") == "1"
	interval := 60 * time.Second
	if raw := strings.TrimSpace(os.Getenv("GOJET_ANALYTICS_RECONCILER_INTERVAL_SECONDS")); raw != "" {
		seconds, parseErr := strconv.Atoi(raw)
		if parseErr != nil || seconds < 5 || seconds > 3600 {
			logger.Error("invalid GOJET_ANALYTICS_RECONCILER_INTERVAL_SECONDS")
			os.Exit(1)
		}
		interval = time.Duration(seconds) * time.Second
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger.Info("analyticsreconciler started", "stream", analytics.ClickStreamKey, "repair", repair, "once", once, "interval_seconds", int(interval.Seconds()))

	for {
		if err := runCycle(ctx, store, publisher, logger, repair); err != nil {
			logger.Error("analytics reconciliation cycle failed", "error", err)
			if once {
				os.Exit(1)
			}
		} else if once {
			return
		}
		select {
		case <-ctx.Done():
			logger.Info("analyticsreconciler stopped")
			return
		case <-time.After(interval):
		}
	}
}

func runCycle(ctx context.Context, store *analytics.Store, publisher *analytics.RedisStreamPublisher, logger *slog.Logger, repair bool) error {
	recovery, err := store.RecoverUnpublishedOutbox(ctx, publisher, 1000)
	if err != nil {
		return fmt.Errorf("recover unpublished outbox: %w", err)
	}
	runID := fmt.Sprintf("p07-%d", time.Now().UTC().UnixNano())
	reconciliation, err := store.ReconcileAggregates(ctx, runID, repair)
	if err != nil {
		return fmt.Errorf("reconcile aggregates: %w", err)
	}
	outbox, consumed, aggregate, err := store.AuthoritativeTotals(ctx)
	if err != nil {
		return fmt.Errorf("read authoritative totals: %w", err)
	}
	complete := recovery.Failed == 0 && outbox == consumed && consumed == aggregate
	reason := "backlog_or_mismatch"
	if complete {
		reason = "reconciled"
	}
	if err := store.RefreshWorkspaceCompleteness(ctx, complete, reason); err != nil {
		return fmt.Errorf("refresh workspace completeness: %w", err)
	}
	logger.Info("analytics reconciliation cycle",
		"run_id", reconciliation.RunID,
		"outbox_pending", recovery.Pending,
		"outbox_published", recovery.Published,
		"outbox_failed", recovery.Failed,
		"source_event_total", reconciliation.SourceEventTotal,
		"aggregate_before", reconciliation.AggregateTotalBefore,
		"aggregate_after", reconciliation.AggregateTotalAfter,
		"repaired", reconciliation.Repaired,
		"accepted_outbox_total", outbox,
		"consumed_event_total", consumed,
		"aggregate_total", aggregate,
		"workspace_complete", complete,
	)
	return nil
}
