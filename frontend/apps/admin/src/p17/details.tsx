import { useEffect, useState } from 'react';
import { useParams } from '@tanstack/react-router';
import { adminRequest, ErrorNotice, JsonPreview, ProtectedLayout, type JsonObject, useAdminSession } from './api';

function DetailFrame({ page, title, item, busy, error }: { page: string; title: string; item: JsonObject | null; busy: boolean; error: string }) {
  const state = error ? (error === 'forbidden' ? 'permission-denied' : 'error') : busy ? 'loading' : item ? 'detail' : 'empty';
  return <ProtectedLayout state={state === 'permission-denied' ? 'permission-denied' : 'normal'}><section className="p17-admin-page" data-page={page} data-state={state}><header><p className="p17-kicker">Direct route</p><h1>{title}</h1><p>Authorization is re-evaluated by the server for this direct URL.</p></header><ErrorNotice error={error} />{item && <JsonPreview value={item} />}</section></ProtectedLayout>;
}

function ManagedDetail({ kind, param }: { kind: 'users' | 'workspaces'; param: 'userId' | 'workspaceId' }) {
  const auth = useAdminSession(); const params = useParams({ strict: false }) as Record<string, string | undefined>; const id = params[param] || ''; const [item, setItem] = useState<JsonObject | null>(null); const [busy, setBusy] = useState(true); const [error, setError] = useState('');
  useEffect(() => { if (!auth.session || !id) return; setBusy(true); void adminRequest<JsonObject>(`/api/admin/${kind}/${encodeURIComponent(id)}`).then((result) => { setItem((result[kind === 'users' ? 'user' : 'workspace'] as JsonObject | undefined) || null); setError(''); }).catch((err) => setError(err instanceof Error ? err.message : 'internal_error')).finally(() => setBusy(false)); }, [auth.session, id, kind]);
  return <DetailFrame page={`admin-${kind}-detail`} title={kind === 'users' ? 'User detail' : 'Workspace detail'} item={item} busy={auth.busy || busy} error={auth.error || error} />;
}

export function UserDetailPage() { return <ManagedDetail kind="users" param="userId" />; }
export function WorkspaceDetailPage() { return <ManagedDetail kind="workspaces" param="workspaceId" />; }

export function DomainEntitlementDetailPage() {
  const auth = useAdminSession(); const params = useParams({ strict: false }) as Record<string, string | undefined>; const workspaceID = params.workspaceId || ''; const [item, setItem] = useState<JsonObject | null>(null); const [busy, setBusy] = useState(true); const [error, setError] = useState('');
  useEffect(() => { if (!auth.session || !workspaceID) return; setBusy(true); void adminRequest<{ entitlement: JsonObject }>(`/api/admin/domain-entitlements/${encodeURIComponent(workspaceID)}`).then((result) => { setItem(result.entitlement || null); setError(''); }).catch((err) => setError(err instanceof Error ? err.message : 'internal_error')).finally(() => setBusy(false)); }, [auth.session, workspaceID]);
  return <DetailFrame page="admin-domain-entitlement-detail" title="Domain entitlement detail" item={item} busy={auth.busy || busy} error={auth.error || error} />;
}

export function AdministratorDetailPage() {
  const auth = useAdminSession(); const params = useParams({ strict: false }) as Record<string, string | undefined>; const adminID = params.adminId || ''; const [item, setItem] = useState<JsonObject | null>(null); const [busy, setBusy] = useState(true); const [error, setError] = useState('');
  useEffect(() => { if (!auth.session || !adminID) return; setBusy(true); void adminRequest<{ items: JsonObject[] }>('/api/admin/administrators').then((result) => { setItem((result.items || []).find((candidate) => String(candidate.id) === adminID) || null); setError(''); }).catch((err) => setError(err instanceof Error ? err.message : 'internal_error')).finally(() => setBusy(false)); }, [auth.session, adminID]);
  return <DetailFrame page="admin-administrator-detail" title="Administrator detail" item={item} busy={auth.busy || busy} error={auth.error || error} />;
}
