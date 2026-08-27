-- GoJet V10 / P16 Trust, Destination Risk and Abuse
-- Repository-global immutable migration: 000021
-- MySQL 8.x

CREATE TABLE destination_risk_overrides (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    workspace_id VARCHAR(64) NOT NULL,
    link_id BIGINT UNSIGNED NOT NULL,
    risk_fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    policy_version VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    base_decision_id BIGINT UNSIGNED NOT NULL,
    base_decision_state ENUM('allow', 'review', 'block', 'unknown') NOT NULL,
    decision ENUM('allow', 'review', 'block') NOT NULL,
    reason VARCHAR(500) NOT NULL,
    policy_context_json JSON NOT NULL,
    actor_id VARCHAR(128) NOT NULL,
    correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    invalidated_at DATETIME(6) NULL,
    invalidated_by VARCHAR(128) NULL,
    invalidation_reason VARCHAR(500) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_destination_risk_override_correlation (workspace_id, correlation_id),
    KEY idx_destination_risk_override_authority (link_id, risk_fingerprint, policy_version, invalidated_at, expires_at, created_at, id),
    KEY idx_destination_risk_override_base_decision (base_decision_id, id),
    KEY idx_destination_risk_override_workspace (workspace_id, created_at, id),
    CONSTRAINT fk_destination_risk_override_link FOREIGN KEY (link_id) REFERENCES links(id) ON DELETE RESTRICT,
    CONSTRAINT fk_destination_risk_override_base_decision FOREIGN KEY (base_decision_id) REFERENCES destination_risk_decisions(id) ON DELETE RESTRICT,
    CONSTRAINT chk_destination_risk_override_reason_nonempty CHECK (CHAR_LENGTH(TRIM(reason)) > 0),
    CONSTRAINT chk_destination_risk_override_actor_nonempty CHECK (CHAR_LENGTH(TRIM(actor_id)) > 0),
    CONSTRAINT chk_destination_risk_override_correlation_nonempty CHECK (CHAR_LENGTH(TRIM(correlation_id)) > 0),
    CONSTRAINT chk_destination_risk_override_policy_nonempty CHECK (CHAR_LENGTH(TRIM(policy_version)) > 0),
    CONSTRAINT chk_destination_risk_override_invalidation_pair CHECK (
        (invalidated_at IS NULL AND invalidated_by IS NULL AND invalidation_reason IS NULL)
        OR
        (invalidated_at IS NOT NULL AND CHAR_LENGTH(TRIM(invalidated_by)) > 0 AND CHAR_LENGTH(TRIM(invalidation_reason)) > 0)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
