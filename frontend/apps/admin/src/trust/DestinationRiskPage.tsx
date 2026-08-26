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
  type DestinationRiskRecord,
} from './runtime';

type DestinationListResponse = { items: DestinationRiskRecord[]; csrf_token: string };
type DestinationDetailResponse = { risk: DestinationRiskRecord };
type ActionMode = 'rescan' | 'override' | null;

function destinationState(item?: DestinationRiskRecord): string {
  if (!item) return 'empty';
  const reason = item.reason_category.toLowerCase();
  if (reason.includes('stale') || reason.includes('fingerprint')) return 'stale-fingerprint';
  if (reason.includes('provider') && item.decision_state !== 'allow') return 'provider-partial';
  return item.decision_state;
}

function destinationListState(items: DestinationRiskRecord[]): string {
  const states = items.map((item) => destinationState(item));
  for (const candidate of ['provider-partial', 'stale-fingerprint', 'block', 'review', 'pending', 'unknown', 'allow']) {
    if (states.includes(candidate)) return candidate;
  }
  return 'empty';
}

export default function DestinationRiskListPage() {
  const viewport = useShellViewport();
  const query = useQuery({
    queryKey: ['p16-admin-destination-risks'],
    queryFn: () => trustGet<DestinationListResponse>('/api/admin/destination-risks?limit=100'),
    retry: false,
  });
  const items = query.data?.items ?? [];
  const state = query.isPending ? 'loading' : query.isError ? 'error' : items.length === 0 ? 'empty' : destinationListState(items);

  return (
    <AdminShell state={trustShellState(query.error)}>
      <section className="trust-page" data-page="admin-destination-risk" data-state={state}>
        <header>
          <p className="trust-eyebrow">TRUST &amp; SAFETY / DESTINATION RISK</p>
          <h1>Destination risk</h1>
          <p>Review exact-fingerprint destination authority. Reachable targets, provider evidence, thresholds and credentials stay server-side.</p>
        </header>
        <TrustNav />
        <p className="trust-safe-note">A list row is operational state, not an allow decision by itself. Missing, stale, pending, review, block and provider-unavailable authority remain fail closed.</p>
        {query.isPending ? <p role="status">Loading destination risk authority…</p> : null}
        {query.isError ? <InlineMessage variant="danger">{trustErrorMessage(query.error)}</InlineMessage> : null}
        {!query.isPending && !query.isError && items.length === 0 ? <EmptyState title="No destination risk records" reason="No durable destination-risk scans are available to this administrator." /> : null}
        {items.length > 0 && viewport !== 'mobile' ? (
          <DataTable caption="Destination risk authority">
            <thead><tr><th scope="col">Risk</th><th scope="col">Workspace / Link</th><th scope="col">Decision</th><th scope="col">Reason</th><th scope="col">Authority</th><th scope="col">Updated</th></tr></thead>
            <tbody>{items.map((item) => (
              <tr key={item.id}>
                <td><a href={`/admin/trust/destination-risk/${item.id}`}>#{item.id}</a></td>
                <td>{item.workspace_id}<br />Link #{item.link_id}</td>
                <td><TrustState value={destinationState(item)} /></td>
                <td>{item.reason_category}</td>
                <td>{item.target_count} target(s) · {item.provider_count} provider observation(s){item.has_active_override ? ' · override active' : ''}</td>
                <td>{formatTimestamp(item.updated_at)}</td>
              </tr>
            ))}</tbody>
          </DataTable>
        ) : null}
        {items.length > 0 && viewport === 'mobile' ? <div className="trust-card-list">{items.map((item) => (
          <Card as="article" key={item.id}>
            <header><strong>Risk #{item.id}</strong><TrustState value={destinationState(item)} /></header>
            <span>{item.workspace_id} · Link #{item.link_id}</span>
            <span>{item.reason_category}</span>
            <a href={`/admin/trust/destination-risk/${item.id}`}>Open risk detail</a>
          </Card>
        ))}</div> : null}
      </section>
    </AdminShell>
  );
}

export function DestinationRiskDetailPage() {
  const { riskId } = useParams({ from: '/admin/trust/destination-risk/$riskId' });
  const queryClient = useQueryClient();
  const [mode, setMode] = useState<ActionMode>(null);
  const [decision, setDecision] = useState<'allow' | 'review' | 'block'>('allow');
  const [reason, setReason] = useState('');
  const [ttlMinutes, setTTLMinutes] = useState('15');
  const [result, setResult] = useState('');
  const [validation, setValidation] = useState('');

  const detail = useQuery({
    queryKey: ['p16-admin-destination-risk', riskId],
    queryFn: () => trustGet<DestinationDetailResponse>(`/api/admin/destination-risks/${encodeURIComponent(riskId)}`),
    retry: false,
  });
  const authority = useQuery({
    queryKey: ['p16-admin-destination-risks', 'csrf'],
    queryFn: () => trustGet<DestinationListResponse>('/api/admin/destination-risks?limit=1'),
    retry: false,
  });
  const item = detail.data?.risk;
  const csrf = authority.data?.csrf_token ?? '';

  const rescan = useMutation({
    mutationFn: () => {
      if (!csrf) throw new Error('Mutation authority is unavailable. Reload current administrator authority before retrying.');
      return trustWrite<{ risk_id: number; created: boolean; status: string }>(`/api/admin/destination-risks/${encodeURIComponent(riskId)}/rescan`, csrf, {});
    },
    onSuccess: async (data) => {
      setValidation(''); setMode(null);
      setResult(data.created ? `Rescan #${data.risk_id} was queued and is now the auditable follow-up request.` : `The existing idempotent rescan #${data.risk_id} was returned.`);
      await queryClient.invalidateQueries({ queryKey: ['p16-admin-destination-risk'] });
      await queryClient.invalidateQueries({ queryKey: ['p16-admin-destination-risks'] });
    },
    onError: (error) => setValidation(trustErrorMessage(error)),
  });

  const override = useMutation({
    mutationFn: () => {
      setValidation('');
      const cleanReason = reason.trim();
      const minutes = Number(ttlMinutes);
      if (!csrf) throw new Error('Mutation authority is unavailable. Reload current administrator authority before retrying.');
      if (!cleanReason || cleanReason.length > 500) throw new Error('Enter an accountable override reason up to 500 characters.');
      if (!Number.isSafeInteger(minutes) || minutes < 1 || minutes > 1440) throw new Error('Override validity must be between 1 minute and 24 hours.');
      return trustWrite<{ override_id: number; decision: string; expires_at: string }>(`/api/admin/destination-risks/${encodeURIComponent(riskId)}/override`, csrf, {
        decision,
        reason: cleanReason,
        expires_at: new Date(Date.now() + minutes * 60_000).toISOString(),
      }, { idempotency: false });
    },
    onSuccess: async (data) => {
      setValidation(''); setMode(null); setReason('');
      setResult(`Override #${data.override_id} recorded as ${data.decision} until ${formatTimestamp(data.expires_at)}. The decision remains fingerprint/policy bound and auditable.`);
      await queryClient.invalidateQueries({ queryKey: ['p16-admin-destination-risk'] });
      await queryClient.invalidateQueries({ queryKey: ['p16-admin-destination-risks'] });
    },
    onError: (error) => setValidation(trustErrorMessage(error)),
  });

  const error = detail.error ?? authority.error;
  const pageState = detail.isPending ? 'loading' : detail.isError ? 'error' : mode ? 'destructive-confirm' : destinationState(item);

  return (
    <AdminShell state={trustShellState(error)}>
      <section className="trust-page" data-page="admin-destination-risk-detail" data-state={pageState}>
        <header>
          <p className="trust-eyebrow">TRUST &amp; SAFETY / DESTINATION RISK</p>
          <h1>Destination risk detail</h1>
          <p>Operate only on the exact current fingerprint and policy authority. Customer destinations and raw provider evidence are intentionally absent.</p>
        </header>
        <TrustNav />
        {detail.isPending ? <p role="status">Loading destination risk detail…</p> : null}
        {detail.isError ? <InlineMessage variant="danger">{trustErrorMessage(detail.error)}</InlineMessage> : null}
        {authority.isError ? <InlineMessage variant="danger">Mutation authority could not be loaded. Read-only detail remains available.</InlineMessage> : null}
        {result ? <InlineMessage variant="success">{result}</InlineMessage> : null}
        {validation ? <InlineMessage variant="danger">{validation}</InlineMessage> : null}
        {item ? <>
          <Card as="section" aria-labelledby="destination-authority-title">
            <header className="trust-action-row"><div><h2 id="destination-authority-title">Current control authority</h2><p>{item.reason_category}</p></div><TrustState value={destinationState(item)} /></header>
            <dl className="trust-kv">
              <div><dt>Risk record</dt><dd>#{item.id}</dd></div>
              <div><dt>Workspace</dt><dd>{item.workspace_id}</dd></div>
              <div><dt>Link</dt><dd>#{item.link_id}</dd></div>
              <div><dt>Fingerprint</dt><dd className="trust-mono" title={item.risk_fingerprint}>{compactFingerprint(item.risk_fingerprint)}</dd></div>
              <div><dt>Policy</dt><dd>{item.policy_version}</dd></div>
              <div><dt>Request / scan</dt><dd>{item.request_kind} / {item.scan_status}</dd></div>
              <div><dt>Attempts</dt><dd>{item.attempts} / {item.max_attempts}</dd></div>
              <div><dt>Coverage</dt><dd>{item.target_count} target(s), {item.provider_count} provider observation(s)</dd></div>
              <div><dt>Override</dt><dd>{item.has_active_override ? 'Active, exact-authority bound' : 'None'}</dd></div>
              <div><dt>Valid until</dt><dd>{formatTimestamp(item.valid_until)}</dd></div>
              <div><dt>Updated</dt><dd>{formatTimestamp(item.updated_at)}</dd></div>
            </dl>
          </Card>
          <Card as="section" className="trust-actions" aria-labelledby="destination-actions-title">
            <div><h2 id="destination-actions-title">Security actions</h2><p className="trust-confirm-note">Every action is server-authorized, CSRF protected and correlation/audit bound. No operator control on this page can skip current safety authority.</p></div>
            <div className="trust-action-row">
              <Button onClick={() => { setValidation(''); setMode(mode === 'rescan' ? null : 'rescan'); }}>Request rescan</Button>
              <Button variant="ghost" onClick={() => { setValidation(''); setMode(mode === 'override' ? null : 'override'); }}>Create bounded override</Button>
            </div>
            {mode === 'rescan' ? <div className="trust-confirm" data-confirm="rescan">
              <h3>Confirm destination rescan</h3>
              <p className="trust-confirm-note">Impact: a new exact-fingerprint scan request is queued. Until current allow authority exists, redirect safety remains fail closed.</p>
              <div className="trust-action-row"><Button disabled={rescan.isPending} onClick={() => rescan.mutate()}>{rescan.isPending ? 'Queueing…' : 'Confirm rescan'}</Button><Button variant="ghost" onClick={() => setMode(null)}>Cancel</Button></div>
            </div> : null}
            {mode === 'override' ? <div className="trust-confirm" data-confirm="override">
              <h3>Confirm bounded manual override</h3>
              <p className="trust-confirm-note">Impact: this changes effective risk authority only for the exact current fingerprint, policy version and bounded lifetime. Fingerprint/policy changes invalidate it.</p>
              <label>Decision<select value={decision} onChange={(event) => setDecision(event.currentTarget.value as typeof decision)}><option value="allow">Allow</option><option value="review">Review</option><option value="block">Block</option></select></label>
              <label>Accountable reason<textarea rows={4} maxLength={500} value={reason} onChange={(event) => setReason(event.currentTarget.value)} /></label>
              <label>Validity<select value={ttlMinutes} onChange={(event) => setTTLMinutes(event.currentTarget.value)}><option value="15">15 minutes</option><option value="60">1 hour</option><option value="360">6 hours</option><option value="1440">24 hours</option></select></label>
              <div className="trust-action-row"><Button disabled={override.isPending} onClick={() => override.mutate()}>{override.isPending ? 'Recording…' : 'Confirm override'}</Button><Button variant="ghost" onClick={() => setMode(null)}>Cancel</Button></div>
            </div> : null}
          </Card>
        </> : null}
        <a href="/admin/trust/destination-risk">Back to destination risk</a>
      </section>
    </AdminShell>
  );
}
