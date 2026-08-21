-- GoJet V10 / P06 Custom Domains
-- Repository-global immutable migration: 000002
-- MySQL 8.x

CREATE TABLE custom_domain_entitlement_sources (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) NOT NULL,
    source ENUM('plan', 'manual_approval') NOT NULL,
    source_key VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status ENUM('active', 'suspended', 'expired', 'revoked') NOT NULL,
    domain_limit INT UNSIGNED NOT NULL,
    starts_at DATETIME(6) NOT NULL,
    expires_at DATETIME(6) NULL,
    degraded_at DATETIME(6) NULL,
    grace_until DATETIME(6) NULL,
    granted_by VARCHAR(128) NULL,
    support_ticket_id VARCHAR(128) NULL,
    decision_reason VARCHAR(500) NULL,
    security_category VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_domain_entitlement_source (workspace_id, source, source_key),
    KEY idx_domain_entitlement_workspace_status (workspace_id, status, starts_at, expires_at),
    CONSTRAINT chk_domain_entitlement_limit CHECK (domain_limit > 0),
    CONSTRAINT chk_domain_entitlement_window CHECK (expires_at IS NULL OR expires_at > starts_at),
    CONSTRAINT chk_domain_entitlement_grace CHECK (
        (degraded_at IS NULL AND grace_until IS NULL)
        OR (source = 'plan' AND degraded_at IS NOT NULL AND grace_until IS NOT NULL AND grace_until > degraded_at)
    ),
    CONSTRAINT chk_domain_manual_authority CHECK (
        source <> 'manual_approval'
        OR (
            granted_by IS NOT NULL
            AND CHAR_LENGTH(TRIM(granted_by)) > 0
            AND support_ticket_id IS NOT NULL
            AND CHAR_LENGTH(TRIM(support_ticket_id)) > 0
            AND decision_reason IS NOT NULL
            AND CHAR_LENGTH(TRIM(decision_reason)) > 0
            AND expires_at IS NOT NULL
        )
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE custom_domain_entitlement_requests (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) NOT NULL,
    support_ticket_id VARCHAR(128) NOT NULL,
    requested_domain_limit INT UNSIGNED NULL,
    status ENUM('requested', 'denied', 'withdrawn') NOT NULL DEFAULT 'requested',
    decision_reason VARCHAR(500) NULL,
    submitted_at DATETIME(6) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_domain_request_ticket (support_ticket_id),
    KEY idx_domain_request_workspace_status (workspace_id, status, submitted_at),
    CONSTRAINT chk_domain_request_limit CHECK (requested_domain_limit IS NULL OR requested_domain_limit > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE custom_domain_usage (
    workspace_id VARCHAR(64) NOT NULL,
    allocated_count INT UNSIGNED NOT NULL DEFAULT 0,
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (workspace_id),
    CONSTRAINT chk_domain_usage_version CHECK (version >= 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE custom_domains (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) NOT NULL,
    hostname_ascii VARCHAR(253) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    display_hostname VARCHAR(253) NOT NULL,
    routing_state ENUM('pending', 'enabled', 'suspended', 'revoked', 'removed') NOT NULL DEFAULT 'pending',
    ownership_status ENUM('pending', 'verified', 'failed', 'lost') NOT NULL DEFAULT 'pending',
    ingress_dns_status ENUM('pending', 'valid', 'invalid') NOT NULL DEFAULT 'pending',
    https_status ENUM('pending', 'active', 'error') NOT NULL DEFAULT 'pending',
    risk_status ENUM('missing', 'allow', 'review', 'block', 'malformed', 'stale') NOT NULL DEFAULT 'missing',
    ownership_token_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    ownership_secret_hash BINARY(32) NOT NULL,
    ownership_secret_issued_at DATETIME(6) NOT NULL,
    ownership_verified_at DATETIME(6) NULL,
    ingress_dns_checked_at DATETIME(6) NULL,
    https_checked_at DATETIME(6) NULL,
    risk_checked_at DATETIME(6) NULL,
    risk_policy_version VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL,
    risk_evidence_ref VARCHAR(255) NULL,
    grace_started_at DATETIME(6) NULL,
    grace_until DATETIME(6) NULL,
    security_category VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    removed_at DATETIME(6) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_custom_domains_hostname (hostname_ascii),
    KEY idx_custom_domains_workspace_state (workspace_id, routing_state, id),
    KEY idx_custom_domains_workspace_axes (workspace_id, ownership_status, ingress_dns_status, https_status, risk_status),
    CONSTRAINT chk_custom_domain_token_version CHECK (ownership_token_version >= 1),
    CONSTRAINT chk_custom_domain_grace CHECK (
        (grace_started_at IS NULL AND grace_until IS NULL)
        OR (grace_started_at IS NOT NULL AND grace_until IS NOT NULL AND grace_until > grace_started_at)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE custom_domain_revalidations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    domain_id BIGINT UNSIGNED NOT NULL,
    workspace_id VARCHAR(64) NOT NULL,
    axis ENUM('entitlement', 'ownership', 'ingress_dns', 'https', 'risk') NOT NULL,
    result ENUM('pass', 'fail', 'pending', 'stale', 'error') NOT NULL,
    policy_version VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    checked_at DATETIME(6) NOT NULL,
    next_due_at DATETIME(6) NULL,
    evidence_ref VARCHAR(255) NULL,
    correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    metadata_json JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_domain_revalidation_domain_checked (domain_id, checked_at, id),
    KEY idx_domain_revalidation_workspace_checked (workspace_id, checked_at, id),
    KEY idx_domain_revalidation_correlation (correlation_id),
    CONSTRAINT fk_domain_revalidation_domain FOREIGN KEY (domain_id) REFERENCES custom_domains(id) ON DELETE RESTRICT,
    CONSTRAINT chk_domain_revalidation_due CHECK (next_due_at IS NULL OR next_due_at > checked_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE custom_domain_audit_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) NOT NULL,
    domain_id BIGINT UNSIGNED NULL,
    entitlement_source_id BIGINT UNSIGNED NULL,
    actor_id VARCHAR(128) NOT NULL,
    action VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    result ENUM('success', 'denied', 'conflict', 'failed') NOT NULL,
    reason VARCHAR(500) NULL,
    correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    metadata_json JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_domain_audit_workspace_created (workspace_id, created_at, id),
    KEY idx_domain_audit_domain_created (domain_id, created_at, id),
    KEY idx_domain_audit_correlation (correlation_id),
    CONSTRAINT fk_domain_audit_domain FOREIGN KEY (domain_id) REFERENCES custom_domains(id) ON DELETE RESTRICT,
    CONSTRAINT fk_domain_audit_entitlement FOREIGN KEY (entitlement_source_id) REFERENCES custom_domain_entitlement_sources(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;