-- GoJet V10 / P16 Trust, Destination Risk and Abuse
-- Repository-global immutable migration: 000024
-- MySQL 8.x
--
-- Adds administrator lifecycle idempotency and durable P16 risk-resource holds.
-- Holds are control-plane safety state only; they do not replace P05 link state
-- or collapse P06 entitlement/ownership/ingress-DNS/HTTPS/risk axes.

ALTER TABLE abuse_report_events
    ADD UNIQUE KEY uq_abuse_events_idempotency (report_id, action, idempotency_key_hash);

CREATE TABLE abuse_resource_holds (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    report_id BIGINT UNSIGNED NOT NULL,
    workspace_id VARCHAR(64) NOT NULL,
    resource_type ENUM('short-link-risk', 'custom-domain-risk') NOT NULL,
    resource_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    exact_fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    state ENUM('active', 'released') NOT NULL DEFAULT 'active',
    reason_category VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    actor_id VARCHAR(128) NOT NULL,
    correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    idempotency_key_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    request_fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    released_at DATETIME(6) NULL,
    released_by VARCHAR(128) NULL,
    release_reason VARCHAR(500) NULL,
    release_correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL,
    active_marker TINYINT GENERATED ALWAYS AS (CASE WHEN state = 'active' THEN 1 ELSE NULL END) STORED,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_abuse_hold_idempotency (report_id, idempotency_key_hash),
    UNIQUE KEY uq_abuse_hold_active_resource (workspace_id, resource_type, resource_id, active_marker),
    KEY idx_abuse_hold_report_created (report_id, created_at, id),
    KEY idx_abuse_hold_workspace_state (workspace_id, state, created_at, id),
    KEY idx_abuse_hold_correlation (correlation_id),
    CONSTRAINT fk_abuse_hold_report FOREIGN KEY (report_id) REFERENCES abuse_reports(id) ON DELETE RESTRICT,
    CONSTRAINT chk_abuse_hold_resource_authority CHECK (
        (resource_type = 'short-link-risk' AND exact_fingerprint IS NOT NULL)
        OR (resource_type = 'custom-domain-risk' AND exact_fingerprint IS NULL)
    ),
    CONSTRAINT chk_abuse_hold_release_state CHECK (
        (state = 'active' AND released_at IS NULL AND released_by IS NULL AND release_reason IS NULL AND release_correlation_id IS NULL)
        OR (state = 'released' AND released_at IS NOT NULL AND released_by IS NOT NULL AND release_reason IS NOT NULL AND release_correlation_id IS NOT NULL)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
