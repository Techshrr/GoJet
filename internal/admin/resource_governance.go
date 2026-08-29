package admin

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type ManagedLink struct {
	ID                 uint64    `json:"id"`
	WorkspaceID        string    `json:"workspace_id"`
	Hostname           string    `json:"hostname"`
	DomainKind         string    `json:"domain_kind"`
	Code               string    `json:"code"`
	Title              string    `json:"title"`
	PrimaryDestination string    `json:"primary_destination"`
	RedirectStatus     int       `json:"redirect_status"`
	Status             string    `json:"status"`
	Version            uint64    `json:"version"`
	RiskFingerprint    string    `json:"risk_fingerprint"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type ManagedDomain struct {
	ID               uint64    `json:"id"`
	WorkspaceID      string    `json:"workspace_id"`
	HostnameASCII    string    `json:"hostname_ascii"`
	DisplayHostname  string    `json:"display_hostname"`
	RoutingState     string    `json:"routing_state"`
	OwnershipStatus  string    `json:"ownership_status"`
	IngressDNSStatus string    `json:"ingress_dns_status"`
	HTTPSStatus      string    `json:"https_status"`
	RiskStatus       string    `json:"risk_status"`
	RiskPolicy       *string   `json:"risk_policy_version,omitempty"`
	SecurityCategory *string   `json:"security_category,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// ManagedContentResource is the deliberately redacted administrator inventory
// projection for P08 QR, P10 Text and P11 Bio resources. The frozen P17
// permission catalog has no qr.manage/text.manage/bio.manage permissions, so
// content.manage is the dedicated resource-inventory permission for this
// grouped IA surface. It never implies links.manage/domains.manage/files.manage.
type ManagedContentResource struct {
	Kind        string    `json:"kind"`
	ID          uint64    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Label       string    `json:"label"`
	State       string    `json:"state"`
	Deleted     bool      `json:"deleted"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (s *Service) ListManagedLinks(ctx context.Context, p Principal, limit int) ([]ManagedLink, error) {
	if err := s.Require(p, PermissionLinksManage); err != nil {
		return nil, err
	}
	limit, err := normalizeAdminEnumerationLimit(limit)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id,workspace_id,hostname,domain_kind,code,title,primary_destination,redirect_status,status,version,risk_fingerprint,created_at,updated_at
FROM links
ORDER BY updated_at DESC,id DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ManagedLink, 0)
	for rows.Next() {
		var item ManagedLink
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Hostname, &item.DomainKind, &item.Code, &item.Title, &item.PrimaryDestination, &item.RedirectStatus, &item.Status, &item.Version, &item.RiskFingerprint, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GetManagedLink(ctx context.Context, p Principal, id uint64) (ManagedLink, error) {
	if err := s.Require(p, PermissionLinksManage); err != nil {
		return ManagedLink{}, err
	}
	if id == 0 {
		return ManagedLink{}, ErrInvalid
	}
	var item ManagedLink
	err := s.db.QueryRowContext(ctx, `
SELECT id,workspace_id,hostname,domain_kind,code,title,primary_destination,redirect_status,status,version,risk_fingerprint,created_at,updated_at
FROM links WHERE id=?`, id).Scan(&item.ID, &item.WorkspaceID, &item.Hostname, &item.DomainKind, &item.Code, &item.Title, &item.PrimaryDestination, &item.RedirectStatus, &item.Status, &item.Version, &item.RiskFingerprint, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ManagedLink{}, ErrNotFound
	}
	return item, err
}

func (s *Service) ListManagedDomains(ctx context.Context, p Principal, limit int) ([]ManagedDomain, error) {
	if err := s.Require(p, PermissionDomainsManage); err != nil {
		return nil, err
	}
	limit, err := normalizeAdminEnumerationLimit(limit)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id,workspace_id,hostname_ascii,display_hostname,routing_state,ownership_status,ingress_dns_status,https_status,risk_status,risk_policy_version,security_category,created_at,updated_at
FROM custom_domains
WHERE routing_state <> 'removed'
ORDER BY updated_at DESC,id DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ManagedDomain, 0)
	for rows.Next() {
		item, err := scanManagedDomain(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GetManagedDomain(ctx context.Context, p Principal, id uint64) (ManagedDomain, error) {
	if err := s.Require(p, PermissionDomainsManage); err != nil {
		return ManagedDomain{}, err
	}
	if id == 0 {
		return ManagedDomain{}, ErrInvalid
	}
	item, err := scanManagedDomain(s.db.QueryRowContext(ctx, `
SELECT id,workspace_id,hostname_ascii,display_hostname,routing_state,ownership_status,ingress_dns_status,https_status,risk_status,risk_policy_version,security_category,created_at,updated_at
FROM custom_domains WHERE id=? AND routing_state <> 'removed'`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return ManagedDomain{}, ErrNotFound
	}
	return item, err
}

func scanManagedDomain(row scanner) (ManagedDomain, error) {
	var item ManagedDomain
	var policy, category sql.NullString
	if err := row.Scan(&item.ID, &item.WorkspaceID, &item.HostnameASCII, &item.DisplayHostname, &item.RoutingState, &item.OwnershipStatus, &item.IngressDNSStatus, &item.HTTPSStatus, &item.RiskStatus, &policy, &category, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return ManagedDomain{}, err
	}
	if policy.Valid {
		v := strings.TrimSpace(policy.String)
		item.RiskPolicy = &v
	}
	if category.Valid {
		v := strings.TrimSpace(category.String)
		item.SecurityCategory = &v
	}
	return item, nil
}

func (s *Service) ListManagedContentResources(ctx context.Context, p Principal, kind string, limit int) ([]ManagedContentResource, error) {
	if err := s.Require(p, PermissionContentManage); err != nil {
		return nil, err
	}
	kind = strings.TrimSpace(kind)
	if kind != "qr" && kind != "text" && kind != "bio" {
		return nil, ErrInvalid
	}
	limit, err := normalizeAdminEnumerationLimit(limit)
	if err != nil {
		return nil, err
	}
	var query string
	switch kind {
	case "qr":
		query = `SELECT id,workspace_id,label,CASE WHEN deleted_at IS NULL THEN 'active' ELSE 'deleted' END,deleted_at IS NOT NULL,updated_at FROM qr_codes ORDER BY updated_at DESC,id DESC LIMIT ?`
	case "text":
		query = `SELECT id,workspace_id,title,CASE WHEN deleted_at IS NOT NULL THEN 'deleted' ELSE visibility END,deleted_at IS NOT NULL,updated_at FROM text_shares ORDER BY updated_at DESC,id DESC LIMIT ?`
	case "bio":
		query = `SELECT id,workspace_id,title,CASE WHEN deleted_at IS NOT NULL THEN 'deleted' ELSE status END,deleted_at IS NOT NULL,updated_at FROM bio_pages ORDER BY updated_at DESC,id DESC LIMIT ?`
	}
	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ManagedContentResource, 0)
	for rows.Next() {
		var item ManagedContentResource
		item.Kind = kind
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Label, &item.State, &item.Deleted, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GetManagedContentResource(ctx context.Context, p Principal, kind string, id uint64) (ManagedContentResource, error) {
	if err := s.Require(p, PermissionContentManage); err != nil {
		return ManagedContentResource{}, err
	}
	kind = strings.TrimSpace(kind)
	if id == 0 || (kind != "qr" && kind != "text" && kind != "bio") {
		return ManagedContentResource{}, ErrInvalid
	}
	var query string
	switch kind {
	case "qr":
		query = `SELECT id,workspace_id,label,CASE WHEN deleted_at IS NULL THEN 'active' ELSE 'deleted' END,deleted_at IS NOT NULL,updated_at FROM qr_codes WHERE id=?`
	case "text":
		query = `SELECT id,workspace_id,title,CASE WHEN deleted_at IS NOT NULL THEN 'deleted' ELSE visibility END,deleted_at IS NOT NULL,updated_at FROM text_shares WHERE id=?`
	case "bio":
		query = `SELECT id,workspace_id,title,CASE WHEN deleted_at IS NOT NULL THEN 'deleted' ELSE status END,deleted_at IS NOT NULL,updated_at FROM bio_pages WHERE id=?`
	}
	item := ManagedContentResource{Kind: kind}
	err := s.db.QueryRowContext(ctx, query, id).Scan(&item.ID, &item.WorkspaceID, &item.Label, &item.State, &item.Deleted, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ManagedContentResource{}, ErrNotFound
	}
	return item, err
}
