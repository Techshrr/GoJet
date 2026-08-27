package domains

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

// ApplyAdminEntitlementControl overlays only the P17 administrator governance
// state on top of the already-resolved P06/P13 entitlement. It deliberately
// does not reinterpret source precedence, domain_limit, downgrade grace or any
// P16 domain-safety verdict. Removing the control reveals the unchanged base
// resolver result.
//
// Historical P06 integration fixtures intentionally apply only migrations
// 000001/000002. Missing-table error 1146 is therefore treated as "P17 not
// installed" for predecessor-only fixtures. Production P17 startup separately
// asserts the 000026 schema before administrator governance can be enabled.
func ApplyAdminEntitlementControl(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, workspaceID string, now time.Time, base ResolvedEntitlement) (ResolvedEntitlement, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return ResolvedEntitlement{}, ErrInvalidEntitlementSource
	}

	var state EntitlementStatus
	var reason string
	var effectiveAt time.Time
	err := q.QueryRowContext(ctx, `
		SELECT state, reason, effective_at
		FROM admin_domain_entitlement_controls
		WHERE workspace_id = ?`, workspaceID).Scan(&state, &reason, &effectiveAt)
	if errors.Is(err, sql.ErrNoRows) {
		return base, nil
	}
	if err != nil {
		if isMissingAdminEntitlementControlTable(err) {
			return base, nil
		}
		return ResolvedEntitlement{}, err
	}
	if now.UTC().Before(effectiveAt.UTC()) {
		return base, nil
	}
	if state != EntitlementSuspended && state != EntitlementRevoked {
		return ResolvedEntitlement{}, ErrInvalidEntitlementSource
	}

	controlled := base
	controlled.Status = state
	controlled.DecisionReason = strings.TrimSpace(reason)
	if controlled.DecisionReason == "" {
		return ResolvedEntitlement{}, ErrInvalidEntitlementSource
	}
	controlled.GracePeriod = false
	controlled.GraceUntil = nil
	controlled.MutationAllowed = false
	controlled.ExistingRoutingAllowed = false
	return controlled, nil
}

func isMissingAdminEntitlementControlTable(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1146
}
