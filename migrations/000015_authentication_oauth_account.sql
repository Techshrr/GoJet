-- GoJet V10 / P15 Authentication, OAuth and Account
-- Repository-global immutable migration: 000015
-- MySQL 8.x
-- Raw passwords, verification/reset/login codes, session tokens, OAuth state values,
-- OAuth handoff codes, provider authorization codes/access tokens and provider client
-- secrets are intentionally not persisted in plaintext.

CREATE TABLE auth_users (
    id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    email VARCHAR(320) NOT NULL,
    email_normalized VARCHAR(320) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs NOT NULL,
    display_name VARCHAR(255) NOT NULL DEFAULT '',
    status ENUM('pending_verification','active','locked','disabled') NOT NULL DEFAULT 'pending_verification',
    email_verified_at DATETIME(6) NULL,
    password_changed_at DATETIME(6) NULL,
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_auth_users_email_normalized (email_normalized),
    KEY idx_auth_users_status (status, updated_at, id),
    CONSTRAINT chk_auth_users_id CHECK (CHAR_LENGTH(TRIM(id)) > 0),
    CONSTRAINT chk_auth_users_email CHECK (CHAR_LENGTH(TRIM(email)) > 0),
    CONSTRAINT chk_auth_users_email_normalized CHECK (CHAR_LENGTH(TRIM(email_normalized)) > 0),
    CONSTRAINT chk_auth_users_version CHECK (version >= 1),
    CONSTRAINT chk_auth_users_verification_state CHECK (
        (status = 'pending_verification' AND email_verified_at IS NULL)
        OR status <> 'pending_verification'
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE auth_credentials (
    user_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    password_hash VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    password_algorithm VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    password_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    failed_attempts INT UNSIGNED NOT NULL DEFAULT 0,
    locked_until DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (user_id),
    CONSTRAINT fk_auth_credentials_user FOREIGN KEY (user_id) REFERENCES auth_users(id) ON DELETE RESTRICT,
    CONSTRAINT chk_auth_credentials_hash CHECK (CHAR_LENGTH(password_hash) >= 20),
    CONSTRAINT chk_auth_credentials_algorithm CHECK (CHAR_LENGTH(TRIM(password_algorithm)) > 0),
    CONSTRAINT chk_auth_credentials_version CHECK (password_version >= 1),
    CONSTRAINT chk_auth_credentials_failed_attempts CHECK (failed_attempts <= 1000000)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE auth_one_time_grants (
    id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    purpose ENUM('email_verification','login_email_code','password_reset','social_email_verification') NOT NULL,
    user_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL,
    email_normalized VARCHAR(320) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs NULL,
    token_hash BINARY(32) NOT NULL,
    attempt_count INT UNSIGNED NOT NULL DEFAULT 0,
    max_attempts INT UNSIGNED NOT NULL DEFAULT 8,
    expires_at DATETIME(6) NOT NULL,
    consumed_at DATETIME(6) NULL,
    invalidated_at DATETIME(6) NULL,
    correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_auth_one_time_grant_token (token_hash),
    KEY idx_auth_one_time_grant_user (user_id, purpose, expires_at, consumed_at, id),
    KEY idx_auth_one_time_grant_email (email_normalized, purpose, expires_at, consumed_at, id),
    KEY idx_auth_one_time_grant_correlation (correlation_id),
    CONSTRAINT fk_auth_one_time_grant_user FOREIGN KEY (user_id) REFERENCES auth_users(id) ON DELETE RESTRICT,
    CONSTRAINT chk_auth_one_time_grant_target CHECK (user_id IS NOT NULL OR email_normalized IS NOT NULL),
    CONSTRAINT chk_auth_one_time_grant_attempts CHECK (attempt_count <= max_attempts AND max_attempts BETWEEN 1 AND 32),
    CONSTRAINT chk_auth_one_time_grant_expiry CHECK (expires_at > created_at),
    CONSTRAINT chk_auth_one_time_grant_terminal CHECK (consumed_at IS NULL OR invalidated_at IS NULL),
    CONSTRAINT chk_auth_one_time_grant_correlation CHECK (CHAR_LENGTH(TRIM(correlation_id)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE auth_sessions (
    id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    user_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    token_hash BINARY(32) NOT NULL,
    csrf_secret_hash BINARY(32) NOT NULL,
    status ENUM('active','revoked','expired') NOT NULL DEFAULT 'active',
    expires_at DATETIME(6) NOT NULL,
    revoked_at DATETIME(6) NULL,
    last_seen_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    user_agent_hash BINARY(32) NULL,
    ip_prefix_hash BINARY(32) NULL,
    correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_auth_session_token (token_hash),
    KEY idx_auth_sessions_user (user_id, status, expires_at, id),
    KEY idx_auth_sessions_expiry (status, expires_at, id),
    KEY idx_auth_sessions_correlation (correlation_id),
    CONSTRAINT fk_auth_sessions_user FOREIGN KEY (user_id) REFERENCES auth_users(id) ON DELETE RESTRICT,
    CONSTRAINT chk_auth_sessions_expiry CHECK (expires_at > created_at),
    CONSTRAINT chk_auth_sessions_revoked CHECK (
        (status = 'revoked' AND revoked_at IS NOT NULL)
        OR (status <> 'revoked' AND revoked_at IS NULL)
    ),
    CONSTRAINT chk_auth_sessions_correlation CHECK (CHAR_LENGTH(TRIM(correlation_id)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE oauth_identities (
    id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    user_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    provider ENUM('google','facebook','github','qq','wechat','rainbow') NOT NULL,
    provider_subject_hash BINARY(32) NOT NULL,
    provider_email_normalized VARCHAR(320) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs NULL,
    provider_email_verified TINYINT(1) NOT NULL DEFAULT 0,
    display_name VARCHAR(255) NOT NULL DEFAULT '',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_oauth_identity_provider_subject (provider, provider_subject_hash),
    UNIQUE KEY uq_oauth_identity_user_provider (user_id, provider),
    KEY idx_oauth_identity_user (user_id, updated_at, id),
    CONSTRAINT fk_oauth_identity_user FOREIGN KEY (user_id) REFERENCES auth_users(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE oauth_states (
    id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    provider ENUM('google','facebook','github','qq','wechat','rainbow') NOT NULL,
    state_hash BINARY(32) NOT NULL,
    initiating_user_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL,
    initiating_session_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    intent ENUM('login','register','bind') NOT NULL,
    redirect_path VARCHAR(500) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '/',
    pkce_verifier_ciphertext VARBINARY(512) NULL,
    pkce_key_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    expires_at DATETIME(6) NOT NULL,
    consumed_at DATETIME(6) NULL,
    correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_oauth_state_hash (state_hash),
    KEY idx_oauth_state_expiry (provider, expires_at, consumed_at, id),
    KEY idx_oauth_state_user (initiating_user_id, created_at, id),
    KEY idx_oauth_state_correlation (correlation_id),
    CONSTRAINT fk_oauth_state_user FOREIGN KEY (initiating_user_id) REFERENCES auth_users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_oauth_state_session FOREIGN KEY (initiating_session_id) REFERENCES auth_sessions(id) ON DELETE RESTRICT,
    CONSTRAINT chk_oauth_state_expiry CHECK (expires_at > created_at),
    CONSTRAINT chk_oauth_state_pkce CHECK (
        (pkce_verifier_ciphertext IS NULL AND pkce_key_id IS NULL)
        OR (pkce_verifier_ciphertext IS NOT NULL AND pkce_key_id IS NOT NULL)
    ),
    CONSTRAINT chk_oauth_state_correlation CHECK (CHAR_LENGTH(TRIM(correlation_id)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE oauth_handoffs (
    id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    provider ENUM('google','facebook','github','qq','wechat','rainbow') NOT NULL,
    code_hash BINARY(32) NOT NULL,
    intent ENUM('login','register','bind') NOT NULL,
    user_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL,
    oauth_identity_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    email_normalized VARCHAR(320) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs NULL,
    provider_email_verified TINYINT(1) NOT NULL DEFAULT 0,
    expires_at DATETIME(6) NOT NULL,
    consumed_at DATETIME(6) NULL,
    correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_oauth_handoff_code (code_hash),
    KEY idx_oauth_handoff_expiry (provider, expires_at, consumed_at, id),
    KEY idx_oauth_handoff_user (user_id, created_at, id),
    KEY idx_oauth_handoff_correlation (correlation_id),
    CONSTRAINT fk_oauth_handoff_user FOREIGN KEY (user_id) REFERENCES auth_users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_oauth_handoff_identity FOREIGN KEY (oauth_identity_id) REFERENCES oauth_identities(id) ON DELETE RESTRICT,
    CONSTRAINT chk_oauth_handoff_expiry CHECK (expires_at > created_at),
    CONSTRAINT chk_oauth_handoff_target CHECK (
        user_id IS NOT NULL OR oauth_identity_id IS NOT NULL OR email_normalized IS NOT NULL
    ),
    CONSTRAINT chk_oauth_handoff_correlation CHECK (CHAR_LENGTH(TRIM(correlation_id)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE oauth_provider_configs (
    provider ENUM('google','facebook','github','qq','wechat','rainbow') NOT NULL,
    enabled TINYINT(1) NOT NULL DEFAULT 0,
    client_id VARCHAR(255) NOT NULL DEFAULT '',
    client_secret_ciphertext VARBINARY(1024) NULL,
    secret_key_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    authorization_url VARCHAR(1000) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    token_url VARCHAR(1000) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    userinfo_url VARCHAR(1000) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    redirect_uri VARCHAR(1000) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    scopes_json JSON NOT NULL,
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    updated_by VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'system',
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (provider),
    CONSTRAINT chk_oauth_provider_secret_pair CHECK (
        (client_secret_ciphertext IS NULL AND secret_key_id IS NULL)
        OR (client_secret_ciphertext IS NOT NULL AND secret_key_id IS NOT NULL)
    ),
    CONSTRAINT chk_oauth_provider_scopes CHECK (JSON_TYPE(scopes_json) = 'ARRAY'),
    CONSTRAINT chk_oauth_provider_version CHECK (version >= 1),
    CONSTRAINT chk_oauth_provider_updated_by CHECK (CHAR_LENGTH(TRIM(updated_by)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE auth_audit_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    actor_kind ENUM('public','user','system','admin') NOT NULL,
    actor_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    user_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL,
    action VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    resource_type VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    resource_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    result ENUM('success','denied','conflict','failed') NOT NULL,
    request_correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    metadata_json JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_auth_audit_user (user_id, created_at, id),
    KEY idx_auth_audit_actor (actor_kind, actor_id, created_at, id),
    KEY idx_auth_audit_action (action, created_at, id),
    KEY idx_auth_audit_correlation (request_correlation_id),
    CONSTRAINT fk_auth_audit_user FOREIGN KEY (user_id) REFERENCES auth_users(id) ON DELETE RESTRICT,
    CONSTRAINT chk_auth_audit_action CHECK (CHAR_LENGTH(TRIM(action)) > 0),
    CONSTRAINT chk_auth_audit_resource CHECK (CHAR_LENGTH(TRIM(resource_type)) > 0),
    CONSTRAINT chk_auth_audit_correlation CHECK (CHAR_LENGTH(TRIM(request_correlation_id)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
