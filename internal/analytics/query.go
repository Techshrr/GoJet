package analytics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidQuery        = errors.New("invalid analytics query")
	ErrAnalyticsNotFound   = errors.New("analytics resource not found")
	ErrCampaignAssociation = errors.New("invalid campaign analytics association")
)

type DatasetStatus string

const (
	DatasetComplete DatasetStatus = "complete"
	DatasetPartial  DatasetStatus = "partial"
	DatasetStale    DatasetStatus = "stale"
)

type WorkspaceState struct {
	WorkspaceID    string        `json:"workspace_id"`
	Status         DatasetStatus `json:"status"`
	DataThroughAt  *time.Time    `json:"data_through_at,omitempty"`
	RetentionDays  int           `json:"retention_days"`
	StateReason    string        `json:"state_reason"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

type Query struct {
	WorkspaceID    string
	LinkID         *uint64
	From           time.Time
	To             time.Time
	Timezone       string
	Granularity    string
	CountryCode    string
	Device         string
	Language       string
	SourceHostname string
	CampaignID     string
	Now            time.Time
}

type Bucket struct {
	Key    string `json:"key"`
	Clicks uint64 `json:"clicks"`
}

type DimensionCount struct {
	Value  string `json:"value"`
	Clicks uint64 `json:"clicks"`
}

type Report struct {
	State             string                      `json:"state"`
	StateReason       string                      `json:"state_reason"`
	RequestedFrom     time.Time                   `json:"requested_from"`
	EffectiveFrom     time.Time                   `json:"effective_from"`
	To                time.Time                   `json:"to"`
	Timezone          string                      `json:"timezone"`
	Granularity       string                      `json:"granularity"`
	RetentionLimited  bool                        `json:"retention_limited"`
	RetentionCutoff   time.Time                   `json:"retention_cutoff"`
	DataThroughAt     *time.Time                  `json:"data_through_at,omitempty"`
	TotalClicks       uint64                      `json:"total_clicks"`
	TotalConversions  uint64                      `json:"total_conversions"`
	Buckets           []Bucket                    `json:"buckets"`
	Dimensions        map[string][]DimensionCount `json:"dimensions"`
	GeneratedAt       time.Time                   `json:"generated_at"`
}

type Conversion struct {
	WorkspaceID  string    `json:"workspace_id"`
	ConversionID string    `json:"conversion_id"`
	CampaignID   string    `json:"campaign_id"`
	LinkID       uint64    `json:"link_id"`
	OccurredAt   time.Time `json:"occurred_at"`
}

type eventProjection struct {
	OccurredAt     time.Time
	CountryCode    string
	Device         string
	Language       string
	SourceHostname string
	CampaignID     string
}

func (s *Store) LinkBelongsToWorkspace(ctx context.Context, workspaceID string, linkID uint64) (bool, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" || linkID == 0 {
		return false, ErrInvalidQuery
	}
	var marker uint64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM links WHERE id = ? AND workspace_id = ? AND deleted_at IS NULL`, linkID, workspaceID).Scan(&marker)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return marker == linkID, nil
}

func (s *Store) GetWorkspaceState(ctx context.Context, workspaceID string) (WorkspaceState, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return WorkspaceState{}, ErrInvalidQuery
	}
	var state WorkspaceState
	var dataThrough sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT workspace_id, status, data_through_at, retention_days, state_reason, updated_at
		FROM analytics_workspace_state WHERE workspace_id = ?`, workspaceID,
	).Scan(&state.WorkspaceID, &state.Status, &dataThrough, &state.RetentionDays, &state.StateReason, &state.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkspaceState{
			WorkspaceID: workspaceID,
			Status: DatasetPartial,
			RetentionDays: 90,
			StateReason: "state_unavailable",
			UpdatedAt: time.Time{},
		}, nil
	}
	if err != nil {
		return WorkspaceState{}, err
	}
	if dataThrough.Valid {
		value := dataThrough.Time.UTC()
		state.DataThroughAt = &value
	}
	state.UpdatedAt = state.UpdatedAt.UTC()
	return state, nil
}

func (s *Store) UpsertWorkspaceState(ctx context.Context, state WorkspaceState) error {
	state.WorkspaceID = strings.TrimSpace(state.WorkspaceID)
	state.StateReason = strings.TrimSpace(state.StateReason)
	if state.WorkspaceID == "" || state.RetentionDays < 1 || state.RetentionDays > 3660 || state.StateReason == "" || len(state.StateReason) > 160 {
		return ErrInvalidQuery
	}
	if state.Status != DatasetComplete && state.Status != DatasetPartial && state.Status != DatasetStale {
		return ErrInvalidQuery
	}
	var dataThrough any
	if state.DataThroughAt != nil {
		dataThrough = state.DataThroughAt.UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO analytics_workspace_state (workspace_id, status, data_through_at, retention_days, state_reason)
		VALUES (?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE status = VALUES(status), data_through_at = VALUES(data_through_at),
		retention_days = VALUES(retention_days), state_reason = VALUES(state_reason)`,
		state.WorkspaceID, state.Status, dataThrough, state.RetentionDays, state.StateReason,
	)
	return err
}

func (s *Store) QueryReport(ctx context.Context, query Query) (Report, error) {
	normalized, location, err := normalizeQuery(query)
	if err != nil {
		return Report{}, err
	}
	if normalized.LinkID != nil {
		allowed, err := s.LinkBelongsToWorkspace(ctx, normalized.WorkspaceID, *normalized.LinkID)
		if err != nil {
			return Report{}, err
		}
		if !allowed {
			return Report{}, ErrAnalyticsNotFound
		}
	}
	state, err := s.GetWorkspaceState(ctx, normalized.WorkspaceID)
	if err != nil {
		return Report{}, err
	}
	cutoff := normalized.Now.AddDate(0, 0, -state.RetentionDays).UTC()
	effectiveFrom := normalized.From
	retentionLimited := normalized.From.Before(cutoff)
	if retentionLimited {
		effectiveFrom = cutoff
	}
	if !effectiveFrom.Before(normalized.To) {
		effectiveFrom = normalized.To
	}

	events, err := s.queryEvents(ctx, normalized, effectiveFrom)
	if err != nil {
		return Report{}, err
	}
	buckets := map[string]uint64{}
	dimensions := map[string]map[string]uint64{
		"country": {}, "device": {}, "language": {}, "source": {}, "campaign": {},
	}
	for _, event := range events {
		buckets[bucketKey(event.OccurredAt, location, normalized.Granularity)]++
		dimensions["country"][event.CountryCode]++
		dimensions["device"][event.Device]++
		dimensions["language"][event.Language]++
		dimensions["source"][event.SourceHostname]++
		dimensions["campaign"][event.CampaignID]++
	}
	bucketList := make([]Bucket, 0, len(buckets))
	for key, clicks := range buckets {
		bucketList = append(bucketList, Bucket{Key: key, Clicks: clicks})
	}
	sort.Slice(bucketList, func(i, j int) bool { return bucketList[i].Key < bucketList[j].Key })

	conversionTotal, err := s.queryConversionTotal(ctx, normalized, effectiveFrom)
	if err != nil {
		return Report{}, err
	}

	resultState := "success"
	reason := state.StateReason
	switch state.Status {
	case DatasetStale:
		resultState = "stale"
	case DatasetPartial:
		resultState = "partial"
	default:
		if retentionLimited {
			resultState = "retention-limited"
			reason = "requested_range_precedes_retention"
		} else if len(events) == 0 {
			resultState = "empty"
			reason = "complete_zero"
		}
	}

	return Report{
		State:            resultState,
		StateReason:      reason,
		RequestedFrom:    normalized.From,
		EffectiveFrom:    effectiveFrom,
		To:               normalized.To,
		Timezone:         normalized.Timezone,
		Granularity:      normalized.Granularity,
		RetentionLimited: retentionLimited,
		RetentionCutoff:  cutoff,
		DataThroughAt:    state.DataThroughAt,
		TotalClicks:      uint64(len(events)),
		TotalConversions: conversionTotal,
		Buckets:          bucketList,
		Dimensions: map[string][]DimensionCount{
			"country": dimensionList(dimensions["country"]),
			"device": dimensionList(dimensions["device"]),
			"language": dimensionList(dimensions["language"]),
			"source": dimensionList(dimensions["source"]),
			"campaign": dimensionList(dimensions["campaign"]),
		},
		GeneratedAt: normalized.Now,
	}, nil
}

func (s *Store) RecordConversion(ctx context.Context, conversion Conversion) (bool, error) {
	conversion.WorkspaceID = strings.TrimSpace(conversion.WorkspaceID)
	conversion.ConversionID = strings.TrimSpace(conversion.ConversionID)
	conversion.CampaignID = strings.TrimSpace(conversion.CampaignID)
	if conversion.WorkspaceID == "" || conversion.ConversionID == "" || len(conversion.ConversionID) > 128 || conversion.CampaignID == "" || len(conversion.CampaignID) > 64 || conversion.LinkID == 0 || conversion.OccurredAt.IsZero() {
		return false, ErrInvalidQuery
	}
	allowed, err := s.LinkBelongsToWorkspace(ctx, conversion.WorkspaceID, conversion.LinkID)
	if err != nil {
		return false, err
	}
	if !allowed {
		return false, ErrAnalyticsNotFound
	}
	var matched int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM analytics_events
		WHERE workspace_id = ? AND link_id = ? AND campaign_id = ?`,
		conversion.WorkspaceID, conversion.LinkID, conversion.CampaignID,
	).Scan(&matched); err != nil {
		return false, err
	}
	if matched == 0 {
		return false, ErrCampaignAssociation
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT IGNORE INTO analytics_conversions (workspace_id, conversion_id, campaign_id, link_id, occurred_at)
		VALUES (?, ?, ?, ?, ?)`,
		conversion.WorkspaceID, conversion.ConversionID, conversion.CampaignID, conversion.LinkID, conversion.OccurredAt.UTC(),
	)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

func (s *Store) queryEvents(ctx context.Context, query Query, effectiveFrom time.Time) ([]eventProjection, error) {
	args := []any{query.WorkspaceID, effectiveFrom.UTC(), query.To.UTC()}
	where := `workspace_id = ? AND occurred_at >= ? AND occurred_at < ?`
	if query.LinkID != nil {
		where += ` AND link_id = ?`
		args = append(args, *query.LinkID)
	}
	filters := []struct {
		column string
		value  string
	}{
		{"country_code", query.CountryCode},
		{"device", query.Device},
		{"language", query.Language},
		{"source_hostname", query.SourceHostname},
		{"campaign_id", query.CampaignID},
	}
	for _, filter := range filters {
		if filter.value != "" {
			where += ` AND ` + filter.column + ` = ?`
			args = append(args, filter.value)
		}
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT occurred_at, country_code, device, language, source_hostname, campaign_id
		FROM analytics_events WHERE `+where+` ORDER BY occurred_at, event_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []eventProjection{}
	for rows.Next() {
		var event eventProjection
		if err := rows.Scan(&event.OccurredAt, &event.CountryCode, &event.Device, &event.Language, &event.SourceHostname, &event.CampaignID); err != nil {
			return nil, err
		}
		event.OccurredAt = event.OccurredAt.UTC()
		result = append(result, event)
	}
	return result, rows.Err()
}

func (s *Store) queryConversionTotal(ctx context.Context, query Query, effectiveFrom time.Time) (uint64, error) {
	args := []any{query.WorkspaceID, effectiveFrom.UTC(), query.To.UTC()}
	where := `workspace_id = ? AND occurred_at >= ? AND occurred_at < ?`
	if query.LinkID != nil {
		where += ` AND link_id = ?`
		args = append(args, *query.LinkID)
	}
	if query.CampaignID != "" {
		where += ` AND campaign_id = ?`
		args = append(args, query.CampaignID)
	}
	var total uint64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM analytics_conversions WHERE `+where, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func normalizeQuery(query Query) (Query, *time.Location, error) {
	query.WorkspaceID = strings.TrimSpace(query.WorkspaceID)
	query.Timezone = strings.TrimSpace(query.Timezone)
	query.Granularity = strings.ToLower(strings.TrimSpace(query.Granularity))
	query.CountryCode = strings.ToLower(strings.TrimSpace(query.CountryCode))
	query.Device = strings.ToLower(strings.TrimSpace(query.Device))
	query.Language = strings.ToLower(strings.TrimSpace(query.Language))
	query.SourceHostname = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(query.SourceHostname), "."))
	query.CampaignID = strings.TrimSpace(query.CampaignID)
	if query.Timezone == "" {
		query.Timezone = "UTC"
	}
	if query.Granularity == "" {
		query.Granularity = "day"
	}
	if query.Now.IsZero() {
		query.Now = time.Now().UTC()
	} else {
		query.Now = query.Now.UTC()
	}
	if query.WorkspaceID == "" || query.From.IsZero() || query.To.IsZero() || !query.From.Before(query.To) || query.To.Sub(query.From) > 3660*24*time.Hour {
		return Query{}, nil, ErrInvalidQuery
	}
	if query.Granularity != "hour" && query.Granularity != "day" {
		return Query{}, nil, ErrInvalidQuery
	}
	if len(query.CountryCode) > 8 || len(query.Device) > 16 || len(query.Language) > 32 || len(query.SourceHostname) > 253 || len(query.CampaignID) > 64 {
		return Query{}, nil, ErrInvalidQuery
	}
	location, err := time.LoadLocation(query.Timezone)
	if err != nil {
		return Query{}, nil, fmt.Errorf("%w: timezone", ErrInvalidQuery)
	}
	query.From = query.From.UTC()
	query.To = query.To.UTC()
	return query, location, nil
}

func bucketKey(value time.Time, location *time.Location, granularity string) string {
	local := value.In(location)
	if granularity == "hour" {
		_, offset := local.Zone()
		return local.Format("2006-01-02T15:00") + formatOffset(offset)
	}
	return local.Format("2006-01-02")
}

func formatOffset(offsetSeconds int) string {
	sign := "+"
	if offsetSeconds < 0 {
		sign = "-"
		offsetSeconds = -offsetSeconds
	}
	hours := offsetSeconds / 3600
	minutes := (offsetSeconds % 3600) / 60
	return sign + twoDigits(hours) + ":" + twoDigits(minutes)
}

func twoDigits(value int) string {
	if value < 10 {
		return "0" + strconv.Itoa(value)
	}
	return strconv.Itoa(value)
}

func dimensionList(values map[string]uint64) []DimensionCount {
	result := make([]DimensionCount, 0, len(values))
	for value, clicks := range values {
		result = append(result, DimensionCount{Value: value, Clicks: clicks})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Clicks == result[j].Clicks {
			return result[i].Value < result[j].Value
		}
		return result[i].Clicks > result[j].Clicks
	})
	return result
}
