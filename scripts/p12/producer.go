package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/analytics"
	"github.com/Techshrr/GoJet/internal/workspace"
	_ "github.com/go-sql-driver/mysql"
)

func main() {
	var (
		action       = flag.String("action", "", "notification|notification-state|analytics-event|last-owner-race")
		workspaceID  = flag.String("workspace", "", "Workspace ID")
		recipient    = flag.String("recipient", "", "recipient user ID")
		category     = flag.String("category", "resources", "notification category")
		eventKey     = flag.String("event-key", "p12.test", "notification event key")
		dedupeKey    = flag.String("dedupe-key", "", "notification dedupe key")
		title        = flag.String("title", "P12 test", "notification title")
		summary      = flag.String("summary", "", "notification summary")
		deepLink     = flag.String("deep-link", "", "notification deep link")
		resourceType = flag.String("resource-type", "", "notification resource type")
		resourceID   = flag.String("resource-id", "", "notification resource ID")
		state        = flag.String("state", "complete", "notification/analytics state")
		reason       = flag.String("reason", "current", "state reason")
		campaignID   = flag.String("campaign", "", "analytics campaign ID")
		linkID       = flag.Uint64("link", 0, "analytics link ID")
		sequence     = flag.Uint64("sequence", 1, "analytics click sequence")
		memberA      = flag.Uint64("member-a", 0, "first owner membership ID")
		memberB      = flag.Uint64("member-b", 0, "second owner membership ID")
	)
	flag.Parse()

	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	if dsn == "" {
		fatal(fmt.Errorf("GOJET_MYSQL_DSN is required"))
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		fatal(err)
	}

	switch *action {
	case "notification":
		store := workspace.NewStore(db)
		item, inserted, err := store.ProduceNotification(ctx, workspace.NotificationInput{
			WorkspaceID:     strings.TrimSpace(*workspaceID),
			RecipientUserID: strings.TrimSpace(*recipient),
			Category:        strings.TrimSpace(*category),
			EventKey:        strings.TrimSpace(*eventKey),
			DedupeKey:       strings.TrimSpace(*dedupeKey),
			Title:           *title,
			Summary:         *summary,
			DeepLink:        *deepLink,
			ResourceType:    strings.TrimSpace(*resourceType),
			ResourceID:      strings.TrimSpace(*resourceID),
		})
		if err != nil {
			fatal(err)
		}
		emit(map[string]any{"inserted": inserted, "notification": item})
	case "notification-state":
		store := workspace.NewStore(db)
		now := time.Now().UTC()
		var through *time.Time
		if *state != "stale" {
			through = &now
		}
		if err := store.SetNotificationState(ctx, strings.TrimSpace(*workspaceID), strings.TrimSpace(*state), strings.TrimSpace(*reason), through); err != nil {
			fatal(err)
		}
		item, err := store.GetNotificationState(ctx, strings.TrimSpace(*workspaceID))
		if err != nil {
			fatal(err)
		}
		emit(item)
	case "last-owner-race":
		if *memberA == 0 || *memberB == 0 || *memberA == *memberB {
			fatal(fmt.Errorf("last-owner-race requires distinct --member-a and --member-b"))
		}
		store := workspace.NewStore(db)
		type outcome struct {
			MemberID uint64 `json:"member_id"`
			Error    string `json:"error,omitempty"`
		}
		start := make(chan struct{})
		results := make(chan outcome, 2)
		run := func(memberID uint64) {
			<-start
			_, err := store.UpdateMemberRole(context.Background(), strings.TrimSpace(*workspaceID), memberID, workspace.RoleOwner, workspace.RoleViewer)
			result := outcome{MemberID: memberID}
			if err != nil {
				result.Error = err.Error()
			}
			results <- result
		}
		go run(*memberA)
		go run(*memberB)
		close(start)
		first, second := <-results, <-results
		emit(map[string]any{"outcomes": []outcome{first, second}})
	case "analytics-event":
		if *linkID == 0 || strings.TrimSpace(*campaignID) == "" {
			fatal(fmt.Errorf("analytics-event requires --link and --campaign"))
		}
		now := time.Now().UTC().Add(-2 * time.Second)
		event, err := analytics.NewClickEvent(strings.TrimSpace(*workspaceID), *linkID, *sequence, now, analytics.Dimensions{
			CountryCode:    "sg",
			Device:         "desktop",
			Language:       "en",
			SourceHostname: "p12.integration.test",
			CampaignID:     strings.TrimSpace(*campaignID),
		})
		if err != nil {
			fatal(err)
		}
		store := analytics.NewStore(db)
		inserted, err := store.PersistConsumedEvent(ctx, event, "p12-"+event.EventID[:16], time.Now().UTC())
		if err != nil {
			fatal(err)
		}
		dataThrough := time.Now().UTC()
		status := analytics.DatasetComplete
		switch *state {
		case "partial":
			status = analytics.DatasetPartial
		case "stale":
			status = analytics.DatasetStale
		case "complete":
		default:
			fatal(fmt.Errorf("invalid analytics state %q", *state))
		}
		if err := store.UpsertWorkspaceState(ctx, analytics.WorkspaceState{
			WorkspaceID:   strings.TrimSpace(*workspaceID),
			Status:        status,
			DataThroughAt: &dataThrough,
			RetentionDays: 90,
			StateReason:   strings.TrimSpace(*reason),
		}); err != nil {
			fatal(err)
		}
		emit(map[string]any{"inserted": inserted, "event": event})
	default:
		fatal(fmt.Errorf("unsupported --action %q", *action))
	}
}

func emit(value any) {
	if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"error": err.Error()})
	os.Exit(1)
}
