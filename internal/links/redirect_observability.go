package links

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/Techshrr/GoJet/internal/analytics"
)

// NewObservedRedirectHandler preserves the frozen redirect implementation.
// Its publisher is called only after the durable click/outbox transaction.
func NewObservedRedirectHandler(store *MySQLStore, risk *RedisRiskStore, publisher AnalyticsEventPublisher, trustTestRoutingHeaders bool, logger *slog.Logger) http.Handler {
	if publisher != nil && logger != nil {
		publisher = observedAnalyticsPublisher{next: publisher, logger: logger}
	}
	return NewRedirectHandlerWithAnalytics(store, risk, publisher, trustTestRoutingHeaders)
}

type observedAnalyticsPublisher struct {
	next   AnalyticsEventPublisher
	logger *slog.Logger
}

func (p observedAnalyticsPublisher) Publish(ctx context.Context, event analytics.Event) (string, error) {
	p.logger.InfoContext(ctx, "redirect click committed", "event_id", event.EventID,
		"workspace_id", event.WorkspaceID, "link_id", event.LinkID, "click_sequence", event.ClickSequence)
	return p.next.Publish(ctx, event)
}
