-- GoJet V10 / P17 Admin, Permissions and Audit
-- Repository-global immutable migration: 000025
-- MySQL 8.x
-- Administrator identity/session/RBAC authority is deliberately separate from P15 customer auth_users/auth_sessions.
-- Raw passwords, admin session/CSRF tokens and TOTP secrets are never persisted in plaintext.

CREATE TABLE admin_administrators (
    id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    email VARCHAR(320) NOT NULL,
    email_normalized VARCHAR(320) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs NOT NULL,
    display_name VARCHAR(255) NOT NULL DEFAULT '',
    status ENUM('active','suspended') NOT NULL DEFAULT 'active',
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_admin_email_normalized (email_normalized),
    KEY idx_admin_status (status, updated_at, id),
    CONSTRAINT chk_admin_id CHECK (CHAR_LENGTH(TRIM(id)) > 0),
    CONSTRAINT chk_admin_email CHECK (CHAR_LENGTH(TRIM(email_normalized)) > 0),
    CONSTRAINT chk_admin_version CHECK (version >= 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE admin_credentials (
    administrator_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    password_hash VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    password_algorithm VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    password_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    failed_attempts INT UNSIGNED NOT NULL DEFAULT 0,
    locked_until DATETIME(6) NULL,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (administrator_id),
    CONSTRAINT fk_admin_credential_administrator FOREIGN KEY (administrator_id) REFERENCES admin_administrators(id) ON DELETE RESTRICT,
    CONSTRAINT chk_admin_credential_hash CHECK (CHAR_LENGTH(password_hash) >= 20),
    CONSTRAINT chk_admin_credential_algorithm CHECK (password_algorithm = 'pbkdf2-sha256'),
    CONSTRAINT chk_admin_credential_attempts CHECK (failed_attempts <= 1000000)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE admin_totp_credentials (
    administrator_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    secret_ciphertext VARBINARY(512) NOT NULL,
    secret_key_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    state ENUM('pending','active') NOT NULL DEFAULT 'pending',
    enrolled_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (administrator_id),
    CONSTRAINT fk_admin_totp_administrator FOREIGN KEY (administrator_id) REFERENCES admin_administrators(id) ON DELETE RESTRICT,
    CONSTRAINT chk_admin_totp_key CHECK (CHAR_LENGTH(TRIM(secret_key_id)) > 0),
    CONSTRAINT chk_admin_totp_state CHECK ((state='pending' AND enrolled_at IS NULL) OR (state='active' AND enrolled_at IS NOT NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE admin_permissions (
    permission VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    PRIMARY KEY (permission),
    CONSTRAINT chk_admin_permission CHECK (CHAR_LENGTH(TRIM(permission)) > 0 AND permission NOT LIKE '%*%')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO admin_permissions(permission) VALUES
('platform.read'),('admins.manage'),('users.manage'),('workspaces.manage'),('links.manage'),('domains.manage'),
('domains.risk.manage'),('domains.entitlements.manage'),('security.manage'),('files.manage'),('tickets.manage'),
('operations.manage'),('billing.manage'),('mail.manage'),('settings.manage'),('content.manage');

CREATE TABLE admin_roles (
    id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    name VARCHAR(128) NOT NULL,
    normalized_name VARCHAR(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs NOT NULL,
    description VARCHAR(500) NOT NULL DEFAULT '',
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_by VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_admin_role_name (normalized_name),
    CONSTRAINT chk_admin_role_id CHECK (CHAR_LENGTH(TRIM(id)) > 0),
    CONSTRAINT chk_admin_role_name CHECK (CHAR_LENGTH(TRIM(normalized_name)) > 0),
    CONSTRAINT chk_admin_role_version CHECK (version >= 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE admin_role_permissions (
    role_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    permission VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (role_id, permission),
    CONSTRAINT fk_admin_role_permission_role FOREIGN KEY (role_id) REFERENCES admin_roles(id) ON DELETE RESTRICT,
    CONSTRAINT fk_admin_role_permission_catalog FOREIGN KEY (permission) REFERENCES admin_permissions(permission) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE admin_role_assignments (
    administrator_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    role_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    assigned_by VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (administrator_id, role_id),
    CONSTRAINT fk_admin_assignment_administrator FOREIGN KEY (administrator_id) REFERENCES admin_administrators(id) ON DELETE RESTRICT,
    CONSTRAINT fk_admin_assignment_role FOREIGN KEY (role_id) REFERENCES admin_roles(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE admin_sessions (
    id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    administrator_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    token_hash BINARY(32) NOT NULL,
    csrf_hash BINARY(32) NOT NULL,
    status ENUM('active','revoked','expired') NOT NULL DEFAULT 'active',
    expires_at DATETIME(6) NOT NULL,
    mfa_verified_at DATETIME(6) NULL,
    last_seen_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    revoked_at DATETIME(6) NULL,
    correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_admin_session_token (token_hash),
    KEY idx_admin_sessions_administrator (administrator_id, status, expires_at, id),
    KEY idx_admin_sessions_expiry (status, expires_at, id),
    CONSTRAINT fk_admin_session_administrator FOREIGN KEY (administrator_id) REFERENCES admin_administrators(id) ON DELETE RESTRICT,
    CONSTRAINT chk_admin_session_expiry CHECK (expires_at > created_at),
    CONSTRAINT chk_admin_session_revoked CHECK ((status='revoked' AND revoked_at IS NOT NULL) OR (status<>'revoked' AND revoked_at IS NULL)),
    CONSTRAINT chk_admin_session_correlation CHECK (CHAR_LENGTH(TRIM(correlation_id)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE admin_audit_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    actor_kind ENUM('anonymous','administrator','system') NOT NULL,
    actor_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    action VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    resource_type VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    resource_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    result ENUM('success','denied','conflict','failed') NOT NULL,
    request_correlation_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    reason VARCHAR(500) NULL,
    before_json JSON NOT NULL,
    after_json JSON NOT NULL,
    metadata_json JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_admin_audit_actor (actor_kind, actor_id, created_at, id),
    KEY idx_admin_audit_action (action, created_at, id),
    KEY idx_admin_audit_resource (resource_type, resource_id, created_at, id),
    KEY idx_admin_audit_correlation (request_correlation_id),
    CONSTRAINT chk_admin_audit_action CHECK (CHAR_LENGTH(TRIM(action)) > 0),
    CONSTRAINT chk_admin_audit_resource CHECK (CHAR_LENGTH(TRIM(resource_type)) > 0),
    CONSTRAINT chk_admin_audit_correlation CHECK (CHAR_LENGTH(TRIM(request_correlation_id)) > 0),
    CONSTRAINT chk_admin_audit_reason CHECK (reason IS NULL OR CHAR_LENGTH(TRIM(reason)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE admin_idempotency_records (
    actor_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    action VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    idempotency_key_hash BINARY(32) NOT NULL,
    request_fingerprint BINARY(32) NOT NULL,
    response_json JSON NOT NULL,
    audit_event_id BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (actor_id, action, idempotency_key_hash),
    KEY idx_admin_idempotency_audit (audit_event_id),
    CONSTRAINT fk_admin_idempotency_audit FOREIGN KEY (audit_event_id) REFERENCES admin_audit_events(id) ON DELETE RESTRICT,
    CONSTRAINT chk_admin_idempotency_actor CHECK (CHAR_LENGTH(TRIM(actor_id)) > 0),
    CONSTRAINT chk_admin_idempotency_action CHECK (CHAR_LENGTH(TRIM(action)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TRIGGER trg_admin_audit_no_update BEFORE UPDATE ON admin_audit_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='admin_audit_events is append-only';
CREATE TRIGGER trg_admin_audit_no_delete BEFORE DELETE ON admin_audit_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='admin_audit_events is append-only';
