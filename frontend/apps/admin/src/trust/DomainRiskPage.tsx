import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useParams } from '@tanstack/react-router';
import { Button, Card, DataTable, EmptyState, InlineMessage, useShellViewport } from '@gojet/ui';
import { AdminShell } from '../shell/AdminShell';
import { TrustNav, TrustState } from './TrustNav';
import { formatTimestamp, trustErrorMessage, trustGet, trustShellState, trustWrite, type DomainRiskRecord } from './runtime';

type DomainListResponse = { items: DomainRiskRecord[]; csrf_token: string };
type DomainDetailResponse = { domain_risk: DomainRiskRecord; csrf_token?: string };
const partialReasons = ['provider-partial', 'provider-unavailable', 'provider-incomplete', 'provider-malformed'];

function domainState(item?: DomainRiskRecord): string {
  if (!item) return 'empty';
  const reason = item.reason_category.toLowerCase();
  if (item.state === 'provider_partial' || item.state === 'malformed') return 'provider-partial';
  if (reason.includes('stale')) return 'stale';
  if (partialReasons.some((value) => reason.includes(value)) && item.state !== 'allow') return 'provider-partial';
  if (item.state === 'revalidating' || (item.request_kind === 'revalidation' && item.state === 'pending')) return 'revalidating';
  return item.state;
}

function domainListState(items: DomainRiskRecord[]): string {
  const states = items.map((item) => domainState(item));
  for (const candidate of ['provider-partial', 'stale', 'block', 'revalidating', 'review', 'pending', 'allow']) if (states.includes(candidate)) return candidate;
  return 'empty';
}

function axisLabel(label: string, value: string) { return <div><dt>{label}</dt><dd><strong>{value}</strong></dd></div>; }

export default function DomainRiskListPage() {
  const viewport = useShellViewport();
  const query = useQuery({ queryKey: ['p16-admin-domain-risks'], queryFn: () => trustGet<DomainListResponse>('/api/admin/domain-risks?limit=100'), retry: false });
  const items = query.data?.items ?? [];
  const state = query.isPending ? 'loading' : query.isError ? 'error' : items.length === 0 ? 'empty' : domainListState(items);
  return <AdminShell state={trustShellState(query.error)}><section className="trust-page" data-page="admin-domain-risk" data-state={state}>
    <header><p className="trust-eyebrow">TRUST &amp; SAFETY / DOMAIN RISK</p><h1>Domain risk</h1><p>Inspect reputation and revalidation authority without collapsing entitlement, ownership, ingress DNS, HTTPS or routing into the reputation decision.</p></header><TrustNav />
    <p className="trust-safe-note">A reputation allow never substitutes for entitlement, ownership, DNS or HTTPS. A destination allow never substitutes for domain authority.</p>
    {query.isPending ? <p role="status">Loading domain risk authority…</p> : null}{query.isError ? <InlineMessage variant="danger">{trustErrorMessage(query.error)}</InlineMessage> : null}
    {!query.isPending && !query.isError && items.length === 0 ? <EmptyState title="No domain risk records" reason="No durable domain reputation evaluation is available to this administrator." /> : null}
    {items.length > 0 && viewport !== 'mobile' ? <DataTable caption="Domain risk authority"><thead><tr><th scope="col">Domain</th><th scope="col">Reputation</th><th scope="col">Trust axes</th><th scope="col">Reason</th><th scope="col">Next due</th></tr></thead><tbody>{items.map((item) => <tr key={item.domain_id}><td><a href={`/admin/trust/domain-risk/${item.domain_id}`}>{item.hostname_ascii}</a><br /><span>#{item.domain_id} · {item.workspace_id}</span></td><td><TrustState value={domainState(item)} /></td><td>Entitlement {item.entitlement_status} · Ownership {item.ownership_status}<br />DNS {item.ingress_dns_status} · HTTPS {item.https_status} · Routing {item.routing_status}</td><td>{item.reason_category}</td><td>{formatTimestamp(item.next_due_at)}</td></tr>)}</tbody></DataTable> : null}
    {items.length > 0 && viewport === 'mobile' ? <div className="trust-card-list">{items.map((item) => <Card as="article" key={item.domain_id}><header><strong>{item.hostname_ascii}</strong><TrustState value={domainState(item)} /></header><span>{item.reason_category}</span><span>Ownership {item.ownership_status} · DNS {item.ingress_dns_status} · HTTPS {item.https_status}</span><a href={`/admin/trust/domain-risk/${item.domain_id}`}>Open domain risk detail</a></Card>)}</div> : null}
  </section></AdminShell>;
}

export function DomainRiskDetailPage() {
  const { domainId } = useParams({ from: '/admin/trust/domain-risk/$domainId' });
  const queryClient = useQueryClient();
  const [confirming, setConfirming] = useState(false); const [reason, setReason] = useState(''); const [validation, setValidation] = useState(''); const [result, setResult] = useState('');
  const detail = useQuery({ queryKey: ['p16-admin-domain-risk', domainId], queryFn: () => trustGet<DomainDetailResponse>(`/api/admin/domain-risks/${encodeURIComponent(domainId)}`), retry: false });
  const authority = useQuery({ queryKey: ['p16-admin-domain-risks', 'csrf'], queryFn: () => trustGet<DomainListResponse>('/api/admin/domain-risks?limit=1'), retry: false });
  const item = detail.data?.domain_risk; const csrf = detail.data?.csrf_token ?? authority.data?.csrf_token ?? '';
  const revalidate = useMutation({
    mutationFn: () => { setValidation(''); const cleanReason = reason.trim(); if (!csrf) throw new Error('Mutation authority is unavailable. Reload current administrator authority before retrying.'); if (!cleanReason || cleanReason.length > 500) throw new Error('Enter an accountable revalidation reason up to 500 characters.'); return trustWrite<{ domain_risk: DomainRiskRecord; created: boolean }>(`/api/admin/domain-risks/${encodeURIComponent(domainId)}/revalidate`, csrf, { reason: cleanReason }); },
    onSuccess: async (data) => { setConfirming(false); setReason(''); setValidation(''); setResult(data.created ? `Domain reputation was revalidated. Current state: ${data.domain_risk.state}.` : `The existing idempotent revalidation result was returned. Current state: ${data.domain_risk.state}.`); await queryClient.invalidateQueries({ queryKey: ['p16-admin-domain-risk'] }); await queryClient.invalidateQueries({ queryKey: ['p16-admin-domain-risks'] }); },
    onError: (error) => setValidation(trustErrorMessage(error)),
  });
  const error = detail.error ?? authority.error; const pageState = revalidate.isPending ? 'revalidating' : detail.isPending ? 'loading' : detail.isError ? 'error' : domainState(item);
  return <AdminShell state={trustShellState(error)}><section className="trust-page" data-page="admin-domain-risk-detail" data-state={pageState}>
    <header><p className="trust-eyebrow">TRUST &amp; SAFETY / DOMAIN RISK</p><h1>Domain risk detail</h1><p>Reputation is one independent authority axis. Revalidation cannot grant entitlement, ownership, ingress DNS, HTTPS or routing state.</p></header><TrustNav />
    {detail.isPending ? <p role="status">Loading domain risk detail…</p> : null}{detail.isError ? <InlineMessage variant="danger">{trustErrorMessage(detail.error)}</InlineMessage> : null}{authority.isError ? <InlineMessage variant="danger">Mutation authority could not be loaded. Read-only detail remains available.</InlineMessage> : null}{result ? <InlineMessage variant="success">{result}</InlineMessage> : null}{validation ? <InlineMessage variant="danger">{validation}</InlineMessage> : null}
    {item ? <><Card as="section" aria-labelledby="domain-authority-title"><header className="trust-action-row"><div><h2 id="domain-authority-title">Independent domain authority</h2><p>{item.hostname_ascii}</p></div><TrustState value={domainState(item)} /></header><dl className="trust-kv"><div><dt>Workspace</dt><dd>{item.workspace_id}</dd></div><div><dt>Domain / evaluation</dt><dd>#{item.domain_id} / #{item.evaluation_id}</dd></div><div><dt>Reputation reason</dt><dd>{item.reason_category}</dd></div><div><dt>Policy</dt><dd>{item.policy_version}</dd></div><div><dt>Request kind</dt><dd>{item.request_kind}</dd></div><div><dt>Provider observations</dt><dd>{item.provider_count}</dd></div>{axisLabel('Entitlement', item.entitlement_status)}{axisLabel('Ownership', item.ownership_status)}{axisLabel('Ingress DNS', item.ingress_dns_status)}{axisLabel('HTTPS', item.https_status)}{axisLabel('Routing', item.routing_status)}<div><dt>Checked</dt><dd>{formatTimestamp(item.checked_at)}</dd></div><div><dt>Next due</dt><dd>{formatTimestamp(item.next_due_at)}</dd></div><div><dt>Valid until</dt><dd>{formatTimestamp(item.valid_until)}</dd></div></dl></Card>
      <Card as="section" className="trust-actions" aria-labelledby="domain-actions-title"><div><h2 id="domain-actions-title">Reputation action</h2><p className="trust-confirm-note">Revalidation refreshes only P16 reputation authority. Existing P06 trust axes remain independently enforced.</p></div><Button onClick={() => { setValidation(''); setConfirming((value) => !value); }}>Request revalidation</Button>{confirming ? <div className="trust-confirm" data-confirm="revalidate"><h3>Confirm domain reputation revalidation</h3><p className="trust-confirm-note">Impact: the server asks the configured reputation signal source and applies local versioned policy. Provider failure remains a non-allow state.</p><label>Accountable reason<textarea rows={4} maxLength={500} value={reason} onChange={(event) => setReason(event.currentTarget.value)} /></label><div className="trust-action-row"><Button disabled={revalidate.isPending} onClick={() => revalidate.mutate()}>{revalidate.isPending ? 'Revalidating…' : 'Confirm revalidation'}</Button><Button variant="ghost" onClick={() => setConfirming(false)}>Cancel</Button></div></div> : null}</Card></> : null}
    <a href="/admin/trust/domain-risk">Back to domain risk</a>
  </section></AdminShell>;
}
