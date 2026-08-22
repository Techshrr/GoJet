package analytics

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func InsertOutboxTx(ctx context.Context, tx *sql.Tx, event Event) error {
	if err := ValidateEvent(event); err != nil {
		return err
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO analytics_outbox (
			event_id, workspace_id, link_id, click_sequence, occurred_at,
			country_code, device, language, source_hostname, campaign_id, payload_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.EventID, event.WorkspaceID, event.LinkID, event.ClickSequence, event.OccurredAt.UTC(),
		event.Dimensions.CountryCode, event.Dimensions.Device, event.Dimensions.Language,
		event.Dimensions.SourceHostname, event.Dimensions.CampaignID, string(raw),
	)
	return err
}

func (s *Store) MarkOutboxPublished(ctx context.Context, eventID, streamID string, at time.Time) error {
	if !validEventID(eventID) || strings.TrimSpace(streamID) == "" || len(streamID) > 64 || at.IsZero() {
		return ErrInvalidEvent
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE analytics_outbox
		SET published_at = COALESCE(published_at, ?),
		    published_stream_id = COALESCE(published_stream_id, ?),
		    publish_attempts = publish_attempts + 1,
		    last_publish_error = NULL
		WHERE event_id = ?`, at.UTC(), strings.TrimSpace(streamID), eventID)
	return err
}

func (s *Store) RecordOutboxPublishFailure(ctx context.Context, eventID string, publishErr error) error {
	if !validEventID(eventID) || publishErr == nil {
		return ErrInvalidEvent
	}
	message := publishErr.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE analytics_outbox
		SET publish_attempts = publish_attempts + 1, last_publish_error = ?
		WHERE event_id = ?`, message, eventID)
	return err
}

func (s *Store) PersistConsumedEvent(ctx context.Context, event Event, streamID string, consumedAt time.Time) (bool, error) {
	if err := ValidateEvent(event); err != nil {
		return false, err
	}
	streamID = strings.TrimSpace(streamID)
	if streamID == "" || len(streamID) > 64 || consumedAt.IsZero() {
		return false, ErrInvalidEvent
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT IGNORE INTO analytics_events (
			event_id, workspace_id, link_id, click_sequence, occurred_at,
			country_code, device, language, source_hostname, campaign_id,
			stream_id, consumed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.EventID, event.WorkspaceID, event.LinkID, event.ClickSequence, event.OccurredAt.UTC(),
		event.Dimensions.CountryCode, event.Dimensions.Device, event.Dimensions.Language,
		event.Dimensions.SourceHostname, event.Dimensions.CampaignID, streamID, consumedAt.UTC(),
	)
	if err != nil {
		return false, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if inserted == 0 {
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}

	bucket := event.OccurredAt.UTC().Truncate(time.Hour)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO analytics_hourly_aggregates (
			workspace_id, link_id, bucket_start, country_code, device,
			language, source_hostname, campaign_id, clicks
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)
		ON DUPLICATE KEY UPDATE clicks = clicks + 1`,
		event.WorkspaceID, event.LinkID, bucket, event.Dimensions.CountryCode, event.Dimensions.Device,
		event.Dimensions.Language, event.Dimensions.SourceHostname, event.Dimensions.CampaignID,
	)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) PendingOutbox(ctx context.Context, limit int) ([]Event, error) {
	if limit <= 0 || limit > 1000 {
		return nil, ErrInvalidEvent
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT payload_json FROM analytics_outbox
		WHERE published_at IS NULL
		ORDER BY created_at, event_id
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Event, 0, limit)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var event Event
		if err := json.Unmarshal(raw, &event); err != nil {
			return nil, fmt.Errorf("decode analytics outbox payload: %w", err)
		}
		if err := ValidateEvent(event); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func validEventID(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return true
}
