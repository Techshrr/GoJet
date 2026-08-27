package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT011(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("real cross-resource administrator projections with exact links/domains/content permission separation and redacted QR/Text/Bio inventory")
	now := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	service, root, _, err := bootstrapCaseRoot(ctx, runtime, "T011", nil, now)
	if err != nil {
		return out, err
	}

	const ws = "ws_p17_t011"
	res, err := runtime.DB.ExecContext(ctx, `INSERT INTO links(workspace_id,hostname,domain_kind,code,title,primary_destination,redirect_status,status,version,risk_fingerprint,routing_json,ab_json,utm_json,access_json,created_at,updated_at) VALUES (?,'go.p17.test','official','t011','T011 Link','https://example.test/t011',302,'active',1,REPEAT('a',64),JSON_OBJECT(),JSON_OBJECT(),JSON_OBJECT(),JSON_OBJECT(),?,?)`, ws, now, now)
	if err != nil {
		return out, err
	}
	linkID, err := res.LastInsertId()
	if err != nil {
		return out, err
	}
	res, err = runtime.DB.ExecContext(ctx, `INSERT INTO custom_domains(workspace_id,hostname_ascii,display_hostname,routing_state,ownership_status,ingress_dns_status,https_status,risk_status,ownership_secret_hash,ownership_secret_issued_at,risk_policy_version,created_at,updated_at) VALUES (?,'admin-t011.example','admin-t011.example','enabled','verified','valid','active','allow',UNHEX(REPEAT('11',32)),?,'p16-policy',?,?)`, ws, now, now, now)
	if err != nil {
		return out, err
	}
	domainID, err := res.LastInsertId()
	if err != nil {
		return out, err
	}
	res, err = runtime.DB.ExecContext(ctx, `INSERT INTO qr_codes(workspace_id,source_link_id,label,created_by,created_at,updated_at) VALUES (?,?,'QR safe label','fixture',?,?)`, ws, linkID, now, now)
	if err != nil {
		return out, err
	}
	qrID, _ := res.LastInsertId()
	res, err = runtime.DB.ExecContext(ctx, `INSERT INTO text_shares(workspace_id,public_slug,title,content,visibility,password_hash,one_time,version,created_by,created_at,updated_at) VALUES (?,'t011-text-slug-00000001','Text safe title','T011_PRIVATE_TEXT_BODY_MUST_NOT_EXPOSE','private','T011_PRIVATE_PASSWORD_HASH_MUST_NOT_EXPOSE',0,1,'fixture',?,?)`, ws, now, now)
	if err != nil {
		return out, err
	}
	textID, _ := res.LastInsertId()
	res, err = runtime.DB.ExecContext(ctx, `INSERT INTO bio_pages(workspace_id,slug,title,bio,status,version,created_by,created_at,updated_at) VALUES (?,'t011-bio-slug-000000001','Bio safe title','T011_PRIVATE_BIO_BODY_MUST_NOT_EXPOSE','paused',1,'fixture',?,?)`, ws, now, now)
	if err != nil {
		return out, err
	}
	bioID, _ := res.LastInsertId()

	_, linksLogin, err := createScopedMFAAdmin(ctx, service, root, "T011", "links", adminaccess.PermissionLinksManage, now.Add(10*time.Second))
	if err != nil {
		return out, err
	}
	_, domainsLogin, err := createScopedMFAAdmin(ctx, service, root, "T011", "domains", adminaccess.PermissionDomainsManage, now.Add(20*time.Second))
	if err != nil {
		return out, err
	}
	_, contentLogin, err := createScopedMFAAdmin(ctx, service, root, "T011", "content", adminaccess.PermissionContentManage, now.Add(30*time.Second))
	if err != nil {
		return out, err
	}
	server, err := adminfixture.NewExtendedHTTPServer(service, nil)
	if err != nil {
		return out, err
	}
	defer server.Close()

	linksOK, err := adminfixture.Request(ctx, server, http.MethodGet, "/api/admin/links/"+itoa64(uint64(linkID)), "", linksLogin.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}
	linksNoDomains, err := adminfixture.Request(ctx, server, http.MethodGet, "/api/admin/domains/"+itoa64(uint64(domainID)), "", linksLogin.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}
	linksNoContent, err := adminfixture.Request(ctx, server, http.MethodGet, "/api/admin/resources/qr/"+itoa64(uint64(qrID)), "", linksLogin.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}
	domainsOK, err := adminfixture.Request(ctx, server, http.MethodGet, "/api/admin/domains/"+itoa64(uint64(domainID)), "", domainsLogin.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}
	domainsNoLinks, err := adminfixture.Request(ctx, server, http.MethodGet, "/api/admin/links/"+itoa64(uint64(linkID)), "", domainsLogin.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}
	qrOK, err := adminfixture.Request(ctx, server, http.MethodGet, "/api/admin/resources/qr/"+itoa64(uint64(qrID)), "", contentLogin.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}
	textOK, err := adminfixture.Request(ctx, server, http.MethodGet, "/api/admin/resources/text/"+itoa64(uint64(textID)), "", contentLogin.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}
	bioOK, err := adminfixture.Request(ctx, server, http.MethodGet, "/api/admin/resources/bio/"+itoa64(uint64(bioID)), "", contentLogin.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}
	contentNoLinks, err := adminfixture.Request(ctx, server, http.MethodGet, "/api/admin/links/"+itoa64(uint64(linkID)), "", contentLogin.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}
	contentNoDomains, err := adminfixture.Request(ctx, server, http.MethodGet, "/api/admin/domains/"+itoa64(uint64(domainID)), "", contentLogin.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}

	catalogCount, err := scalarInt(ctx, runtime.DB, `SELECT COUNT(*) FROM admin_permissions`)
	if err != nil {
		return out, err
	}
	wildcardCount, err := scalarInt(ctx, runtime.DB, `SELECT COUNT(*) FROM admin_permissions WHERE permission LIKE '%*%' OR permission IN ('qr.manage','text.manage','bio.manage')`)
	if err != nil {
		return out, err
	}
	redactedRaw := strings.ToLower(qrOK.Raw + textOK.Raw + bioOK.Raw)

	out.RecordCounts = map[string]int{"permissions": catalogCount, "links": 1, "domains": 1, "content_resources": 3}
	out.Checks = map[string]bool{
		"links_permission_reads_only_links":                                 linksOK.Status == http.StatusOK && linksNoDomains.Status == http.StatusForbidden && linksNoContent.Status == http.StatusForbidden,
		"domains_permission_reads_only_domains":                             domainsOK.Status == http.StatusOK && domainsNoLinks.Status == http.StatusForbidden,
		"content_permission_reads_qr_text_bio":                              qrOK.Status == http.StatusOK && textOK.Status == http.StatusOK && bioOK.Status == http.StatusOK,
		"content_permission_does_not_escalate_links_domains":                contentNoLinks.Status == http.StatusForbidden && contentNoDomains.Status == http.StatusForbidden,
		"qr_text_bio_projection_redacts_user_content_and_password_material": !strings.Contains(redactedRaw, "t011_private_text_body_must_not_expose") && !strings.Contains(redactedRaw, "t011_private_password_hash_must_not_expose") && !strings.Contains(redactedRaw, "t011_private_bio_body_must_not_expose") && !strings.Contains(redactedRaw, "password_hash") && !strings.Contains(redactedRaw, "\"content\":") && !strings.Contains(redactedRaw, "\"bio\":"),
		"admin_resource_surfaces_are_no_store_noindex":                      adminfixture.NoStoreNoIndex(linksOK) && adminfixture.NoStoreNoIndex(domainsOK) && adminfixture.NoStoreNoIndex(qrOK) && adminfixture.NoStoreNoIndex(textOK) && adminfixture.NoStoreNoIndex(bioOK),
		"exact_frozen_permission_catalog_preserved":                         catalogCount == len(adminaccess.PermissionCatalog) && wildcardCount == 0 && len(adminaccess.PermissionCatalog) == 16,
	}
	pass(&out)
	return out, nil
}

func itoa64(v uint64) string { return fmt.Sprintf("%d", v) }
