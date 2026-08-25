-- GoJet V10 / P15 Authentication, OAuth and Account
-- Repository-global immutable migration: 000017
-- MySQL 8.x
-- Adds P15 login-email-code and password-reset templates to inherited P14 mail authority.
-- Raw code/token material remains runtime-only and is never persisted by this migration.

INSERT INTO mail_templates
(template_key,locale,version,subject_template,text_template,html_template,variable_allowlist_json,internal_only,enabled)
VALUES
(
  'auth-login-email-code','en',1,
  'Your GoJet login code',
  'Use this one-time code to sign in to GoJet: {{login_code}}\n\nThe code expires shortly. If you did not request it, ignore this message.',
  '<p>Use this one-time code to sign in to GoJet:</p><p><strong>{{login_code}}</strong></p><p>The code expires shortly. If you did not request it, ignore this message.</p>',
  JSON_ARRAY('login_code'),0,1
),
(
  'auth-password-reset','en',1,
  'Reset your GoJet password',
  'Use this one-time reset token to reset your GoJet password: {{reset_token}}\n\nThe token expires shortly. If you did not request it, ignore this message.',
  '<p>Use this one-time reset token to reset your GoJet password:</p><p><strong>{{reset_token}}</strong></p><p>The token expires shortly. If you did not request it, ignore this message.</p>',
  JSON_ARRAY('reset_token'),0,1
)
ON DUPLICATE KEY UPDATE template_key=template_key;
