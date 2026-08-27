-- GoJet V10 / P16 Trust, Destination Risk and Abuse
-- Repository-global immutable migration: 000022
-- MySQL 8.x
--
-- P16 owns durable domain reputation/provider/revalidation evidence. The
-- inherited P06 custom_domains row remains the runtime/domain-axis projection
-- authority and is not replaced or collapsed by these tables.

CREATE TABLE domain_risk_evaluations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) NOT NULL,
    domain_id BIGINT UNSIGNED NOT NULL,
    hostname_ascii VARCHAR(253) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    policy_version VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    request_kind ENUM('initial', 'revalidation') NOT NULL,
    idempotency_key VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    state ENUM('pending', 'revalidating', 'allow', 'review', 'block', 'malformed', 'stale', 'provider_partial') NOT NULL,
    reason_category VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    actor_id VARCHAR(128) NOT NULL,
    valid_until DATETIME(6) NULL,
    checked_at DATETIME(6) NULL,
    next_due_at DATETIME(6) NULL,
    entitlement_snapshot VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    ownership_snapshot VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    ingress_dns_snapshot VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    https_snapshot VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    routing_snapshot VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_domain_risk_idempotency (workspace_id, idempotency_key),
    KEY idx_domain_risk_domain_created (domain_id, created_at, id),
    KEY idx_domain_risk_workspace_state (workspace_id, state, next_due_at, id),
    KEY idx_domain_risk_correlation (correlation_id),
    CONSTRAINT fk_domain_risk_evaluation_domain FOREIGN KEY (domain_id) REFERENCES custom_domains(id) ON DELETE RESTRICT,
    CONSTRAINT chk_domain_risk_validity CHECK (valid_until IS NULL OR checked_at IS NULL OR valid_until > checked_at),
    CONSTRAINT chk_domain_risk_next_due CHECK (next_due_at IS NULL OR checked_at IS NULL OR next_due_at > checked_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE domain_risk_provider_observations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    evaluation_id BIGINT UNSIGNED NOT NULL,
    provider VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    outcome ENUM('allow', 'review', 'block', 'unknown', 'unavailable') NOT NULL,
    signal_code VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    evidence_json JSON NOT NULL,
    observed_at DATETIME(6) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_domain_risk_provider (evaluation_id, provider),
    KEY idx_domain_risk_provider_outcome (provider, outcome, observed_at, id),
    CONSTRAINT fk_domain_risk_provider_evaluation FOREIGN KEY (evaluation_id) REFERENCES domain_risk_evaluations(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE domain_risk_audit_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) NOT NULL,
    domain_id BIGINT UNSIGNED NOT NULL,
    evaluation_id BIGINT UNSIGNED NULL,
    actor_id VARCHAR(128) NOT NULL,
    action VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    result ENUM('success', 'denied', 'conflict', 'failed') NOT NULL,
    reason_category VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    metadata_json JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_domain_risk_audit_workspace_created (workspace_id, created_at, id),
    KEY idx_domain_risk_audit_domain_created (domain_id, created_at, id),
    KEY idx_domain_risk_audit_correlation (correlation_id),
    CONSTRAINT fk_domain_risk_audit_domain FOREIGN KEY (domain_id) REFERENCES custom_domains(id) ON DELETE RESTRICT,
    CONSTRAINT fk_domain_risk_audit_evaluation FOREIGN KEY (evaluation_id) REFERENCES domain_risk_evaluations(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;