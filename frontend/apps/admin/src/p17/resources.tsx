import { useEffect, useState } from 'react';
import { useParams } from '@tanstack/react-router';
import { Button } from '@gojet/ui';
import { adminRequest, ErrorNotice, ProtectedLayout, useAdminSession } from './api';

const resources = {
  links: { title: 'Links', api: '/api/admin/links', route: '/admin/resources/links', key: 'link', permission: 'links.manage' },
  domains: { title: 'Domains', api: '/api/admin/domains', route: '/admin/resources/domains', key: 'domain', permission: 'domains.manage' },
  qr: { title: 'QR codes', api: '/api/admin/resources/qr', route: '/admin/resources/qr', key: 'resource', permission: 'content.manage' },
  text: { title: 'Text', api: '/api/admin/resources/text', route: '/admin/resources/text', key: 'resource', permission: 'content.manage' },
  bio: { title: 'Bio', api: '/api/admin/resources/bio', route: '/admin/resources/bio', key: 'resource', permission: 'content.manage' },
  files: { title: 'Files', api: '/api/admin/files', route: '/admin/files', key: 'file', permission: 'files.manage' },
} as const;
type Kind = keyof typeof resources;
type Resource = { id: number; [key: string]: string | number | boolean | null | undefined };
type FileAction = 'quarantine' | 'rescan' | 'restore' | 'delete';
const fields = ['workspace_id', 'title', 'label', 'original_name', 'hostname', 'hostname_ascii', 'status', 'state', 'routing_state', 'ownership_status', 'https_status', 'risk_status', 'scan_state', 'scan_generation', 'size_bytes', 'published', 'deleted', 'deleted_at', 'updated_at'];

export function ResourcePage({ kind, detail = false }: { kind: Kind; detail?: boolean }) {
  const config = resources[kind];
  const params = useParams({ strict: false }) as Record<string, string | undefined>;
  const id = params.resourceId || '';
  // Remount when the URL changes so an old resource cannot appear under a new URL.
  return <ResourceContent key={`${kind}:${detail}:${id}`} kind={kind} detail={detail} id={id} config={config} />;
}

function ResourceContent({ kind, detail, id, config }: { kind: Kind; detail: boolean; id: string; config: typeof resources[Kind] }) {
  const auth = useAdminSession();
  const [items, setItems] = useState<Resource[]>([]);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState('');
  const [action, setAction] = useState<FileAction | null>(null);
  const [reason, setReason] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [saving, setSaving] = useState(false);
  const [revision, setRevision] = useState(0);
  const permitted = auth.session?.permissions.includes(config.permission) === true;
  useEffect(() => {
    let active = true;
    setItems([]);
    if (!auth.session) return () => { active = false; };
    if (detail && !/^[1-9][0-9]*$/.test(id)) { setError('invalid_resource_id'); setBusy(false); return; }
    setBusy(true); setError('');
    adminRequest<Record<string, Resource | Resource[]>>(`${config.api}${detail ? `/${encodeURIComponent(id)}` : '?limit=100'}`)
      .then(result => {
        const value = detail ? result[config.key] : result.items;
        if (detail ? !value || Array.isArray(value) : !Array.isArray(value)) throw new Error('invalid_resource_response');
        if (!value) throw new Error('invalid_resource_response');
        const records = Array.isArray(value) ? value : [value];
        if (records.some(record => !record || !Number.isSafeInteger(record.id) || record.id <= 0)) throw new Error('invalid_resource_response');
        if (detail && String(records[0]?.id) !== id) throw new Error('resource_identity_mismatch');
        if (active) setItems(records);
      })
      .catch(err => { if (active) setError(err instanceof Error ? err.message : 'internal_error'); })
      .finally(() => { if (active) setBusy(false); });
    return () => { active = false; };
  }, [auth.session, config, detail, id, revision]);
  async function applyAction() {
    if (!action || !auth.session || !permitted || !reason.trim() || confirmation !== action.toUpperCase() || saving) return;
    setSaving(true); setError('');
    try {
      // Refresh the one-time CSRF authority for this explicit mutation; never replay it automatically.
      const session = await adminRequest<{ csrf_token: string }>('/api/admin/auth/session');
      await adminRequest(`${config.api}/${encodeURIComponent(id)}/${action}`, { method: 'POST', body: JSON.stringify({ reason: reason.trim() }) }, session.csrf_token);
      setAction(null); setReason(''); setConfirmation(''); setRevision(value => value + 1);
    } catch (err) { setError(err instanceof Error ? err.message : 'internal_error'); }
    finally { setSaving(false); }
  }
  const item = items[0];
  const failure = auth.error || error;
  const state = failure ? (failure === 'forbidden' ? 'permission-denied' : 'error') : auth.busy || busy ? 'loading' : action ? 'destructive-confirm' : detail ? 'detail' : items.length ? 'ready' : 'empty';
  return <ProtectedLayout state={state === 'permission-denied' ? 'permission-denied' : 'normal'}>
    <section className="p17-admin-page" data-page={`admin-resources-${kind}`} data-state={state}>
      <header><h1>{config.title}{detail ? ' detail' : ''}</h1></header>
      <nav aria-label="Resource inventory" className="p17-actions">{Object.entries(resources).map(([key, value]) => <a key={key} href={value.route} aria-current={kind === key ? 'page' : undefined}>{value.title}</a>)}</nav>
      <ErrorNotice error={failure} />
      {state === 'loading' ? <p role="status">Loading resources…</p> : null}
      {state === 'empty' ? <p>No resources found.</p> : null}
      {!detail && !failure && !busy ? <><p>Showing up to 100 recently updated resources.</p><ul className="p17-list">{items.map(item => <li key={item.id}><a href={`${config.route}/${item.id}`}>{String(item.title || item.label || item.original_name || item.hostname_ascii || item.id)}</a><span>{String(item.status || item.state || item.scan_state || item.routing_state || '')}</span></li>)}</ul></> : null}
      {detail && !failure && !busy && item ? <dl>{fields.filter(field => item[field] !== undefined && item[field] !== null).map(field => <div key={field}><dt>{field.replaceAll('_', ' ')}</dt><dd style={{ overflowWrap: 'anywhere' }}>{String(item[field])}</dd></div>)}</dl> : null}
      {detail && kind === 'files' && permitted && !failure && !busy && item ? <div className="p17-actions">{(['quarantine', 'rescan', 'restore', 'delete'] as const).map(value => <Button key={value} disabled={saving} onClick={() => { setAction(value); setReason(''); setConfirmation(''); }}>{value}</Button>)}</div> : null}
      {action ? <form role="alertdialog" aria-label={`Confirm ${action}`} onSubmit={event => { event.preventDefault(); void applyAction(); }}>
        <p>This changes file {id}. Current scan and authorization rules still apply.</p>
        <label>Reason<input required value={reason} onChange={event => setReason(event.target.value)} /></label>
        <label>Type {action.toUpperCase()}<input required value={confirmation} onChange={event => setConfirmation(event.target.value)} /></label>
        <Button type="submit" disabled={saving || !reason.trim() || confirmation !== action.toUpperCase()}>Confirm {action}</Button>
        <Button type="button" disabled={saving} onClick={() => setAction(null)}>Cancel</Button>
      </form> : null}
    </section>
  </ProtectedLayout>;
}
