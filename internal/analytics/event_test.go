package analytics

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestClickEventIdentityAndNormalization(t *testing.T) {
	occurred := time.Date(2026, 8, 22, 9, 30, 0, 123000000, time.UTC)
	dimensions := Dimensions{
		CountryCode:    " SG ",
		Device:         "Mobile",
		Language:       "EN-SG",
		SourceHostname: "SOURCE.P07.TEST.",
	}
	first, err := NewClickEvent("ws-p07", 42, 7, occurred, dimensions)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewClickEvent("ws-p07", 42, 7, occurred.Add(time.Hour), dimensions)
	if err != nil {
		t.Fatal(err)
	}
	if first.EventID != second.EventID {
		t.Fatalf("event identity must depend on workspace/link/click sequence, got %q != %q", first.EventID, second.EventID)
	}
	if first.Dimensions.CountryCode != "sg" || first.Dimensions.Device != "mobile" || first.Dimensions.Language != "en-sg" || first.Dimensions.SourceHostname != "source.p07.test" {
		t.Fatalf("dimensions not normalized: %#v", first.Dimensions)
	}
	if err := ValidateEvent(first); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}
	raw, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "destination") {
		t.Fatalf("click event must not contain destination data: %s", raw)
	}
}

func TestSanitizeDimensionsPreservesMeasuredValuesAndDropsUnsafeValues(t *testing.T) {
	got := SanitizeDimensions(Dimensions{
		CountryCode:    " SG ",
		Device:         "móbile",
		Language:       strings.Repeat("x", 33),
		SourceHostname: "例子.test.",
		CampaignID:     "bad\nvalue",
	})
	if got.CountryCode != "sg" {
		t.Fatalf("valid measured country must be preserved, got %q", got.CountryCode)
	}
	if got.Device != "" || got.Language != "" || got.SourceHostname != "" || got.CampaignID != "" {
		t.Fatalf("unsafe measured dimensions must degrade to unknown/empty: %#v", got)
	}
}

func TestNewClickEventRemainsStrictForUnsanitizedPayloads(t *testing.T) {
	_, err := NewClickEvent("ws-p07", 1, 1, time.Now().UTC(), Dimensions{SourceHostname: "例子.test"})
	if err == nil {
		t.Fatal("strict event constructor must reject an unsanitized non-ASCII dimension")
	}
}

func TestValidateEventRejectsIdentityMutation(t *testing.T) {
	event, err := NewClickEvent("ws-p07", 1, 1, time.Now().UTC(), Dimensions{Device: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	event.ClickSequence = 2
	if err := ValidateEvent(event); err == nil {
		t.Fatal("mutated click sequence must invalidate deterministic event identity")
	}
}
