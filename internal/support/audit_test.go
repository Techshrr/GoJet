package support

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
)

type auditSQLResult int64

func (r auditSQLResult) LastInsertId() (int64, error) { return int64(r), nil }
func (r auditSQLResult) RowsAffected() (int64, error) { return int64(r), nil }

type recordingAuditExecer struct {
	calls int
	args  []any
}

func (e *recordingAuditExecer) ExecContext(_ context.Context, _ string, args ...any) (sql.Result, error) {
	e.calls++
	e.args = append([]any(nil), args...)
	return auditSQLResult(1), nil
}

type recordingAuditWriter struct {
	inputs []SupportAuditInput
	err    error
}

func (w *recordingAuditWriter) RecordSupportAudit(_ context.Context, input SupportAuditInput) error {
	w.inputs = append(w.inputs, input)
	return w.err
}

func TestSupportAuditRejectsNonAllowlistedMetadataBeforeSQL(t *testing.T) {
	execer := &recordingAuditExecer{}
	err := recordSupportAuditExecer(context.Background(), execer, SupportAuditInput{
		WorkspaceID: "ws-1", ActorID: "user-1", Action: "ticket_created", ResourceType: "ticket", ResourceID: "tkt-1",
		CorrelationID: "corr-1", Result: AuditSuccess,
		Metadata: map[string]string{"recipient": "secret@example.test"},
	})
	if err == nil {
		t.Fatal("unsafe audit metadata was accepted")
	}
	if execer.calls != 0 {
		t.Fatalf("unsafe metadata reached SQL: %d", execer.calls)
	}
}

func TestSupportAuditAcceptsAllowlistedMetadataWithoutRawSecrets(t *testing.T) {
	execer := &recordingAuditExecer{}
	err := recordSupportAuditExecer(context.Background(), execer, SupportAuditInput{
		WorkspaceID: "ws-1", ActorID: "user-1", Action: "ticket_created", ResourceType: "ticket", ResourceID: "tkt-1",
		CorrelationID: "corr-1", Result: AuditSuccess,
		Metadata: map[string]string{"status": "open", "created": "true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if execer.calls != 1 {
		t.Fatalf("SQL calls=%d", execer.calls)
	}
}

func TestWithSupportCorrelationGeneratesOneStableRequestCorrelation(t *testing.T) {
	var headerValue, contextValue, firstRead, secondRead string
	handler := WithSupportCorrelation(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		headerValue = r.Header.Get("X-Request-ID")
		contextValue = SupportAuditCorrelation(r.Context())
		firstRead = supportRequestCorrelationID(r)
		secondRead = supportRequestCorrelationID(r)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/support/tickets", nil))
	if headerValue == "" || headerValue != contextValue || headerValue != firstRead || firstRead != secondRead {
		t.Fatalf("header=%q context=%q first=%q second=%q", headerValue, contextValue, firstRead, secondRead)
	}
}

func TestWithSupportCorrelationReplacesUnsafeClientCorrelation(t *testing.T) {
	var seen string
	handler := WithSupportCorrelation(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = r.Header.Get("X-Request-ID") }))
	req := httptest.NewRequest(http.MethodGet, "/api/support/tickets", nil)
	req.Header.Set("X-Request-ID", "unsafe correlation with spaces")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if seen == "" || seen == "unsafe correlation with spaces" || !validAuditASCII(seen, 128) {
		t.Fatalf("canonical correlation=%q", seen)
	}
}

type replayDomainProjector struct {
	resolved     domains.ResolvedEntitlement
	projectCalls int
}

func (p *replayDomainProjector) ResolveEntitlement(_ context.Context, _ string, _ time.Time) (domains.ResolvedEntitlement, error) {
	return p.resolved, nil
}
func (p *replayDomainProjector) ProjectAccessRequest(_ context.Context, input domains.AccessRequestInput) (domains.AccessRequest, error) {
	p.projectCalls++
	return domains.AccessRequest{WorkspaceID: input.WorkspaceID, SupportTicketID: input.SupportTicketID, SubmittedAt: input.SubmittedAt}, nil
}

func TestRequestedDomainReplayRepairsAuditWithoutSecondP06Write(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	ticket := Ticket{
		ID: "tkt-1", WorkspaceID: "ws-1", RequesterUserID: "user-1", Category: CustomDomainAccessCategory,
		Subject: "Domain access", Status: TicketOpen, CreatedAt: now, UpdatedAt: now, Version: 1, CorrelationID: "corr-1",
	}
	inner := &replayDomainProjector{resolved: domains.ResolvedEntitlement{
		Status: domains.EntitlementRequested, Source: domains.SourceNone, SupportTicketID: "tkt-1",
	}}
	audit := &recordingAuditWriter{}
	projector, err := NewAuditedDomainAccessProjector(inner, audit)
	if err != nil {
		t.Fatal(err)
	}
	request, err := ProjectTicketDomainAccess(context.Background(), projector, ticket)
	if err != nil {
		t.Fatal(err)
	}
	if request.SupportTicketID != ticket.ID || inner.projectCalls != 0 {
		t.Fatalf("request=%+v projectCalls=%d", request, inner.projectCalls)
	}
	if len(audit.inputs) != 1 || audit.inputs[0].Action != "domain_request_linked" || audit.inputs[0].CorrelationID != ticket.CorrelationID {
		t.Fatalf("audit=%+v", audit.inputs)
	}
	if audit.inputs[0].Metadata["grant_authority"] != "NONE" {
		t.Fatalf("audit metadata=%v", audit.inputs[0].Metadata)
	}
}
