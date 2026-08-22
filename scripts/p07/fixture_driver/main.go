package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/analytics"
	"github.com/Techshrr/GoJet/internal/links"
)

func main() {
	workspace := flag.String("workspace", "", "Workspace ID")
	linkID := flag.Uint64("link-id", 0, "Link ID")
	at := flag.String("at", "", "RFC3339 event time")
	country := flag.String("country", "", "country dimension")
	device := flag.String("device", "", "device dimension")
	language := flag.String("language", "", "language dimension")
	source := flag.String("source", "", "source hostname dimension")
	campaign := flag.String("campaign", "", "campaign measurement key")
	publish := flag.Bool("publish", true, "publish durable outbox event to Redis")
	flag.Parse()

	occurredAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*at))
	if err != nil || strings.TrimSpace(*workspace) == "" || *linkID == 0 {
		fatal(fmt.Errorf("invalid fixture input"))
	}
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	redisAddr := strings.TrimSpace(os.Getenv("GOJET_REDIS_ADDR"))
	if dsn == "" || redisAddr == "" {
		fatal(fmt.Errorf("required configuration missing"))
	}
	db, err := links.OpenMySQL(dsn)
	if err != nil {
		fatal(err)
	}
	defer db.Close()
	store := links.NewMySQLStore(db)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	link, err := store.GetByID(ctx, strings.TrimSpace(*workspace), *linkID)
	if err != nil {
		fatal(err)
	}
	claimed, state, event, err := store.ClaimRedirectAccessCurrentAuthorityWithAnalytics(
		ctx, link.ID, link.Version, link.RiskFingerprint, occurredAt.UTC(),
		analytics.Dimensions{
			CountryCode: *country,
			Device: *device,
			Language: *language,
			SourceHostname: *source,
			CampaignID: *campaign,
		},
	)
	if err != nil {
		fatal(err)
	}
	if state != links.AccessClaimAllowed || event == nil {
		fatal(fmt.Errorf("claim state %s did not create analytics event", state))
	}

	streamID := ""
	if *publish {
		redisDB := 0
		if raw := strings.TrimSpace(os.Getenv("GOJET_REDIS_DB")); raw != "" {
			parsed, parseErr := strconv.Atoi(raw)
			if parseErr != nil || parsed < 0 {
				fatal(fmt.Errorf("invalid GOJET_REDIS_DB"))
			}
			redisDB = parsed
		}
		redisClient := links.NewRedisClient(redisAddr, os.Getenv("GOJET_REDIS_PASSWORD"), redisDB)
		defer redisClient.Close()
		publisher := analytics.NewRedisStreamPublisher(redisClient)
		streamID, err = publisher.Publish(ctx, *event)
		if err != nil {
			_ = store.RecordAnalyticsOutboxPublishFailure(ctx, event.EventID, err)
			fatal(err)
		}
		if err := store.MarkAnalyticsOutboxPublished(ctx, event.EventID, streamID, time.Now().UTC()); err != nil {
			fatal(err)
		}
	}

	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"event": event,
		"link_id": claimed.ID,
		"click_count": claimed.ClickCount,
		"published": *publish,
		"stream_id": streamID,
	})
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
