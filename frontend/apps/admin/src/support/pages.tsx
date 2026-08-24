import { useMemo, useState } from 'react';
import { Link, useParams } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Card, InlineMessage } from '@gojet/ui';
import { AdminShell } from '../shell/AdminShell';
import {
  AdminSupportRequestError, adminSupportGet, adminSupportWrite, readAdminSupportRuntime,
  type AdminMailQueueItem, type AdminMailSettings, type AdminMailTemplate,
  type AdminTicket, type AdminTicketMessage,
} from './runtime';

function ticketState(status: AdminTicket['status'] | undefined): 'open' | 'awaiting' | 'closed' {
  if (status === 'closed') return 'closed';
  if (status === 'awaiting_user' || status === 'awaiting_support') return 'awaiting';
  return 'open';
}
function errorMessage(error: unknown, fallback: string) {
  if (error instanceof AdminSupportRequestError && error.status === 403) return 'Your administrator permission does not cover this area.';
  if (error instanceof AdminSupportRequestError && error.status === 503) return 'Administrator permission authority is unavailable.';
  return fallback;
}

export function AdminTicketsPage() {
  const runtime = useMemo(() => readAdminSupportRuntime(), []);
  const query = useQuery({
    queryKey: ['p14-admin-tickets'], enabled: runtime !== null, retry: false,
    queryFn: () => adminSupportGet<{ items: AdminTicket[] }>(runtime!, '/api/admin/support/tickets'),
  });
  const items = query.data?.items ?? [];
  const state = !runtime || query.isError ? 'error' : query.isPending ? 'loading' : items.length === 0 ? 'empty' : ticketState(items[0]?.status);
  return <AdminShell state={!runtime ? 'admin-auth-required' : query.isError ? 'permission-denied' : 'normal'}><section className="admin-support-page" data-page="admin-tickets" data-state={state}>
    <header className="admin-support-header"><div><p className="admin-support-eyebrow">SUPPORT / TICKETS</p><h1>Support tickets</h1><p>Ticket state is operational support authority only. It never grants domain entitlement or billing authority.</p></div><Link to="/admin/mail">Mail operations</Link></header>
    {query.isPending && runtime ? <p role="status">Loading support tickets…</p> : null}
    {!runtime || query.isError ? <InlineMessage variant="danger">{errorMessage(query.error, 'Support tickets are unavailable.')}</InlineMessage> : null}
    {!query.isPending && !query.isError && runtime && items.length === 0 ? <Card as="section"><h2>No tickets</h2><p>No support tickets are currently queued for administrator action.</p></Card> : null}
    <div className="admin-support-list">{items.map((ticket) => <Card as="article" key={ticket.id}><div className="admin-support-row"><div><strong>{ticket.subject}</strong><p>{ticket.category}</p></div><span>{ticket.status}</span></div><Link to="/admin/tickets/$ticketId" params={{ ticketId: ticket.id }}>Open ticket</Link></Card>)}</div>
  </section></AdminShell>;
}

export function AdminTicketDetailPage() {
  const runtime = useMemo(() => readAdminSupportRuntime(), []);
  const { ticketId } = useParams({ from: '/admin/tickets/$ticketId' });
  const client = useQueryClient();
  const [kind, setKind] = useState<'support_reply' | 'internal_note'>('support_reply');
  const [message, setMessage] = useState('');
  const [attachmentName, setAttachmentName] = useState('');
  const query = useQuery({
    queryKey: ['p14-admin-ticket', ticketId], enabled: runtime !== null, retry: false,
    queryFn: () => adminSupportGet<{ ticket: AdminTicket; messages: AdminTicketMessage[] }>(runtime!, `/api/admin/support/tickets/${encodeURIComponent(ticketId)}`),
  });
  const reply = useMutation({
    mutationFn: () => adminSupportWrite(runtime!, `/api/admin/support/tickets/${encodeURIComponent(ticketId)}/replies`, 'POST', { kind, message }, true),
    onSuccess: async () => { setMessage(''); await client.invalidateQueries({ queryKey: ['p14-admin-ticket', ticketId] }); await client.invalidateQueries({ queryKey: ['p14-admin-tickets'] }); },
  });
  const close = useMutation({
    mutationFn: () => adminSupportWrite(runtime!, `/api/admin/support/tickets/${encodeURIComponent(ticketId)}`, 'PATCH', { action: 'close', expected_version: query.data!.ticket.version }),
    onSuccess: async () => { await client.invalidateQueries({ queryKey: ['p14-admin-ticket', ticketId] }); await client.invalidateQueries({ queryKey: ['p14-admin-tickets'] }); },
  });
  const ticket = query.data?.ticket;
  const state = !runtime || query.isError || reply.isError || close.isError ? 'error' : query.isPending ? 'loading' : attachmentName ? 'attachment-blocked' : reply.isPending ? 'replying' : ticketState(ticket?.status);
  const submitReply = (event: React.FormEvent) => { event.preventDefault(); if (message.trim() && !attachmentName) reply.mutate(); };
  return <AdminShell state={!runtime ? 'admin-auth-required' : query.isError ? 'permission-denied' : 'normal'}><section className="admin-support-page" data-page="admin-tickets" data-state={state}>
    <header className="admin-support-header"><div><p className="admin-support-eyebrow">SUPPORT / TICKET</p><h1>{ticket?.subject ?? 'Ticket detail'}</h1><p>Internal notes remain administrator-only and never enter requester mail or requester APIs.</p></div><Link to="/admin/tickets">Back to tickets</Link></header>
    {query.isPending && runtime ? <p role="status">Loading ticket…</p> : null}
    {!runtime || query.isError ? <InlineMessage variant="danger">{errorMessage(query.error, 'Ticket detail is unavailable.')}</InlineMessage> : null}
    {reply.isError || close.isError ? <InlineMessage variant="danger">Support could not complete this administrator action.</InlineMessage> : null}
    {ticket ? <Card as="section"><dl className="admin-support-kv"><div><dt>Status</dt><dd>{ticket.status}</dd></div><div><dt>Category</dt><dd>{ticket.category}</dd></div><div><dt>Version</dt><dd>{ticket.version}</dd></div><div><dt>Correlation</dt><dd>{ticket.correlation_id}</dd></div></dl></Card> : null}
    {query.data ? <section className="admin-support-thread" aria-label="Ticket messages">{query.data.messages.map((item) => <Card as="article" key={item.id} data-kind={item.kind}><header><strong>{item.kind === 'internal_note' ? 'Internal note' : item.actor_type === 'support' ? 'Support' : 'Requester'}</strong><span>{new Date(item.created_at).toLocaleString()}</span></header><p>{item.body}</p></Card>)}</section> : null}
    {ticket && ticket.status !== 'closed' ? <form className="admin-support-form" onSubmit={submitReply}><label>Reply type<select aria-label="Reply type" value={kind} onChange={(event) => setKind(event.currentTarget.value as 'support_reply' | 'internal_note')}><option value="support_reply">Support reply</option><option value="internal_note">Internal note</option></select></label><label>Admin reply<textarea aria-label="Admin reply" value={message} onChange={(event) => setMessage(event.currentTarget.value)} rows={5}/></label><label>Admin attachment<input aria-label="Admin attachment" type="file" onChange={(event) => setAttachmentName(event.currentTarget.files?.[0]?.name ?? '')}/></label>{attachmentName ? <InlineMessage variant="warning">Attachment remains blocked until the inherited P09 scanner can prove a current clean verdict. No file bytes were sent.</InlineMessage> : null}<div className="admin-support-actions"><Button type="submit" disabled={!message.trim() || !!attachmentName || reply.isPending}>{reply.isPending ? 'Sending reply…' : 'Send reply'}</Button><Button type="button" variant="destructive" disabled={close.isPending} onClick={() => close.mutate()}>Close ticket</Button></div></form> : null}
  </section></AdminShell>;
}

export function AdminMailPage() {
  const runtime = useMemo(() => readAdminSupportRuntime(), []);
  const client = useQueryClient();
  const [recipient, setRecipient] = useState('');
  const queue = useQuery({ queryKey: ['p14-admin-mail-queue'], enabled: runtime !== null, retry: false, queryFn: () => adminSupportGet<{ items: AdminMailQueueItem[] }>(runtime!, '/api/admin/mail/queue') });
  const templates = useQuery({ queryKey: ['p14-admin-mail-templates'], enabled: runtime !== null, retry: false, queryFn: () => adminSupportGet<{ items: AdminMailTemplate[] }>(runtime!, '/api/admin/mail/templates') });
  const settings = useQuery({ queryKey: ['p14-admin-mail-settings'], enabled: runtime !== null, retry: false, queryFn: () => adminSupportGet<{ settings: AdminMailSettings; credentials_masked: boolean; credential_source: string }>(runtime!, '/api/admin/mail/settings') });
  const [enabledOverride, setEnabledOverride] = useState<boolean | null>(null);
  const saveSettings = useMutation({
    mutationFn: () => adminSupportWrite(runtime!, '/api/admin/mail/settings', 'PATCH', { expected_version: settings.data!.settings.version, enabled: enabledOverride ?? settings.data!.settings.enabled }),
    onSuccess: async () => { setEnabledOverride(null); await client.invalidateQueries({ queryKey: ['p14-admin-mail-settings'] }); },
  });
  const testSend = useMutation({
    mutationFn: () => adminSupportWrite(runtime!, '/api/admin/mail/test', 'POST', { recipient }, true),
    onSuccess: async () => { setRecipient(''); await client.invalidateQueries({ queryKey: ['p14-admin-mail-queue'] }); },
  });
  const queries = [queue, templates, settings];
  const errorCount = queries.filter((item) => item.isError).length;
  const pending = queries.some((item) => item.isPending);
  const items = queue.data?.items ?? [];
  const state = !runtime || errorCount === 3 || saveSettings.isError || testSend.isError ? 'error' : pending ? 'loading' : errorCount > 0 ? 'partial' : items.length === 0 ? 'empty' : items[0]!.status;
  const checked = enabledOverride ?? settings.data?.settings.enabled ?? false;
  return <AdminShell state={!runtime ? 'admin-auth-required' : errorCount === 3 ? 'permission-denied' : errorCount > 0 ? 'partial-service-degradation' : 'normal'}><section className="admin-support-page" data-page="admin-mail" data-state={state}>
    <header className="admin-support-header"><div><p className="admin-support-eyebrow">OPERATIONS / MAIL</p><h1>Mail operations</h1><p>Queue state is communication evidence only. Credentials remain runtime-managed and masked.</p></div><Link to="/admin/tickets">Support tickets</Link></header>
    {pending && runtime ? <p role="status">Loading mail operations…</p> : null}
    {!runtime || errorCount === 3 ? <InlineMessage variant="danger">{errorMessage(queue.error ?? templates.error ?? settings.error, 'Mail operations are unavailable.')}</InlineMessage> : null}
    {errorCount > 0 && errorCount < 3 ? <InlineMessage variant="warning">Some mail operational data is unavailable. Safe prior context remains visible.</InlineMessage> : null}
    {saveSettings.isError || testSend.isError ? <InlineMessage variant="danger">Mail operation could not be confirmed by the server.</InlineMessage> : null}
    {settings.data ? <Card as="section"><h2>Dispatch settings</h2><label className="admin-support-check"><input type="checkbox" aria-label="Mail delivery enabled" checked={checked} onChange={(event) => setEnabledOverride(event.currentTarget.checked)}/>Mail delivery enabled</label><p>Settings version {settings.data.settings.version}. Credentials: {settings.data.credentials_masked ? 'masked' : 'unavailable'} ({settings.data.credential_source}).</p><Button type="button" disabled={saveSettings.isPending || enabledOverride === null} onClick={() => saveSettings.mutate()}>Save settings</Button></Card> : null}
    <Card as="section"><h2>Safe test send</h2><label>Test recipient<input type="email" aria-label="Test recipient" value={recipient} onChange={(event) => setRecipient(event.currentTarget.value)}/></label><Button type="button" disabled={!recipient || testSend.isPending} onClick={() => testSend.mutate()}>{testSend.isPending ? 'Queueing…' : 'Queue test message'}</Button><p>Recipient values and SMTP credentials are not exposed in the queue view.</p></Card>
    {templates.data ? <Card as="section"><h2>Templates</h2><ul className="admin-support-templates">{templates.data.items.map((item) => <li key={`${item.key}:${item.locale}:${item.version}`}><strong>{item.key}</strong> · {item.locale} · v{item.version} · {item.enabled ? 'enabled' : 'disabled'}<span>{item.variable_allowlist.join(', ')}</span></li>)}</ul></Card> : null}
    {!pending && !queue.isError && items.length === 0 ? <Card as="section"><h2>Queue empty</h2><p>No outbound mail jobs are currently visible.</p></Card> : null}
    <div className="admin-support-list">{items.map((item) => <Card as="article" key={item.id}><div className="admin-support-row"><div><strong>{item.template_key}</strong><p>{item.resource_type} · {item.resource_id}</p></div><span>{item.status}</span></div><p>Attempts: {item.attempt_count}{item.last_error_code ? ` · ${item.last_error_code}` : ''}</p></Card>)}</div>
  </section></AdminShell>;
}
