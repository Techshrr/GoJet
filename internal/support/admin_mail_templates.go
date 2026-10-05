package support

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

type AdminMailTemplatePatch struct {
	Locale          string `json:"locale"`
	ExpectedVersion uint64 `json:"expected_version"`
	Subject         string `json:"subject_template"`
	Text            string `json:"text_template"`
	HTML            string `json:"html_template"`
}

type adminMailTemplateEditor interface {
	UpdateAdminMailTemplate(context.Context, string, AdminMailTemplatePatch) (AdminMailTemplateView, error)
}

func renderAdminTemplate(base AdminMailTemplateView, patch AdminMailTemplatePatch, values map[string]string) (RenderedMail, error) {
	if patch.Locale != base.Locale || patch.ExpectedVersion != base.Version {
		return RenderedMail{}, ErrSupportConflict
	}
	if strings.TrimSpace(patch.Subject) == "" || len(patch.Subject) > 500 || strings.ContainsAny(patch.Subject, "\r\n") || len(patch.Text) > 24000 || len(patch.HTML) > 24000 || (strings.TrimSpace(patch.Text) == "" && strings.TrimSpace(patch.HTML) == "") {
		return RenderedMail{}, ErrInvalidInput
	}
	rendered, err := RenderMailTemplate(MailTemplate{Key: base.Key, Version: base.Version, SubjectTemplate: patch.Subject, TextTemplate: patch.Text, HTMLTemplate: patch.HTML, VariableAllowlist: base.VariableAllowlist, InternalOnlyTemplate: base.InternalOnly}, values)
	if err != nil {
		return RenderedMail{}, ErrInvalidInput
	}
	return rendered, nil
}

func templateSamples(base AdminMailTemplateView) map[string]string {
	values := make(map[string]string, len(base.VariableAllowlist))
	for _, name := range base.VariableAllowlist {
		values[name] = "example"
	}
	return values
}

// Published versions are immutable: queued mail keeps its original content.
// The new version and its audit record commit together.
func (s *Store) UpdateAdminMailTemplate(ctx context.Context, key string, patch AdminMailTemplatePatch) (AdminMailTemplateView, error) {
	var item AdminMailTemplateView
	if s == nil || s.db == nil || patch.ExpectedVersion == 0 || patch.ExpectedVersion == ^uint64(0) || SupportAuditActor(ctx) == "" {
		return item, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return item, err
	}
	defer tx.Rollback()
	var allow []byte
	err = tx.QueryRowContext(ctx, `SELECT template_key,locale,version,subject_template,text_template,html_template,variable_allowlist_json,internal_only,enabled,updated_at FROM mail_templates WHERE template_key=? AND locale=? ORDER BY version DESC LIMIT 1 FOR UPDATE`, key, patch.Locale).Scan(&item.Key, &item.Locale, &item.Version, &item.SubjectTemplate, &item.TextTemplate, &item.HTMLTemplate, &allow, &item.InternalOnly, &item.Enabled, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrSupportNotFound
	}
	if err != nil {
		return item, err
	}
	if err = json.Unmarshal(allow, &item.VariableAllowlist); err != nil {
		return item, err
	}
	if _, err = renderAdminTemplate(item, patch, templateSamples(item)); err != nil {
		return item, err
	}
	item.Version++
	_, err = tx.ExecContext(ctx, `INSERT INTO mail_templates(template_key,locale,version,subject_template,text_template,html_template,variable_allowlist_json,internal_only,enabled) VALUES (?,?,?,?,?,?,?,?,?)`, item.Key, item.Locale, item.Version, patch.Subject, patch.Text, patch.HTML, allow, item.InternalOnly, item.Enabled)
	if err != nil {
		return item, err
	}
	err = recordSupportAuditExecer(ctx, tx, SupportAuditInput{ActorID: SupportAuditActor(ctx), Action: "admin_mail_template_updated", ResourceType: "mail_template", ResourceID: item.Key + ":" + item.Locale, CorrelationID: SupportAuditCorrelation(ctx), Result: AuditSuccess, Metadata: map[string]string{"template_key": item.Key, "version": strconv.FormatUint(item.Version, 10)}})
	if err != nil {
		return item, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT updated_at FROM mail_templates WHERE template_key=? AND locale=? AND version=?`, item.Key, item.Locale, item.Version).Scan(&item.UpdatedAt); err != nil {
		return item, err
	}
	if err = tx.Commit(); err != nil {
		return item, err
	}
	item.SubjectTemplate, item.TextTemplate, item.HTMLTemplate = patch.Subject, patch.Text, patch.HTML
	return item, nil
}

func (s *AuditedAdminMailStore) UpdateAdminMailTemplate(ctx context.Context, key string, patch AdminMailTemplatePatch) (AdminMailTemplateView, error) {
	editor, ok := s.inner.(adminMailTemplateEditor)
	if !ok {
		return AdminMailTemplateView{}, ErrAuthenticationUnavailable
	}
	return editor.UpdateAdminMailTemplate(ctx, key, patch)
}

func (a *AdminMailAPI) patchTemplate(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.mailAdminActor(w, r)
	if !ok {
		return
	}
	var patch AdminMailTemplatePatch
	if !decodeSupportJSON(w, r, &patch) {
		return
	}
	editor, ok := a.store.(adminMailTemplateEditor)
	if !ok {
		writeSupportError(w, r, 503, "template_editor_unavailable", "Template editing is unavailable.")
		return
	}
	item, err := editor.UpdateAdminMailTemplate(WithSupportAuditActor(r.Context(), principal.UserID), r.PathValue("key"), patch)
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	writeSupportJSON(w, 200, map[string]any{"template": item})
}

func (a *AdminMailAPI) previewTemplate(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.mailAdminActor(w, r); !ok {
		return
	}
	var patch AdminMailTemplatePatch
	if !decodeSupportJSON(w, r, &patch) {
		return
	}
	items, err := a.store.ListAdminMailTemplates(r.Context())
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	var base *AdminMailTemplateView
	for i := range items {
		if items[i].Key == r.PathValue("key") && items[i].Locale == patch.Locale && (base == nil || items[i].Version > base.Version) {
			base = &items[i]
		}
	}
	if base == nil {
		writeSupportStoreError(w, r, ErrSupportNotFound)
		return
	}
	rendered, err := renderAdminTemplate(*base, patch, templateSamples(*base))
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	writeSupportJSON(w, 200, map[string]any{"preview": rendered, "sample_values": true})
}
