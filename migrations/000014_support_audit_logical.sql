-- GoJet V10 / P14 Support Tickets and Mail
-- Repository-global immutable migration: 000014
-- MySQL 8.x
-- Logical audit identity is correlation/action/resource/result. This keeps
-- post-commit repair idempotent without adding raw idempotency material.

ALTER TABLE support_audit_events
    ADD UNIQUE KEY uq_support_audit_logical
        (request_correlation_id, action, resource_type, resource_id, result);
