-- GoJet V10 / P17 Workspace API-key governance
-- Repository-global immutable migration: 000028
-- MySQL 8.x

CREATE TABLE workspace_api_keys (
    id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    workspace_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    name VARCHAR(128) NOT NULL,
    secret_hash BINARY(32) NOT NULL,
    secret_prefix VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    scopes_json JSON NOT NULL,
    status ENUM('active', 'revoked') NOT NULL DEFAULT 'active',
    expires_at DATETIME(6) NULL,
    rate_limit_per_minute INT UNSIGNED NOT NULL DEFAULT 60,
    created_by VARCHAR(128) NOT NULL,
    revoked_by VARCHAR(128) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    rotated_at DATETIME(6) NULL,
    revoked_at DATETIME(6) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_workspace_api_keys_hash (secret_hash),
    KEY idx_workspace_api_keys_workspace (workspace_id, status, created_at, id),
    KEY idx_workspace_api_keys_expiry (workspace_id, expires_at, id),
    CONSTRAINT fk_workspace_api_keys_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE RESTRICT,
    CONSTRAINT chk_workspace_api_keys_name_nonempty CHECK (CHAR_LENGTH(TRIM(name)) > 0),
    CONSTRAINT chk_workspace_api_keys_scopes_array CHECK (JSON_TYPE(scopes_json) = 'ARRAY'),
    CONSTRAINT chk_workspace_api_keys_rate CHECK (rate_limit_per_minute BETWEEN 1 AND 10000)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
