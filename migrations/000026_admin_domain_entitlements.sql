-- GoJet V10 / P17 Admin Domain Entitlement Governance
-- Repository-global immutable migration: 000026
-- MySQL 8.x
-- P17 owns administrator decisions/control materialization only. P06/P13 stay
-- authoritative for custom-domain entitlement sources/resolution and P16 safety
-- remains independently authoritative.

ALTER TABLE custom_domain_entitlement_requests
    MODIFY COLUMN status ENUM('requested','approved','denied','withdrawn') NOT NULL DEFAULT 'requested';

CREATE TABLE admin_domain_entitlement_controls (
    workspace_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    state ENUM('suspended','revoked') NOT NULL,
    reason VARCHAR(500) NOT NULL,
    actor_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    decision_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    effective_at DATETIME(6) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (workspace_id),
    KEY idx_admin_domain_entitlement_control_state (state, effective_at, workspace_id),
    KEY idx_admin_domain_entitlement_control_decision (decision_id),
    CONSTRAINT chk_admin_domain_entitlement_control_reason CHECK (CHAR_LENGTH(TRIM(reason)) > 0),
    CONSTRAINT chk_admin_domain_entitlement_control_actor CHECK (CHAR_LENGTH(TRIM(actor_id)) > 0),
    CONSTRAINT chk_admin_domain_entitlement_control_decision CHECK (CHAR_LENGTH(TRIM(decision_id)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE admin_domain_entitlement_decisions (
    id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    workspace_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    action ENUM('approve','deny','suspend','revoke','restore') NOT NULL,
    actor_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    request_correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason VARCHAR(500) NOT NULL,
    support_ticket_id VARCHAR(128) NULL,
    source_id BIGINT UNSIGNED NULL,
    domain_limit INT UNSIGNED NULL,
    starts_at DATETIME(6) NULL,
    expires_at DATETIME(6) NULL,
    effective_at DATETIME(6) NULL,
    scope VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    confirmation VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    existing_link_impact VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    user_visible_category VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    current_security_ownership_evidence VARCHAR(500) NULL,
    affected_routes INT UNSIGNED NOT NULL DEFAULT 0,
    before_json JSON NOT NULL,
    after_json JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_admin_domain_entitlement_decisions_workspace (workspace_id, created_at, id),
    KEY idx_admin_domain_entitlement_decisions_actor (actor_id, created_at, id),
    KEY idx_admin_domain_entitlement_decisions_correlation (request_correlation_id),
    KEY idx_admin_domain_entitlement_decisions_ticket (support_ticket_id),
    CONSTRAINT chk_admin_domain_entitlement_decision_actor CHECK (CHAR_LENGTH(TRIM(actor_id)) > 0),
    CONSTRAINT chk_admin_domain_entitlement_decision_correlation CHECK (CHAR_LENGTH(TRIM(request_correlation_id)) > 0),
    CONSTRAINT chk_admin_domain_entitlement_decision_reason CHECK (CHAR_LENGTH(TRIM(reason)) > 0),
    CONSTRAINT chk_admin_domain_entitlement_approve CHECK (
        action <> 'approve' OR (
            domain_limit IS NOT NULL AND domain_limit > 0
            AND starts_at IS NOT NULL
            AND expires_at IS NOT NULL AND expires_at > starts_at
            AND support_ticket_id IS NOT NULL AND CHAR_LENGTH(TRIM(support_ticket_id)) > 0
            AND source_id IS NOT NULL
        )
    ),
    CONSTRAINT chk_admin_domain_entitlement_deny CHECK (
        action <> 'deny' OR (
            user_visible_category IS NOT NULL AND CHAR_LENGTH(TRIM(user_visible_category)) > 0
        )
    ),
    CONSTRAINT chk_admin_domain_entitlement_suspend CHECK (
        action <> 'suspend' OR (
            effective_at IS NOT NULL
            AND scope IS NOT NULL AND CHAR_LENGTH(TRIM(scope)) > 0
        )
    ),
    CONSTRAINT chk_admin_domain_entitlement_revoke CHECK (
        action <> 'revoke' OR (
            confirmation = 'REVOKE'
            AND existing_link_impact = 'disable_existing_routing'
        )
    ),
    CONSTRAINT chk_admin_domain_entitlement_restore CHECK (
        action <> 'restore' OR (
            current_security_ownership_evidence IS NOT NULL
            AND CHAR_LENGTH(TRIM(current_security_ownership_evidence)) > 0
        )
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

DELIMITER $$

CREATE TRIGGER trg_admin_domain_entitlement_decisions_no_update
BEFORE UPDATE ON admin_domain_entitlement_decisions
FOR EACH ROW
BEGIN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='admin_domain_entitlement_decisions is append-only';
END$$

CREATE TRIGGER trg_admin_domain_entitlement_decisions_no_delete
BEFORE DELETE ON admin_domain_entitlement_decisions
FOR EACH ROW
BEGIN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='admin_domain_entitlement_decisions is append-only';
END$$

DELIMITER ;
