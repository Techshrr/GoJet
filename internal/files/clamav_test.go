package files

import (
	"errors"
	"testing"
	"time"
)

func TestParseVersionReplyFreshAndStale(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	health, err := parseVersionReply("ClamAV 1.4.2/27888/Sat Aug 22 11:00:00 2026", now, 48*time.Hour)
	if err != nil || !health.Fresh || health.SignatureVersion != "27888" {
		t.Fatalf("unexpected fresh health: %#v err=%v", health, err)
	}
	health, err = parseVersionReply("ClamAV 1.4.2/27800/Wed Aug 19 11:00:00 2026", now, 48*time.Hour)
	if err != nil || health.Fresh {
		t.Fatalf("unexpected stale health: %#v err=%v", health, err)
	}
}

func TestParseScanReplyIsFailClosed(t *testing.T) {
	health := ClamAVHealth{EngineVersion: "1.4.2", SignatureVersion: "27888", SignatureDate: time.Now().UTC(), Fresh: true}
	clean, err := parseScanReply("stream: OK", health)
	if err != nil || clean.Verdict != VerdictClean {
		t.Fatalf("clean parse failed: %#v err=%v", clean, err)
	}
	infected, err := parseScanReply("stream: Win.Test.EICAR_HDB-1 FOUND", health)
	if err != nil || infected.Verdict != VerdictInfected || infected.VerdictCode == "" {
		t.Fatalf("infected parse failed: %#v err=%v", infected, err)
	}
	unknown, err := parseScanReply("stream: UNKNOWN MAYBE", health)
	if !errors.Is(err, ErrScanIndeterminate) || unknown.Verdict != VerdictError {
		t.Fatalf("indeterminate response must fail closed: %#v err=%v", unknown, err)
	}
}
