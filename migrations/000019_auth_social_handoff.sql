-- GoJet V10 / P15 Authentication, OAuth and Account
-- Repository-global immutable migration: 000019
-- MySQL 8.x
-- Extends the pre-frozen OAuth handoff table for short-lived callback/social
-- continuation authority. Raw provider subjects, browser handoff codes and
-- social email verification codes remain outside durable plaintext storage.

ALTER TABLE oauth_handoffs
    ADD COLUMN handoff_kind ENUM('callback','social_registration') NOT NULL DEFAULT 'callback' AFTER provider,
    ADD COLUMN provider_subject_hash BINARY(32) NULL AFTER oauth_identity_id,
    ADD COLUMN display_name VARCHAR(255) NOT NULL DEFAULT '' AFTER provider_email_verified,
    ADD KEY idx_oauth_handoff_kind_expiry (handoff_kind, expires_at, consumed_at, id),
    DROP CHECK chk_oauth_handoff_target,
    ADD CONSTRAINT chk_oauth_handoff_target CHECK (
        user_id IS NOT NULL
        OR oauth_identity_id IS NOT NULL
        OR email_normalized IS NOT NULL
        OR provider_subject_hash IS NOT NULL
    ),
    ADD CONSTRAINT chk_oauth_handoff_subject CHECK (
        provider_subject_hash IS NULL OR OCTET_LENGTH(provider_subject_hash) = 32
    );

ALTER TABLE auth_one_time_grants
    ADD COLUMN oauth_handoff_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER user_id,
    ADD KEY idx_auth_one_time_grant_handoff (oauth_handoff_id, purpose, expires_at, consumed_at, id),
    ADD CONSTRAINT fk_auth_one_time_grant_handoff FOREIGN KEY (oauth_handoff_id) REFERENCES oauth_handoffs(id) ON DELETE RESTRICT,
    ADD CONSTRAINT chk_auth_one_time_grant_social_handoff CHECK (
        (purpose = 'social_email_verification' AND oauth_handoff_id IS NOT NULL)
        OR (purpose <> 'social_email_verification' AND oauth_handoff_id IS NULL)
    );

INSERT INTO mail_templates
(template_key,locale,version,subject_template,text_template,html_template,variable_allowlist_json,internal_only,enabled)
VALUES
(
  'auth-social-email-verification','en',1,
  'Verify your email for GoJet social registration',
  'Use this verification code to finish creating your GoJet account: {{verification_code}}\n\nThe code expires shortly. If you did not request this account, ignore this message.',
  '<p>Use this verification code to finish creating your GoJet account:</p><p><strong>{{verification_code}}</strong></p><p>The code expires shortly. If you did not request this account, ignore this message.</p>',
  JSON_ARRAY('verification_code'),0,1
)
ON DUPLICATE KEY UPDATE template_key=template_key;
