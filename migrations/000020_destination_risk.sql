-- GoJet V10 / P16 Trust, Destination Risk and Abuse
-- Repository-global immutable migration: 000020
-- MySQL 8.x

CREATE TABLE destination_risk_scans (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) NOT NULL,
    link_id BIGINT UNSIGNED NOT NULL,
    risk_fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    policy_version VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    request_kind ENUM('initial', 'rescan') NOT NULL,
    idempotency_key VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status ENUM('queued', 'leased', 'retry', 'completed', 'failed') NOT NULL DEFAULT 'queued',
    attempts INT UNSIGNED NOT NULL DEFAULT 0,
    max_attempts INT UNSIGNED NOT NULL DEFAULT 5,
    available_at DATETIME(6) NOT NULL,
    lease_owner VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL,
    lease_expires_at DATETIME(6) NULL,
    correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    last_error_code VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NULL,
    completed_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_destination_risk_scan_idempotency (workspace_id, idempotency_key),
    KEY idx_destination_risk_scan_link_created (link_id, created_at, id),
    KEY idx_destination_risk_scan_queue (status, available_at, lease_expires_at, id),
    KEY idx_destination_risk_scan_fingerprint (link_id, risk_fingerprint, policy_version, id),
    KEY idx_destination_risk_scan_correlation (correlation_id),
    CONSTRAINT fk_destination_risk_scan_link FOREIGN KEY (link_id) REFERENCES links(id) ON DELETE RESTRICT,
    CONSTRAINT chk_destination_risk_scan_attempts CHECK (attempts <= max_attempts),
    CONSTRAINT chk_destination_risk_scan_max_attempts CHECK (max_attempts BETWEEN 1 AND 20),
    CONSTRAINT chk_destination_risk_scan_policy_nonempty CHECK (CHAR_LENGTH(TRIM(policy_version)) > 0),
    CONSTRAINT chk_destination_risk_scan_idempotency_nonempty CHECK (CHAR_LENGTH(TRIM(idempotency_key)) > 0),
    CONSTRAINT chk_destination_risk_scan_correlation_nonempty CHECK (CHAR_LENGTH(TRIM(correlation_id)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE destination_risk_scan_targets (
    scan_id BIGINT UNSIGNED NOT NULL,
    target_order INT UNSIGNED NOT NULL,
    normalized_url TEXT NOT NULL,
    target_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (scan_id, target_order),
    UNIQUE KEY uq_destination_risk_scan_target_hash (scan_id, target_hash),
    CONSTRAINT fk_destination_risk_scan_target_scan FOREIGN KEY (scan_id) REFERENCES destination_risk_scans(id) ON DELETE RESTRICT,
    CONSTRAINT chk_destination_risk_target_order CHECK (target_order >= 1),
    CONSTRAINT chk_destination_risk_target_url_nonempty CHECK (CHAR_LENGTH(TRIM(normalized_url)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE destination_risk_provider_observations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    scan_id BIGINT UNSIGNED NOT NULL,
    provider VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    outcome ENUM('allow', 'review', 'block', 'unknown', 'unavailable') NOT NULL,
    signal_code VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    evidence_json JSON NOT NULL,
    observed_at DATETIME(6) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_destination_risk_provider_scan (scan_id, provider),
    KEY idx_destination_risk_provider_outcome (provider, outcome, observed_at, id),
    CONSTRAINT fk_destination_risk_provider_scan FOREIGN KEY (scan_id) REFERENCES destination_risk_scans(id) ON DELETE RESTRICT,
    CONSTRAINT chk_destination_risk_provider_nonempty CHECK (CHAR_LENGTH(TRIM(provider)) > 0),
    CONSTRAINT chk_destination_risk_signal_nonempty CHECK (CHAR_LENGTH(TRIM(signal_code)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE destination_risk_decisions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) NOT NULL,
    link_id BIGINT UNSIGNED NOT NULL,
    scan_id BIGINT UNSIGNED NOT NULL,
    risk_fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    policy_version VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    state ENUM('pending', 'allow', 'review', 'block', 'unknown') NOT NULL,
    reason_category VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    decision_metadata_json JSON NOT NULL,
    valid_until DATETIME(6) NULL,
    decided_at DATETIME(6) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_destination_risk_decision_scan (scan_id),
    KEY idx_destination_risk_decision_authority (link_id, risk_fingerprint, policy_version, decided_at, id),
    KEY idx_destination_risk_decision_workspace (workspace_id, decided_at, id),
    CONSTRAINT fk_destination_risk_decision_link FOREIGN KEY (link_id) REFERENCES links(id) ON DELETE RESTRICT,
    CONSTRAINT fk_destination_risk_decision_scan FOREIGN KEY (scan_id) REFERENCES destination_risk_scans(id) ON DELETE RESTRICT,
    CONSTRAINT chk_destination_risk_reason_nonempty CHECK (CHAR_LENGTH(TRIM(reason_category)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE destination_risk_audit_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) NOT NULL,
    link_id BIGINT UNSIGNED NULL,
    scan_id BIGINT UNSIGNED NULL,
    actor_id VARCHAR(128) NOT NULL,
    action VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    result ENUM('success', 'denied', 'conflict', 'failed') NOT NULL,
    reason VARCHAR(500) NULL,
    correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    metadata_json JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_destination_risk_audit_workspace (workspace_id, created_at, id),
    KEY idx_destination_risk_audit_link (link_id, created_at, id),
    KEY idx_destination_risk_audit_scan (scan_id, created_at, id),
    KEY idx_destination_risk_audit_correlation (correlation_id),
    CONSTRAINT fk_destination_risk_audit_link FOREIGN KEY (link_id) REFERENCES links(id) ON DELETE RESTRICT,
    CONSTRAINT fk_destination_risk_audit_scan FOREIGN KEY (scan_id) REFERENCES destination_risk_scans(id) ON DELETE RESTRICT,
    CONSTRAINT chk_destination_risk_audit_actor_nonempty CHECK (CHAR_LENGTH(TRIM(actor_id)) > 0),
    CONSTRAINT chk_destination_risk_audit_action_nonempty CHECK (CHAR_LENGTH(TRIM(action)) > 0),
    CONSTRAINT chk_destination_risk_audit_correlation_nonempty CHECK (CHAR_LENGTH(TRIM(correlation_id)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
