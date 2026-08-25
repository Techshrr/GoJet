-- GoJet V10 / P15 Authentication, OAuth and Account
-- Repository-global immutable migration: 000016
-- MySQL 8.x
-- Adds runtime grant-key identity and the P15 verification template to the inherited P14 mail authority.
-- Raw verification material is never persisted by this migration.

ALTER TABLE auth_one_time_grants
    ADD COLUMN token_key_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER token_hash,
    ADD KEY idx_auth_one_time_grant_key (token_key_id, purpose, expires_at, id),
    ADD CONSTRAINT chk_auth_one_time_grant_key CHECK (
        token_key_id IS NULL OR CHAR_LENGTH(TRIM(token_key_id)) > 0
    );

INSERT INTO mail_templates
(template_key,locale,version,subject_template,text_template,html_template,variable_allowlist_json,internal_only,enabled)
VALUES
(
  'auth-email-verification','en',1,
  'Verify your GoJet email',
  'Use this verification code to finish creating your GoJet account: {{verification_code}}\n\nThe code expires shortly. If you did not request this account, ignore this message.',
  '<p>Use this verification code to finish creating your GoJet account:</p><p><strong>{{verification_code}}</strong></p><p>The code expires shortly. If you did not request this account, ignore this message.</p>',
  JSON_ARRAY('verification_code'),0,1
)
ON DUPLICATE KEY UPDATE template_key=template_key;
