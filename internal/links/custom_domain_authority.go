package links

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrCustomDomainUnavailable = errors.New("custom domain unavailable")

// CustomDomainAssignmentAuthority is a server-owned same-transaction guard.
// It returns the canonical hostname only when the Workspace currently has
// mutation entitlement and the domain's Ownership / Ingress DNS / HTTPS / Risk
// axes are all ready. Implementations must lock the authoritative domain row
// until the Link transaction commits.
type CustomDomainAssignmentAuthority interface {
	AuthorizeCustomDomainAssignmentTx(ctx context.Context, tx *sql.Tx, workspaceID, hostname string, now time.Time) (canonicalHostname string, err error)
}

// CustomDomainRoutingAuthority is the runtime redirect authority. It differs
// from assignment by consuming the current existing-routing policy, including
// only the exact normal-downgrade grace allowed by P06.
type CustomDomainRoutingAuthority interface {
	AuthorizeCustomDomainRoutingTx(ctx context.Context, tx *sql.Tx, workspaceID, hostname string, now time.Time) (canonicalHostname string, err error)
}

type CustomDomainAuthority interface {
	CustomDomainAssignmentAuthority
	CustomDomainRoutingAuthority
}

func NewMySQLStoreWithCustomDomainAuthority(db *sql.DB, authority CustomDomainAuthority) *MySQLStore {
	return &MySQLStore{db: db, customDomainAuthority: authority}
}

func (s *MySQLStore) authorizeCustomDomainTx(ctx context.Context, tx *sql.Tx, workspaceID, hostname, domainKind string) (string, error) {
	if domainKind != "custom" {
		return hostname, nil
	}
	if s == nil || s.customDomainAuthority == nil {
		return "", ErrCustomDomainUnavailable
	}
	canonical, err := s.customDomainAuthority.AuthorizeCustomDomainAssignmentTx(ctx, tx, workspaceID, hostname, time.Now().UTC())
	if err != nil || canonical == "" {
		return "", ErrCustomDomainUnavailable
	}
	return canonical, nil
}

func (s *MySQLStore) authorizeCustomDomainRoutingTx(ctx context.Context, tx *sql.Tx, workspaceID, hostname, domainKind string, now time.Time) (string, error) {
	if domainKind != "custom" {
		return hostname, nil
	}
	if s == nil || s.customDomainAuthority == nil || now.IsZero() {
		return "", ErrCustomDomainUnavailable
	}
	canonical, err := s.customDomainAuthority.AuthorizeCustomDomainRoutingTx(ctx, tx, workspaceID, hostname, now.UTC())
	if err != nil || canonical == "" {
		return "", ErrCustomDomainUnavailable
	}
	return canonical, nil
}
