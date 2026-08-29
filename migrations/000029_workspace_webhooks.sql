-- GoJet V10 / P17 Workspace outbound-webhook governance
-- Repository-global immutable migration: 000029
-- MySQL 8.x

CREATE TABLE workspace_webhooks (
    id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    workspace_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    name VARCHAR(128) NOT NULL,
    endpoint_url VARCHAR(2048) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    events_json JSON NOT NULL,
    secret_ciphertext VARBINARY(512) NOT NULL,
    secret_key_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    secret_prefix VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status ENUM('active', 'disabled') NOT NULL DEFAULT 'active',
    created_by VARCHAR(128) NOT NULL,
    updated_by VARCHAR(128) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    rotated_at DATETIME(6) NULL,
    disabled_at DATETIME(6) NULL,
    PRIMARY KEY (id),
    KEY idx_workspace_webhooks_workspace (workspace_id, status, created_at, id),
    CONSTRAINT fk_workspace_webhooks_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE RESTRICT,
    CONSTRAINT chk_workspace_webhooks_name_nonempty CHECK (CHAR_LENGTH(TRIM(name)) > 0),
    CONSTRAINT chk_workspace_webhooks_endpoint_nonempty CHECK (CHAR_LENGTH(TRIM(endpoint_url)) > 0),
    CONSTRAINT chk_workspace_webhooks_events_array CHECK (JSON_TYPE(events_json) = 'ARRAY')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE workspace_webhook_deliveries (
    id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    workspace_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    webhook_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    event_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    event_type VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    body MEDIUMBLOB NOT NULL,
    body_sha256 BINARY(32) NOT NULL,
    status ENUM('retrying', 'delivered', 'failed') NOT NULL DEFAULT 'retrying',
    attempts INT UNSIGNED NOT NULL DEFAULT 0,
    next_attempt_at DATETIME(6) NOT NULL,
    last_attempt_at DATETIME(6) NULL,
    last_status_code SMALLINT UNSIGNED NULL,
    last_error_code VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    request_correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    delivered_at DATETIME(6) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_workspace_webhook_event (webhook_id, event_id),
    KEY idx_workspace_webhook_due (status, next_attempt_at, id),
    KEY idx_workspace_webhook_delivery_workspace (workspace_id, webhook_id, created_at, id),
    CONSTRAINT fk_workspace_webhook_deliveries_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE RESTRICT,
    CONSTRAINT fk_workspace_webhook_deliveries_webhook FOREIGN KEY (webhook_id) REFERENCES workspace_webhooks(id) ON DELETE RESTRICT,
    CONSTRAINT chk_workspace_webhook_attempts CHECK (attempts <= 5),
    CONSTRAINT chk_workspace_webhook_event_nonempty CHECK (CHAR_LENGTH(TRIM(event_id)) > 0 AND CHAR_LENGTH(TRIM(event_type)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
