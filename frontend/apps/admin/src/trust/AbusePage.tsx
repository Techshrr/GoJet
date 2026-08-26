import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useParams } from '@tanstack/react-router';
import { Button, Card, DataTable, EmptyState, InlineMessage, useShellViewport } from '@gojet/ui';
import { AdminShell } from '../shell/AdminShell';
import { TrustNav, TrustState } from './TrustNav';
import {
  compactFingerprint,
  formatTimestamp,
  trustErrorMessage,
  trustGet,
  trustShellState,
  trustWrite,
  type AbuseReportRecord,
} from './runtime';

type AbuseListResponse = { items: AbuseReportRecord[]; csrf_token: string };
type AbuseDetailResponse = { report: AbuseReportRecord; csrf_token?: string };
type AbuseAction = 'investigate' | 'resolve' | 'dismiss' | 'block' | 'suspend' | 'restore';

type ActionCopy = { title: string; impact: string };
const actionCopy: Record<AbuseAction, ActionCopy> = {
  investigate: { title: 'Begin investigation', impact: 'Moves the report into investigating state. It does not change resource traffic by itself.' },
  resolve: { title: 'Resolve report', impact: 'Closes the report as resolved. Active resource holds must be handled separately before closure when recovery is intended.' },
  dismiss: { title: 'Dismiss report', impact: 'Closes the report as dismissed. This records an accountable review outcome and does not silently remove an active resource hold.' },
  block: { title: 'Block short-link resource', impact: 'Creates an exact-fingerprint abuse hold and projects an immediate fail-closed block for this short link.' },
  suspend: { title: 'Suspend custom domain', impact: 'Applies the inherited P06 security suspension immediately with zero grace and records the P16 abuse hold.' },
  restore: { title: 'Restore resource', impact: 'Releases the active abuse hold only when current independent safety authority allows recovery. Restore never manufactures allow authority.' },
};

export default function AbuseListPage() {
  const viewport = useShellViewport();
  const query = useQuery({
    queryKey: ['p16-admin-abuse'],
    queryFn: () => trustGet<AbuseListResponse>('/api/admin/abuse?limit=100'),
    retry: false,
  });
  const items = query.data?.items ?? [];
  const pageState = query.isPending ? 'loading' : query.isError ? 'error' : items.length === 0 ? 'empty' : items.some((item) => item.status === 'open') ? 'open' : items.some((item) => item.status === 'investigating') ? 'investigating' : items.some((item) => item.status === 'resolved') ? 'resolved' : 'dismissed';

  return (
    <AdminShell state={trustShellState(query.error)}>
      <section className="trust-page" data-page="admin-abuse" data-state={pageState}>
        <header>
          <p className="trust-eyebrow">TRUST &amp; SAFETY / ABUSE</p>
          <h1>Abuse reports</h1>
          <p>Review sanitized public reports and take server-authorized lifecycle or resource actions. Reporter secrets, raw request fingerprints and provider evidence stay server-side.</p>
        </header>
        <TrustNav />
        <p className="trust-safe-note">A report is evidence for review, not automatic proof. Resource actions remain permission-bound, current-authority checked and independently audited.</p>
        {query.isPending ? <p role="status">Loading abuse reports…</p> : null}
        {query.isError ? <InlineMessage variant="danger">{trustErrorMessage(query.error)}</InlineMessage> : null}
        {!query.isPending && !query.isError && items.length === 0 ? <EmptyState title="No abuse reports" reason="There are no durable abuse reports available to this administrator." /> : null}
        {items.length > 0 && viewport !== 'mobile' ? (
          <DataTable caption="Abuse reports">
            <thead><tr><th scope="col">Report</th><th scope="col">Resource</th><th scope="col">Category</th><th scope="col">Status</th><th scope="col">Hold</th><th scope="col">Updated</th></tr></thead>
            <tbody>{items.map((item) => (
              <tr key={item.id}>
                <td><a href={`/admin/trust/abuse/${item.id}`}>{item.public_id}</a></td>
                <td>{item.resource_type}<br />{item.hostname}{item.safe_code ? ` / ${item.safe_code}` : ''}</td>
                <td>{item.category}</td>
                <td><TrustState value={item.status} /></td>
                <td>{item.active_hold ? `${item.active_hold.state} · ${item.active_hold.reason_category}` : 'None'}</td>
                <td>{formatTimestamp(item.updated_at)}</td>
              </tr>
            ))}</tbody>
          </DataTable>
        ) : null}
        {items.length > 0 && viewport === 'mobile' ? <div className="trust-card-list">{items.map((item) => (
          <Card as="article" key={item.id}>
            <header><strong>{item.public_id}</strong><TrustState value={item.status} /></header>
            <span>{item.resource_type} · {item.hostname}</span>
            <span>{item.category}{item.active_hold ? ` · hold ${item.active_hold.state}` : ''}</span>
            <a href={`/admin/trust/abuse/${item.id}`}>Open abuse detail</a>
          </Card>
        ))}</div> : null}
      </section>
    </AdminShell>
  );
}

export function AbuseDetailPage() {
  const { reportId } = useParams({ from: '/admin/trust/abuse/$reportId' });
  const queryClient = useQueryClient();
  const [action, setAction] = useState<AbuseAction | null>(null);
  const [reason, setReason] = useState('');
  const [validation, setValidation] = useState('');
  const [result, setResult] = useState('');

  const detail = useQuery({
    queryKey: ['p16-admin-abuse-detail', reportId],
    queryFn: () => trustGet<AbuseDetailResponse>(`/api/admin/abuse/${encodeURIComponent(reportId)}`),
    retry: false,
  });
  const authority = useQuery({
    queryKey: ['p16-admin-abuse', 'csrf'],
    queryFn: () => trustGet<AbuseListResponse>('/api/admin/abuse?limit=1'),
    retry: false,
  });
  const item = detail.data?.report;
  const csrf = detail.data?.csrf_token ?? authority.data?.csrf_token ?? '';

  const mutation = useMutation({
    mutationFn: () => {
      if (!item || !action) throw new Error('No current abuse action is selected.');
      const cleanReason = reason.trim();
      if (!csrf) throw new Error('Mutation authority is unavailable. Reload current administrator authority before retrying.');
      if (!cleanReason || cleanReason.length > 500) throw new Error('Enter an accountable action reason up to 500 characters.');
      const resourceAction = action === 'block' || action === 'suspend' || action === 'restore';
      return trustWrite<{ report: AbuseReportRecord; action: AbuseAction; changed: boolean }>(`/api/admin/abuse/${encodeURIComponent(reportId)}/actions`, csrf, {
        action,
        expected_version: item.version,
        exact_fingerprint: resourceAction && item.resource_type === 'short-link-risk' ? item.destination_fingerprint ?? '' : '',
        reason: cleanReason,
      });
    },
    onSuccess: async (data) => {
      setValidation(''); setAction(null); setReason('');
      setResult(`${actionCopy[data.action].title}: ${data.changed ? 'server authority changed and was audited' : 'the existing idempotent result was returned'}. Current report state: ${data.report.status}.`);
      await queryClient.invalidateQueries({ queryKey: ['p16-admin-abuse-detail'] });
      await queryClient.invalidateQueries({ queryKey: ['p16-admin-abuse'] });
    },
    onError: (error) => setValidation(trustErrorMessage(error)),
  });

  const lifecycleActions: AbuseAction[] = item?.status === 'open' ? ['investigate', 'dismiss'] : item?.status === 'investigating' ? ['resolve', 'dismiss'] : [];
  const resourceActions: AbuseAction[] = item?.status !== 'investigating' ? [] : item.active_hold ? ['restore'] : item.resource_type === 'short-link-risk' ? ['block'] : ['suspend'];
  const error = detail.error ?? authority.error;
  const pageState = detail.isPending ? 'loading' : detail.isError ? 'error' : action ? 'destructive-confirm' : item?.status ?? 'error';

  return (
    <AdminShell state={trustShellState(error)}>
      <section className="trust-page" data-page="admin-abuse-detail" data-state={pageState}>
        <header>
          <p className="trust-eyebrow">TRUST &amp; SAFETY / ABUSE</p>
          <h1>Abuse report detail</h1>
          <p>Only sanitized evidence is rendered. Lifecycle decisions and resource holds are separate operations with separate effects.</p>
        </header>
        <TrustNav />
        {detail.isPending ? <p role="status">Loading abuse report…</p> : null}
        {detail.isError ? <InlineMessage variant="danger">{trustErrorMessage(detail.error)}</InlineMessage> : null}
        {authority.isError ? <InlineMessage variant="danger">Mutation authority could not be loaded. Read-only detail remains available.</InlineMessage> : null}
        {result ? <InlineMessage variant="success">{result}</InlineMessage> : null}
        {validation ? <InlineMessage variant="danger">{validation}</InlineMessage> : null}
        {item ? <>
          <Card as="section" aria-labelledby="abuse-report-title">
            <header className="trust-action-row"><div><h2 id="abuse-report-title">{item.public_id}</h2><p>{item.category}</p></div><TrustState value={item.status} /></header>
            <dl className="trust-kv">
              <div><dt>Workspace</dt><dd>{item.workspace_id}</dd></div>
              <div><dt>Resource</dt><dd>{item.resource_type} · #{item.resource_id}</dd></div>
              <div><dt>Hostname</dt><dd>{item.hostname}</dd></div>
              <div><dt>Safe code</dt><dd>{item.safe_code || 'Not applicable'}</dd></div>
              <div><dt>Version</dt><dd>{item.version}</dd></div>
              <div><dt>Created</dt><dd>{formatTimestamp(item.created_at)}</dd></div>
              <div><dt>Updated</dt><dd>{formatTimestamp(item.updated_at)}</dd></div>
              {item.destination_fingerprint ? <div><dt>Exact fingerprint</dt><dd className="trust-mono" title={item.destination_fingerprint}>{compactFingerprint(item.destination_fingerprint)}</dd></div> : null}
              <div><dt>Active hold</dt><dd>{item.active_hold ? `${item.active_hold.state} · ${item.active_hold.reason_category} · #${item.active_hold.id}` : 'None'}</dd></div>
            </dl>
            <div className="trust-safe-note"><strong>Sanitized report detail</strong><p>{item.details || 'No additional sanitized detail was supplied.'}</p></div>
          </Card>
          <Card as="section" className="trust-actions" aria-labelledby="abuse-actions-title">
            <div><h2 id="abuse-actions-title">Accountable actions</h2><p className="trust-confirm-note">Visible actions reflect current report state. The server still re-checks permission, version, fingerprint and independent safety authority.</p></div>
            <div className="trust-action-row">
              {lifecycleActions.map((value) => <Button key={value} variant={value === 'dismiss' ? 'ghost' : 'secondary'} onClick={() => { setValidation(''); setAction(value); }}>{actionCopy[value].title}</Button>)}
              {resourceActions.map((value) => <Button key={value} variant="ghost" onClick={() => { setValidation(''); setAction(value); }}>{actionCopy[value].title}</Button>)}
              {lifecycleActions.length === 0 && resourceActions.length === 0 ? <span>No further action is available from this report state.</span> : null}
            </div>
            {action ? <div className="trust-confirm" data-confirm={action}>
              <h3>{actionCopy[action].title}</h3>
              <p className="trust-confirm-note">Impact: {actionCopy[action].impact}</p>
              <label>Accountable reason<textarea rows={4} maxLength={500} value={reason} onChange={(event) => setReason(event.currentTarget.value)} /></label>
              <div className="trust-action-row"><Button disabled={mutation.isPending} onClick={() => mutation.mutate()}>{mutation.isPending ? 'Applying…' : `Confirm ${action}`}</Button><Button variant="ghost" onClick={() => setAction(null)}>Cancel</Button></div>
            </div> : null}
          </Card>
        </> : null}
        <a href="/admin/trust/abuse">Back to abuse reports</a>
      </section>
    </AdminShell>
  );
}
