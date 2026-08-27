-- GoJet V10 / P16 Trust, Destination Risk and Abuse
-- Repository-global immutable migration: 000023
-- MySQL 8.x
--
-- Public abuse intake stores only server-resolved resource authority and
-- redacted reporter-provided context. Raw Turnstile tokens, remote addresses,
-- provider evidence and arbitrary client workspace identifiers are prohibited.

CREATE TABLE abuse_reports (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id VARCHAR(40) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    workspace_id VARCHAR(64) NOT NULL,
    resource_type ENUM('short-link-risk', 'custom-domain-risk') NOT NULL,
    resource_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    hostname_ascii VARCHAR(253) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    safe_code VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL,
    destination_fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    category ENUM('phishing', 'malware', 'spam', 'scam', 'impersonation', 'other') NOT NULL,
    details_redacted VARCHAR(1000) NOT NULL DEFAULT '',
    request_fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    idempotency_key_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status ENUM('open', 'investigating', 'resolved', 'dismissed') NOT NULL DEFAULT 'open',
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    evidence_ref VARCHAR(160) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_abuse_reports_public_id (public_id),
    UNIQUE KEY uq_abuse_reports_idempotency (idempotency_key_hash),
    KEY idx_abuse_reports_workspace_status (workspace_id, status, created_at, id),
    KEY idx_abuse_reports_resource (workspace_id, resource_type, resource_id, created_at, id),
    KEY idx_abuse_reports_correlation (correlation_id),
    CONSTRAINT chk_abuse_reports_version CHECK (version >= 1),
    CONSTRAINT chk_abuse_short_code CHECK (
        (resource_type = 'short-link-risk' AND safe_code IS NOT NULL AND CHAR_LENGTH(safe_code) > 0 AND destination_fingerprint IS NOT NULL)
        OR (resource_type = 'custom-domain-risk' AND safe_code IS NULL AND destination_fingerprint IS NULL)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE abuse_report_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    report_id BIGINT UNSIGNED NOT NULL,
    workspace_id VARCHAR(64) NOT NULL,
    actor_id VARCHAR(128) NOT NULL,
    action VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    from_status ENUM('open', 'investigating', 'resolved', 'dismissed') NULL,
    to_status ENUM('open', 'investigating', 'resolved', 'dismissed') NULL,
    result ENUM('success', 'denied', 'conflict', 'failed') NOT NULL,
    reason_category VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    idempotency_key_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    request_fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    metadata_json JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_abuse_events_report_created (report_id, created_at, id),
    KEY idx_abuse_events_workspace_created (workspace_id, created_at, id),
    KEY idx_abuse_events_correlation (correlation_id),
    CONSTRAINT fk_abuse_events_report FOREIGN KEY (report_id) REFERENCES abuse_reports(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
