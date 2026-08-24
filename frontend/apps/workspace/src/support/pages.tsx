import { useMemo, useState } from 'react';
import { Link, useParams } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { GoJetApiError } from '@gojet/api-client';
import type { SupportTicketStatus } from '@gojet/api-client';
import { Button, Card, EmptyState, InlineMessage } from '@gojet/ui';
import { WorkspaceShell } from '../shell/WorkspaceShell';
import { createSupportClient, readSupportRuntime } from './runtime';

function statusLabel(status: SupportTicketStatus): string {
  switch (status) {
    case 'awaiting_user': return 'Awaiting your reply';
    case 'awaiting_support': return 'Awaiting support';
    case 'closed': return 'Closed';
    default: return 'Open';
  }
}

function supportError(error: unknown): { variant: 'warning' | 'danger'; text: string; state: string } {
  if (error instanceof GoJetApiError) {
    if (error.code === 'rate_limited') return { variant: 'warning', text: 'Too many support submissions. Try again after the server rate-limit window.', state: 'rate-limited' };
    if (error.code === 'turnstile_rejected') return { variant: 'warning', text: 'Verification was rejected. No ticket or mail was created.', state: 'Turnstile-error' };
    if (error.status === 404) return { variant: 'danger', text: 'This ticket is unavailable for the current Workspace identity.', state: 'forbidden' };
  }
  return { variant: 'danger', text: 'Support could not complete this request. Existing server-confirmed context remains visible.', state: 'error' };
}

export function SupportListPage() {
  const runtime = useMemo(() => readSupportRuntime(), []);
  const client = useMemo(() => runtime ? createSupportClient(runtime) : null, [runtime]);
  const query = useQuery({
    queryKey: ['p14-support-tickets', runtime?.workspaceId, runtime?.actorId],
    enabled: Boolean(runtime && client),
    queryFn: () => client!.list(runtime!.workspaceId),
    retry: false,
  });
  const items = query.data?.items ?? [];
  const state = !runtime ? 'error' : query.isPending ? 'loading' : query.isError ? 'error' : items.length === 0 ? 'empty' : items.some((item) => item.status === 'awaiting_user') ? 'awaiting-user' : items.some((item) => item.status === 'awaiting_support') ? 'awaiting-support' : items.every((item) => item.status === 'closed') ? 'closed' : 'open';

  return (
    <WorkspaceShell sectionLabel="Support">
      <section className="support-page" data-page="support-list" data-state={state}>
        <header className="support-page-header">
          <div><p className="support-eyebrow">SUPPORT</p><h1>Support tickets</h1><p>Tickets are scoped to your current Workspace identity. Navigation never replaces server authorization.</p></div>
          <Link to="/app/support/new" className="support-primary-link">New ticket</Link>
        </header>
        {!runtime ? <InlineMessage variant="danger">Authoritative Workspace identity is unavailable. Support data is not loaded from local state.</InlineMessage> : null}
        {query.isPending && runtime ? <p role="status">Loading support tickets…</p> : null}
        {query.isError ? <InlineMessage variant="danger">Support tickets could not be loaded. No stale local list is presented as current.</InlineMessage> : null}
        {query.isSuccess && items.length === 0 ? <EmptyState title="No support tickets" reason="Create a ticket when you need help. Access requests remain requests only and never grant domain entitlement." /> : null}
        {items.length > 0 ? <div className="support-ticket-list" role="list" aria-label="Support tickets">{items.map((ticket) => (
          <Card as="article" key={ticket.id} className="support-ticket-card" data-status={ticket.status}>
            <div className="support-ticket-card__top"><div><p>{ticket.category}</p><h2><Link to="/app/support/$ticketId" params={{ ticketId: ticket.id }}>{ticket.subject}</Link></h2></div><strong>{statusLabel(ticket.status)}</strong></div>
            <p>Updated {new Date(ticket.updated_at).toLocaleString()}</p>
          </Card>
        ))}</div> : null}
      </section>
    </WorkspaceShell>
  );
}

export function SupportNewPage() {
  const runtime = useMemo(() => readSupportRuntime(), []);
  const client = useMemo(() => runtime ? createSupportClient(runtime) : null, [runtime]);
  const queryClient = useQueryClient();
  const requestedCategory = useMemo(() => new URLSearchParams(window.location.search).get('category') ?? '', []);
  const [category, setCategory] = useState(requestedCategory === 'custom-domain-access' ? 'custom-domain-access' : 'general');
  const [subject, setSubject] = useState('');
  const [message, setMessage] = useState('');
  const [attachmentName, setAttachmentName] = useState('');
  const mutation = useMutation({
    networkMode: 'always',
    mutationFn: async () => {
      if (!runtime || !client) throw new Error('Support runtime unavailable');
      if (!runtime.turnstileToken) throw new GoJetApiError(400, 'turnstile_rejected', 'Verification is required.');
      return client.create({ workspace_id: runtime.workspaceId, category, subject: subject.trim(), message: message.trim(), turnstile_token: runtime.turnstileToken });
    },
    onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: ['p14-support-tickets', runtime?.workspaceId, runtime?.actorId] }); },
  });
  const error = mutation.isError ? supportError(mutation.error) : null;
  const blockedAttachment = attachmentName !== '';
  const canSubmit = Boolean(runtime && client && subject.trim() && message.trim() && runtime.turnstileToken && !blockedAttachment && !mutation.isPending);
  const state = mutation.isSuccess ? 'success' : error?.state ?? (mutation.isPending ? 'submitting' : blockedAttachment ? 'attachment' : runtime?.turnstileToken ? 'input' : 'Turnstile-required');

  return (
    <WorkspaceShell sectionLabel="New support ticket">
      <section className="support-page" data-page="support-new" data-state={state}>
        <header className="support-page-header"><div><p className="support-eyebrow">SUPPORT</p><h1>New ticket</h1><p>Ticket creation is protected by server-side verification, rate limiting and idempotency.</p></div><Link to="/app/support">Back to support</Link></header>
        {!runtime ? <InlineMessage variant="danger">Authoritative Workspace identity is unavailable. Submission is disabled.</InlineMessage> : null}
        {category === 'custom-domain-access' ? <InlineMessage variant="info">This creates a support request only. It cannot grant custom-domain entitlement, ownership, DNS, HTTPS or risk authority.</InlineMessage> : null}
        {!runtime?.turnstileToken ? <InlineMessage variant="warning">Verification is required before this ticket can be submitted.</InlineMessage> : null}
        {blockedAttachment ? <InlineMessage variant="warning">Attachment “{attachmentName}” is held locally and has not been uploaded. P14 does not invent an unapproved attachment HTTP route; submission remains blocked until bytes can enter the inherited P09 quarantine/scan authority.</InlineMessage> : null}
        {error ? <InlineMessage variant={error.variant}>{error.text}</InlineMessage> : null}
        {mutation.isSuccess ? <InlineMessage variant="success">Ticket created successfully. <Link to="/app/support/$ticketId" params={{ ticketId: mutation.data.ticket.id }}>Open ticket</Link></InlineMessage> : null}
        <form className="support-form" onSubmit={(event) => { event.preventDefault(); if (canSubmit) mutation.mutate(); }}>
          <Card as="section" className="support-form-card">
            <label>Category<select aria-label="Support category" value={category} onChange={(event) => setCategory(event.currentTarget.value)} disabled={mutation.isPending}><option value="general">General support</option><option value="custom-domain-access">Custom domain access request</option></select></label>
            <label>Subject<input aria-label="Support subject" value={subject} onChange={(event) => setSubject(event.currentTarget.value)} maxLength={300} required disabled={mutation.isPending} /></label>
            <label>Message<textarea aria-label="Support message" value={message} onChange={(event) => setMessage(event.currentTarget.value)} rows={8} required disabled={mutation.isPending} /></label>
            <label>Attachment<input aria-label="Support attachment" type="file" accept=".txt,text/plain" onChange={(event) => setAttachmentName(event.currentTarget.files?.[0]?.name ?? '')} disabled={mutation.isPending} /></label>
            {blockedAttachment ? <Button type="button" variant="ghost" onClick={() => setAttachmentName('')}>Remove attachment selection</Button> : null}
            <Button type="submit" disabled={!canSubmit}>{mutation.isPending ? 'Submitting…' : 'Submit ticket'}</Button>
          </Card>
        </form>
      </section>
    </WorkspaceShell>
  );
}

export function SupportThreadPage() {
  const { ticketId } = useParams({ from: '/app/support/$ticketId' });
  const runtime = useMemo(() => readSupportRuntime(), []);
  const client = useMemo(() => runtime ? createSupportClient(runtime) : null, [runtime]);
  const queryClient = useQueryClient();
  const [reply, setReply] = useState('');
  const [attachmentName, setAttachmentName] = useState('');
  const query = useQuery({
    queryKey: ['p14-support-ticket', ticketId, runtime?.actorId],
    enabled: Boolean(runtime && client && ticketId),
    queryFn: () => client!.get(ticketId),
    retry: false,
  });
  const replyMutation = useMutation({
    networkMode: 'always',
    mutationFn: () => client!.reply(ticketId, reply.trim()),
    onSuccess: async () => {
      setReply('');
      await queryClient.invalidateQueries({ queryKey: ['p14-support-ticket', ticketId, runtime?.actorId] });
      await queryClient.invalidateQueries({ queryKey: ['p14-support-tickets', runtime?.workspaceId, runtime?.actorId] });
    },
  });
  const closeMutation = useMutation({
    mutationFn: () => client!.close(ticketId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['p14-support-ticket', ticketId, runtime?.actorId] });
      await queryClient.invalidateQueries({ queryKey: ['p14-support-tickets', runtime?.workspaceId, runtime?.actorId] });
    },
  });
  const detail = query.data;
  const ticket = detail?.ticket;
  const queryError = query.isError ? supportError(query.error) : null;
  const mutationError = replyMutation.isError ? supportError(replyMutation.error) : closeMutation.isError ? supportError(closeMutation.error) : null;
  const blockedAttachment = attachmentName !== '';
  const state = !runtime ? 'error' : query.isPending ? 'loading' : queryError?.state ?? (blockedAttachment ? 'attachment-blocked' : replyMutation.isPending ? 'replying' : ticket?.status === 'closed' ? 'closed' : ticket?.status === 'awaiting_user' || ticket?.status === 'awaiting_support' ? 'awaiting' : 'open');

  return (
    <WorkspaceShell sectionLabel="Support ticket">
      <section className="support-page" data-page="support-thread" data-state={state}>
        <header className="support-page-header"><div><p className="support-eyebrow">SUPPORT THREAD</p><h1>{ticket?.subject ?? 'Support ticket'}</h1>{ticket ? <p><strong>{statusLabel(ticket.status)}</strong> · {ticket.category}</p> : <p>Ticket access is re-authorized on every direct load.</p>}</div><Link to="/app/support">Back to support</Link></header>
        {!runtime ? <InlineMessage variant="danger">Authoritative Workspace identity is unavailable.</InlineMessage> : null}
        {query.isPending && runtime ? <p role="status">Loading support thread…</p> : null}
        {queryError ? <InlineMessage variant="danger">{queryError.text}</InlineMessage> : null}
        {mutationError ? <InlineMessage variant={mutationError.variant}>{mutationError.text}</InlineMessage> : null}
        {blockedAttachment ? <InlineMessage variant="warning">Attachment “{attachmentName}” remains local and blocked. No file bytes were sent or represented as clean.</InlineMessage> : null}
        {detail ? <div className="support-thread" aria-label="Ticket messages">{detail.messages.map((item) => (
          <Card as="article" key={item.id} className="support-message" data-kind={item.kind}><header><strong>{item.actor_type === 'support' ? 'Support' : 'You'}</strong><time dateTime={item.created_at}>{new Date(item.created_at).toLocaleString()}</time></header><p>{item.body}</p></Card>
        ))}</div> : null}
        {ticket && ticket.status !== 'closed' ? (
          <form className="support-form" onSubmit={(event) => { event.preventDefault(); if (reply.trim() && !blockedAttachment && !replyMutation.isPending) replyMutation.mutate(); }}>
            <Card as="section" className="support-form-card">
              <label>Reply<textarea aria-label="Ticket reply" value={reply} onChange={(event) => setReply(event.currentTarget.value)} rows={6} required disabled={replyMutation.isPending || closeMutation.isPending} /></label>
              <label>Attachment<input aria-label="Reply attachment" type="file" accept=".txt,text/plain" onChange={(event) => setAttachmentName(event.currentTarget.files?.[0]?.name ?? '')} disabled={replyMutation.isPending || closeMutation.isPending} /></label>
              {blockedAttachment ? <Button type="button" variant="ghost" onClick={() => setAttachmentName('')}>Remove attachment selection</Button> : null}
              <div className="support-actions"><Button type="submit" disabled={!reply.trim() || blockedAttachment || replyMutation.isPending}>{replyMutation.isPending ? 'Sending…' : 'Send reply'}</Button><Button type="button" variant="ghost" disabled={closeMutation.isPending} onClick={() => closeMutation.mutate()}>{closeMutation.isPending ? 'Closing…' : 'Close ticket'}</Button></div>
            </Card>
          </form>
        ) : null}
        {ticket?.status === 'closed' ? <InlineMessage variant="info">This ticket is closed. Closing changes ticket state only and grants no other authority.</InlineMessage> : null}
      </section>
    </WorkspaceShell>
  );
}
