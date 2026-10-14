package links

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"testing"

	"github.com/Techshrr/GoJet/internal/analytics"
)

type observedPublisherProbe struct {
	got   analytics.Event
	calls int
	err   error
}

func (p *observedPublisherProbe) Publish(_ context.Context, event analytics.Event) (string, error) {
	p.got = event
	p.calls++
	return "original-stream-id", p.err
}

func TestObservedPublisherPreservesEventAndFailure(t *testing.T) {
	for _, failure := range []error{nil, errors.New("unavailable")} {
		var logs bytes.Buffer
		next := &observedPublisherProbe{err: failure}
		publisher := observedAnalyticsPublisher{next: next, logger: slog.New(slog.NewJSONHandler(&logs, nil))}
		event := analytics.Event{SchemaVersion: 1, EventType: "click", EventID: "event-test", WorkspaceID: "workspace-test", LinkID: 7, ClickSequence: 9}
		stream, err := publisher.Publish(context.Background(), event)
		if stream != "original-stream-id" || err != failure || next.calls != 1 || !reflect.DeepEqual(next.got, event) {
			t.Fatal("observability changed the underlying publisher result or invocation")
		}
		if !bytes.Contains(logs.Bytes(), []byte("redirect click committed")) {
			t.Fatal("durable click correlation record absent")
		}
	}
}
