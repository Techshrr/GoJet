import { useCallback, useEffect, useMemo, useState } from 'react';
import { Button } from '@gojet/ui';
import { adminRequest, ErrorNotice, JsonPreview, ProtectedLayout, type JsonObject, useAdminSession } from './api';

export function OverviewPage() {
  const auth = useAdminSession(); const [overview, setOverview] = useState<JsonObject | null>(null); const [error, setError] = useState('');
  useEffect(() => { if (!auth.session) return; void adminRequest<{ overview: JsonObject }>('/api/admin/overview').then((result) => { setOverview(result.overview); setError(''); }).catch((err) => setError(err instanceof Error ? err.message : 'internal_error')); }, [auth.session]);
  const state = auth.busy ? 'loading' : auth.error || error ? 'partial-service-degradation' : 'normal';
  return <ProtectedLayout state={state === 'partial-service-degradation' ? 'partial-service-degradation' : 'normal'}><section className="p17-admin-page" data-page="admin-overview" data-state={state}><header><p className="p17-kicker">Operations</p><h1>Operations overview</h1><p>Counts come from the exact server-side overview authority.</p></header><ErrorNotice error={auth.error || error} />{overview && <div className="p17-metrics">{Object.entries(overview).map(([key, value]) => <article key={key}><strong>{String(value)}</strong><span>{key.replaceAll('_', ' ')}</span></article>)}</div>}</section></ProtectedLayout>;
}

function useOperations() {
  const auth = useAdminSession(); const [jobs, setJobs] = useState<JsonObject[]>([]); const [services, setServices] = useState<JsonObject[]>([]); const [busy, setBusy] = useState(true); const [error, setError] = useState('');
  const reloadOperations = useCallback(async () => { if (!auth.session) return; setBusy(true); setError(''); try { const [j, s] = await Promise.all([adminRequest<{ items: JsonObject[] }>('/api/admin/operations/jobs'), adminRequest<{ items: JsonObject[] }>('/api/admin/operations/services')]); setJobs(j.items || []); setServices(s.items || []); } catch (err) { setError(err instanceof Error ? err.message : 'internal_error'); } finally { setBusy(false); } }, [auth.session]);
  useEffect(() => { void reloadOperations(); }, [reloadOperations]);
  return { ...auth, jobs, services, busy: busy || auth.busy, error: error || auth.error, reloadOperations };
}

function OperationsPage({ mode }: { mode: 'jobs' | 'services' }) {
  const ops = useOperations(); const [confirm, setConfirm] = useState<JsonObject | null>(null); const [reason, setReason] = useState('Browser operational recovery'); const items = mode === 'jobs' ? ops.jobs : ops.services;
  const staleJob = mode === 'jobs' && items.some((item) => item.updated_at && Date.now() - new Date(String(item.updated_at)).getTime() > 24 * 60 * 60 * 1000);
  const normalized = mode === 'jobs'
    ? staleJob ? 'stale' : items.some((item) => String(item.status) === 'failed') ? 'failed' : items.some((item) => String(item.status) === 'retry') ? 'retrying' : items.length ? 'normal' : 'normal'
    : items.some((item) => String(item.status) === 'degraded') ? 'partial-service-degradation' : items.some((item) => String(item.status) === 'unknown') ? 'unavailable' : items.length && items.every((item) => String(item.status) === 'healthy') ? 'healthy' : 'normal';
  const state = ops.error ? (ops.error === 'forbidden' ? 'permission-denied' : 'error') : ops.busy ? 'loading' : confirm ? (mode === 'services' ? 'restart-confirm' : 'destructive-confirm') : normalized;
  const id = confirm ? String(confirm.id || confirm.service_id || confirm.job_id) : '';
  const impact = confirm ? (mode === 'jobs' ? `requeue destination-risk job ${id}` : `restart service ${id}`) : '';
  async function mutate() { if (!ops.session || !confirm) return; const path = mode === 'jobs' ? `/api/admin/operations/jobs/${id}/requeue` : `/api/admin/operations/services/${id}/restart`; try { await adminRequest(path, { method: 'POST', body: JSON.stringify({ reason, impact_confirmation: impact }) }, ops.session.csrf_token); setConfirm(null); await ops.reloadOperations(); } catch { await ops.reloadOperations(); } }
  return <ProtectedLayout state={state === 'permission-denied' ? 'permission-denied' : state === 'partial-service-degradation' ? 'partial-service-degradation' : 'normal'}><section className="p17-admin-page" data-page={`admin-operations-${mode}`} data-state={state}><header><p className="p17-kicker">Operations</p><h1>{mode === 'jobs' ? 'Jobs' : 'Services'}</h1><p>High-risk operations require operations.manage, reason and exact impact confirmation.</p></header><ErrorNotice error={ops.error} /><ul className="p17-list">{items.map((item, index) => { const itemID = String(item.id || item.service_id || item.job_id || index); return <li key={itemID}><button type="button" onClick={() => setConfirm(item)}>{String(item.name || item.kind || itemID)}</button><span>{String(item.status || 'unknown')}</span></li>; })}</ul>{confirm && <section className="p17-confirm" role="alertdialog" aria-label={mode === 'jobs' ? 'Confirm job requeue' : 'Confirm service restart'}><label>Reason<input value={reason} onChange={(event) => setReason(event.target.value)} /></label><label>Impact confirmation<input readOnly value={impact} /></label><Button type="button" onClick={() => void mutate()}>{mode === 'jobs' ? 'Requeue job' : 'Restart service'}</Button><Button type="button" variant="ghost" onClick={() => setConfirm(null)}>Cancel</Button></section>}</section></ProtectedLayout>;
}

export function OperationsJobsPage() { return <OperationsPage mode="jobs" />; }
export function OperationsServicesPage() { return <OperationsPage mode="services" />; }

export function AuditPage() {
  const auth = useAdminSession(); const [items, setItems] = useState<JsonObject[]>([]); const [busy, setBusy] = useState(true); const [error, setError] = useState(''); const [filter, setFilter] = useState(''); const [selected, setSelected] = useState<JsonObject | null>(null);
  useEffect(() => { if (!auth.session) return; setBusy(true); void adminRequest<{ items: JsonObject[] }>('/api/admin/audit').then((result) => { setItems(result.items || []); setError(''); }).catch((err) => setError(err instanceof Error ? err.message : 'internal_error')).finally(() => setBusy(false)); }, [auth.session]);
  const filtered = useMemo(() => items.filter((item) => JSON.stringify(item).toLowerCase().includes(filter.toLowerCase())), [items, filter]); const stale = items.some((item) => item.created_at && Date.now() - new Date(String(item.created_at)).getTime() > 24 * 60 * 60 * 1000); const partial = selected && (!!selected.before !== !!selected.after);
  const state = auth.error || error ? 'error' : auth.busy || busy ? 'loading' : filter && filtered.length === 0 ? 'filtered-empty' : items.length === 0 ? 'empty' : selected ? (partial ? 'partial-diff' : 'detail') : stale ? 'stale' : 'ready';
  return <ProtectedLayout><section className="p17-admin-page" data-page="admin-audit" data-state={state}><header><p className="p17-kicker">Audit</p><h1>Immutable audit</h1><p>Before/after metadata is redacted by the server before display.</p></header><ErrorNotice error={auth.error || error} /><label>Filter audit<input type="search" value={filter} onChange={(event) => setFilter(event.target.value)} /></label><ul className="p17-list">{filtered.map((item) => <li key={String(item.id)}><button type="button" onClick={() => setSelected(item)}>{String(item.action)}</button><span>{String(item.result)}</span><time>{String(item.created_at || '')}</time></li>)}</ul>{selected && <JsonPreview value={selected} />}</section></ProtectedLayout>;
}

function PlatformReadPage({ page, title, endpoint, responseKey }: { page: string; title: string; endpoint: string; responseKey: string }) {
  const auth = useAdminSession(); const [value, setValue] = useState<any>(null); const [busy, setBusy] = useState(true); const [error, setError] = useState('');
  const load = useCallback(async () => { if (!auth.session) return; setBusy(true); setError(''); try { const result = await adminRequest<JsonObject>(endpoint); setValue(result[responseKey]); } catch (err) { setError(err instanceof Error ? err.message : 'internal_error'); } finally { setBusy(false); } }, [auth.session, endpoint, responseKey]); useEffect(() => { void load(); }, [load]);
  const providerState = value && typeof value === 'object' && !Array.isArray(value) ? String(value.provider_state || value.https_state || '') : '';
  const state = auth.error || error ? (auth.error === 'forbidden' || error === 'forbidden' ? 'permission-denied' : 'error') : auth.busy || busy ? 'loading' : providerState === 'provider_error' || providerState === 'failed' ? 'error' : Array.isArray(value) && value.length === 0 ? 'empty' : 'ready';
  return <ProtectedLayout state={state === 'permission-denied' ? 'permission-denied' : 'normal'}><section className="p17-admin-page" data-page={page} data-state={state}><header><p className="p17-kicker">Platform</p><h1>{title}</h1></header><ErrorNotice error={auth.error || error} />{!busy && !error && <JsonPreview value={value} />}</section></ProtectedLayout>;
}

export function PlatformGeneralPage() { return <PlatformReadPage page="admin-platform-general" title="General settings" endpoint="/api/admin/settings/general" responseKey="setting" />; }
export function OfficialDomainsPage() { return <PlatformReadPage page="admin-official-domains" title="Official domains" endpoint="/api/admin/official-domains" responseKey="items" />; }
export function TurnstilePage() { return <PlatformReadPage page="admin-turnstile" title="Turnstile" endpoint="/api/admin/bot-protection" responseKey="bot_protection" />; }
export function AnnouncementsPage() { return <PlatformReadPage page="admin-announcements" title="Announcements" endpoint="/api/admin/announcements" responseKey="items" />; }
