package analytics

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type EventPublisher interface {
	Publish(context.Context, Event) (string, error)
}

type OutboxRecoveryResult struct {
	Pending   int `json:"pending"`
	Published int `json:"published"`
	Failed    int `json:"failed"`
}

type ReconciliationResult struct {
	RunID                string `json:"run_id"`
	SourceEventTotal     uint64 `json:"source_event_total"`
	AggregateTotalBefore uint64 `json:"aggregate_total_before"`
	AggregateTotalAfter  uint64 `json:"aggregate_total_after"`
	MismatchBefore       bool   `json:"mismatch_before"`
	Repaired             bool   `json:"repaired"`
}

func (s *Store) RecoverUnpublishedOutbox(ctx context.Context, publisher EventPublisher, limit int) (OutboxRecoveryResult, error) {
	if publisher == nil || limit <= 0 || limit > 1000 {
		return OutboxRecoveryResult{}, ErrInvalidEvent
	}
	events, err := s.PendingOutbox(ctx, limit)
	if err != nil {
		return OutboxRecoveryResult{}, err
	}
	result := OutboxRecoveryResult{Pending: len(events)}
	for _, event := range events {
		streamID, publishErr := publisher.Publish(ctx, event)
		if publishErr != nil {
			result.Failed++
			_ = s.RecordOutboxPublishFailure(ctx, event.EventID, publishErr)
			continue
		}
		if err := s.MarkOutboxPublished(ctx, event.EventID, streamID, time.Now().UTC()); err != nil {
			// Publication may already have succeeded. Leaving the outbox row pending
			// causes a safe later replay; deterministic event IDs make that replay
			// logical-once at analytics_events/aggregate persistence.
			result.Failed++
			continue
		}
		result.Published++
	}
	return result, nil
}

func (s *Store) ReconcileAggregates(ctx context.Context, runID string, repair bool) (ReconciliationResult, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" || len(runID) > 64 {
		return ReconciliationResult{}, ErrInvalidQuery
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ReconciliationResult{}, err
	}
	defer tx.Rollback()

	var sourceTotal, aggregateBefore uint64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM analytics_events`).Scan(&sourceTotal); err != nil {
		return ReconciliationResult{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(clicks), 0) FROM analytics_hourly_aggregates`).Scan(&aggregateBefore); err != nil {
		return ReconciliationResult{}, err
	}
	mismatch := sourceTotal != aggregateBefore
	repaired := false
	if repair && mismatch {
		if _, err := tx.ExecContext(ctx, `DELETE FROM analytics_hourly_aggregates`); err != nil {
			return ReconciliationResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO analytics_hourly_aggregates (
				workspace_id, link_id, bucket_start, country_code, device,
				language, source_hostname, campaign_id, clicks
			)
			SELECT workspace_id, link_id,
			       STR_TO_DATE(DATE_FORMAT(occurred_at, '%Y-%m-%d %H:00:00.000000'), '%Y-%m-%d %H:%i:%s.%f'),
			       country_code, device, language, source_hostname, campaign_id, COUNT(*)
			FROM analytics_events
			GROUP BY workspace_id, link_id,
			         STR_TO_DATE(DATE_FORMAT(occurred_at, '%Y-%m-%d %H:00:00.000000'), '%Y-%m-%d %H:%i:%s.%f'),
			         country_code, device, language, source_hostname, campaign_id`); err != nil {
			return ReconciliationResult{}, err
		}
		repaired = true
	}

	var aggregateAfter uint64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(clicks), 0) FROM analytics_hourly_aggregates`).Scan(&aggregateAfter); err != nil {
		return ReconciliationResult{}, err
	}
	metadata, err := json.Marshal(map[string]any{
		"mismatch_before": mismatch,
		"repair_requested": repair,
		"source": "analytics_events",
	})
	if err != nil {
		return ReconciliationResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO analytics_reconciliation_runs (
			run_id, scope, source_event_total, aggregate_total_before,
			aggregate_total_after, repaired, metadata_json
		) VALUES (?, 'global-hourly', ?, ?, ?, ?, ?)`,
		runID, sourceTotal, aggregateBefore, aggregateAfter, repaired, string(metadata),
	); err != nil {
		return ReconciliationResult{}, fmt.Errorf("record reconciliation run: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ReconciliationResult{}, err
	}
	return ReconciliationResult{
		RunID: runID,
		SourceEventTotal: sourceTotal,
		AggregateTotalBefore: aggregateBefore,
		AggregateTotalAfter: aggregateAfter,
		MismatchBefore: mismatch,
		Repaired: repaired,
	}, nil
}

func (s *Store) AuthoritativeTotals(ctx context.Context) (outboxAccepted, consumedEvents, aggregateClicks uint64, err error) {
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM analytics_outbox`).Scan(&outboxAccepted); err != nil {
		return
	}
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM analytics_events`).Scan(&consumedEvents); err != nil {
		return
	}
	if err = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(clicks), 0) FROM analytics_hourly_aggregates`).Scan(&aggregateClicks); err != nil {
		return
	}
	return
}

func (s *Store) RefreshWorkspaceCompleteness(ctx context.Context, complete bool, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 160 {
		return ErrInvalidQuery
	}
	status := DatasetPartial
	if complete {
		status = DatasetComplete
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO analytics_workspace_state (
			workspace_id, status, data_through_at, retention_days, state_reason
		)
		SELECT workspaces.workspace_id, ?, MAX(events.occurred_at), 90, ?
		FROM (
			SELECT workspace_id FROM analytics_outbox
			UNION
			SELECT workspace_id FROM analytics_events
		) AS workspaces
		LEFT JOIN analytics_events AS events ON events.workspace_id = workspaces.workspace_id
		GROUP BY workspaces.workspace_id
		ON DUPLICATE KEY UPDATE
			status = VALUES(status),
			data_through_at = VALUES(data_through_at),
			state_reason = VALUES(state_reason)`, status, reason)
	return err
}
