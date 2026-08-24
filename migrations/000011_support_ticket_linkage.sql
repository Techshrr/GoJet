-- GoJet V10 / P14 Support Tickets and Mail
-- Repository-global immutable migration: 000011
-- MySQL 8.x
-- Strengthen ticket/message attachment linkage without rewriting published 000010.

ALTER TABLE support_ticket_messages
    ADD UNIQUE KEY uq_support_ticket_message_ticket_id (ticket_id, id);

ALTER TABLE support_ticket_attachments
    ADD KEY idx_support_ticket_attachment_ticket_message (ticket_id, message_id),
    ADD CONSTRAINT fk_support_ticket_attachment_ticket_message
        FOREIGN KEY (ticket_id, message_id)
        REFERENCES support_ticket_messages(ticket_id, id)
        ON DELETE RESTRICT;
