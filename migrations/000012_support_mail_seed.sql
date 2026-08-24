-- GoJet V10 / P14 Support Tickets and Mail
-- Repository-global immutable migration: 000012
-- MySQL 8.x
-- Seed only versioned, allowlisted P14 mail templates. Secrets/tokens/provider evidence are not template variables.

INSERT INTO mail_templates
(template_key,locale,version,subject_template,text_template,html_template,variable_allowlist_json,internal_only,enabled)
VALUES
(
  'support-ticket-created','en',1,
  'We received support ticket {{ticket_id}}',
  'Hello {{display_name}},\n\nWe received your support request "{{subject}}". Current status: {{status}}.',
  '<p>Hello {{display_name}},</p><p>We received your support request “{{subject}}”.</p><p>Current status: {{status}}.</p>',
  JSON_ARRAY('ticket_id','display_name','subject','status'),0,1
),
(
  'support-ticket-reply','en',1,
  'Update for support ticket {{ticket_id}}',
  'Hello {{display_name}},\n\nThere is an update on "{{subject}}":\n\n{{message_body}}',
  '<p>Hello {{display_name}},</p><p>There is an update on “{{subject}}”:</p><p>{{message_body}}</p>',
  JSON_ARRAY('ticket_id','display_name','subject','message_body'),0,1
),
(
  'public-contact-received','en',1,
  'We received your message',
  'Hello {{contact_name}},\n\nWe received your message about "{{contact_subject}}".',
  '<p>Hello {{contact_name}},</p><p>We received your message about “{{contact_subject}}”.</p>',
  JSON_ARRAY('contact_name','contact_subject'),0,1
)
ON DUPLICATE KEY UPDATE template_key=template_key;
