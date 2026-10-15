package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Techshrr/GoJet/internal/support"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func TestP20MailTemplateVersionAndAudit(t *testing.T) {
	if os.Getenv("GOJET_P20_RBAC_PROBE") != "1" {
		t.Skip("requires migrated MySQL")
	}
	runtime, err := adminfixture.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	ctx := context.Background()
	_, err = runtime.DB.ExecContext(ctx, `INSERT INTO mail_templates(template_key,locale,version,subject_template,text_template,html_template,variable_allowlist_json) VALUES ('p20-template-probe','en',1,'Original','Hello {{display_name}}','<p>{{display_name}}</p>',JSON_ARRAY('display_name'))`)
	if err != nil {
		t.Fatal(err)
	}
	store, err := support.NewStore(runtime.DB)
	if err != nil {
		t.Fatal(err)
	}
	input := support.MailEnqueueInput{TemplateKey: "p20-template-probe", Locale: "en", RecipientKind: "requester", RecipientValue: "template@example.test", ResourceType: "template_probe", ResourceID: "old"}
	enqueue := func() {
		t.Helper()
		tx, e := runtime.DB.BeginTx(ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback()
		if e = support.EnqueueMailTx(ctx, tx, input, time.Now()); e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(); e != nil {
			t.Fatal(e)
		}
	}
	enqueue()
	patch := support.AdminMailTemplatePatch{Locale: "en", ExpectedVersion: 1, Subject: "Updated", Text: "Hi {{display_name}}", HTML: "<strong>{{display_name}}</strong>"}
	var updated support.AdminMailTemplateView
	// The same correlation middleware used by the native HTTP route supplies audit context.
	handler := support.WithSupportCorrelation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		updated, err = store.UpdateAdminMailTemplate(support.WithSupportAuditActor(r.Context(), "p20-template-admin"), "p20-template-probe", patch)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("PATCH", "/", nil))
	if err != nil || updated.Version != 2 {
		t.Fatalf("save: %+v %v", updated, err)
	}
	// Exercise the production list query and preserve both immutable revisions.
	listed, listErr := store.ListAdminMailTemplates(ctx)
	if listErr != nil {
		t.Fatalf("list templates: %v", listErr)
	}
	var revisions []support.AdminMailTemplateView
	for _, item := range listed {
		if item.Key == "p20-template-probe" && item.Locale == "en" {
			revisions = append(revisions, item)
		}
	}
	if len(revisions) != 2 || revisions[0].Version != 2 || revisions[0].SubjectTemplate != "Updated" ||
		revisions[1].Version != 1 || revisions[1].SubjectTemplate != "Original" {
		t.Fatalf("listed revisions: %+v", revisions)
	}
	if len(revisions[0].VariableAllowlist) != 1 || revisions[0].VariableAllowlist[0] != "display_name" {
		t.Fatalf("list lost variable allowlist: %+v", revisions[0])
	}
	var version int
	if err = runtime.DB.QueryRow(`SELECT template_version FROM mail_jobs WHERE template_key='p20-template-probe' AND resource_id='old'`).Scan(&version); err != nil || version != 1 {
		t.Fatalf("queued version changed: %d %v", version, err)
	}
	// A logical retry after editing must not create a second notification mail.
	enqueue()
	input.ResourceID = "new"
	enqueue()
	if err = runtime.DB.QueryRow(`SELECT template_version FROM mail_jobs WHERE template_key='p20-template-probe' AND resource_id='new'`).Scan(&version); err != nil || version != 2 {
		t.Fatalf("new mail version: %d %v", version, err)
	}
	var count int
	if err = runtime.DB.QueryRow(`SELECT COUNT(*) FROM mail_jobs WHERE template_key='p20-template-probe'`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("dedupe: %d %v", count, err)
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("PATCH", "/", nil))
	if !errors.Is(err, support.ErrSupportConflict) {
		t.Fatalf("stale edit accepted: %v", err)
	}
	patch.ExpectedVersion = 2
	// Missing correlation makes the audit insert fail; the template insert must roll back.
	if _, err = store.UpdateAdminMailTemplate(support.WithSupportAuditActor(ctx, "p20-template-admin"), "p20-template-probe", patch); err == nil {
		t.Fatal("unaudited save accepted")
	}
	if err = runtime.DB.QueryRow(`SELECT COUNT(*) FROM mail_templates WHERE template_key='p20-template-probe'`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("revision count: %d %v", count, err)
	}
	if err = runtime.DB.QueryRow(`SELECT COUNT(*) FROM support_audit_events WHERE action='admin_mail_template_updated' AND resource_id='p20-template-probe:en'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("audit: %d %v", count, err)
	}
}
