package analytics

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	EventSchemaVersion = 1
	ClickEventType      = "link.click"
	ClickStreamKey      = "gojet:analytics:clicks:v1"
	WorkerGroup         = "gojet-analytics-workers-v1"
)

var ErrInvalidEvent = errors.New("invalid analytics event")

type Dimensions struct {
	CountryCode    string `json:"country_code"`
	Device         string `json:"device"`
	Language       string `json:"language"`
	SourceHostname string `json:"source_hostname"`
	CampaignID     string `json:"campaign_id"`
}

type Event struct {
	SchemaVersion int        `json:"schema_version"`
	EventType     string     `json:"event_type"`
	EventID       string     `json:"event_id"`
	WorkspaceID   string     `json:"workspace_id"`
	LinkID        uint64     `json:"link_id"`
	ClickSequence uint64     `json:"click_sequence"`
	OccurredAt    time.Time  `json:"occurred_at"`
	Dimensions    Dimensions `json:"dimensions"`
}

func NewClickEvent(workspaceID string, linkID, clickSequence uint64, occurredAt time.Time, dimensions Dimensions) (Event, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" || len(workspaceID) > 64 || linkID == 0 || clickSequence == 0 || occurredAt.IsZero() {
		return Event{}, ErrInvalidEvent
	}
	dimensions = normalizeDimensions(dimensions)
	if !validDimensions(dimensions) {
		return Event{}, ErrInvalidEvent
	}
	identity := fmt.Sprintf("gojet.analytics.click.v1\n%s\n%d\n%d", workspaceID, linkID, clickSequence)
	digest := sha256.Sum256([]byte(identity))
	return Event{
		SchemaVersion: EventSchemaVersion,
		EventType:     ClickEventType,
		EventID:       hex.EncodeToString(digest[:]),
		WorkspaceID:   workspaceID,
		LinkID:        linkID,
		ClickSequence: clickSequence,
		OccurredAt:    occurredAt.UTC(),
		Dimensions:    dimensions,
	}, nil
}

func ValidateEvent(event Event) error {
	if event.SchemaVersion != EventSchemaVersion || event.EventType != ClickEventType || len(event.EventID) != 64 {
		return ErrInvalidEvent
	}
	if _, err := hex.DecodeString(event.EventID); err != nil {
		return ErrInvalidEvent
	}
	rebuilt, err := NewClickEvent(event.WorkspaceID, event.LinkID, event.ClickSequence, event.OccurredAt, event.Dimensions)
	if err != nil || rebuilt.EventID != event.EventID || rebuilt.Dimensions != event.Dimensions {
		return ErrInvalidEvent
	}
	return nil
}

// SanitizeDimensions converts externally measured request metadata into the
// strict analytics storage contract without inventing values. Valid values are
// normalized and preserved. A value that is oversized, non-ASCII, or contains
// a control character becomes the empty/unknown dimension instead of making an
// otherwise-authorized redirect fail its analytics outbox transaction.
func SanitizeDimensions(in Dimensions) Dimensions {
	out := normalizeDimensions(in)
	if !validDimensionValue(out.CountryCode, 8) {
		out.CountryCode = ""
	}
	if !validDimensionValue(out.Device, 16) {
		out.Device = ""
	}
	if !validDimensionValue(out.Language, 32) {
		out.Language = ""
	}
	if !validDimensionValue(out.SourceHostname, 253) {
		out.SourceHostname = ""
	}
	if !validDimensionValue(out.CampaignID, 64) {
		out.CampaignID = ""
	}
	return out
}

func normalizeDimensions(in Dimensions) Dimensions {
	return Dimensions{
		CountryCode:    strings.ToLower(strings.TrimSpace(in.CountryCode)),
		Device:         strings.ToLower(strings.TrimSpace(in.Device)),
		Language:       strings.ToLower(strings.TrimSpace(in.Language)),
		SourceHostname: strings.ToLower(strings.TrimSuffix(strings.TrimSpace(in.SourceHostname), ".")),
		CampaignID:     strings.TrimSpace(in.CampaignID),
	}
}

func validDimensions(in Dimensions) bool {
	return validDimensionValue(in.CountryCode, 8) &&
		validDimensionValue(in.Device, 16) &&
		validDimensionValue(in.Language, 32) &&
		validDimensionValue(in.SourceHostname, 253) &&
		validDimensionValue(in.CampaignID, 64)
}

func validDimensionValue(value string, maxBytes int) bool {
	if len(value) > maxBytes {
		return false
	}
	for _, r := range value {
		if r > 127 || r < 32 {
			return false
		}
	}
	return true
}
