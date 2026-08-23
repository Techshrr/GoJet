import { type FormEvent, useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { GoJetApiError, type TextVisibility } from '@gojet/api-client';
import { Button, Card, InlineMessage, TextField } from '@gojet/ui';
import { WorkspaceShell } from '../shell/WorkspaceShell';
import { isReadOnly } from '../links/runtime';
import { createWorkspaceTextClient, readTextRuntime } from '../text/runtime';

function toLocalDateTime(value?: string | null): string {
  if (!value) return '';
  const date = new Date(value);
  const shifted = new Date(date.valueOf() - date.getTimezoneOffset() * 60_000);
  return shifted.toISOString().slice(0, 16);
}
function toRFC3339(value: string): string | undefined {
  if (!value) return undefined;
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? undefined : date.toISOString();
}

export default function TextDetailPage() {
  const { shareId } = useParams({ from: '/app/text/$shareId' });
  const numericId = Number(shareId);
  const runtime = useMemo(() => readTextRuntime(), []);
  const client = useMemo(() => runtime ? createWorkspaceTextClient(runtime) : null, [runtime]);
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const readOnly = runtime ? isReadOnly(runtime) : true;
  const [title, setTitle] = useState('');
  const [content, setContent] = useState('');
  const [visibility, setVisibility] = useState<TextVisibility>('private');
  const [expiresAt, setExpiresAt] = useState('');
  const [oneTime, setOneTime] = useState(false);
  const [password, setPassword] = useState('');
  const [clearPassword, setClearPassword] = useState(false);
  const [changeReason, setChangeReason] = useState('Update Text share');

  const detailQuery = useQuery({
    queryKey: ['text-share', runtime?.workspaceId, numericId],
    enabled: client !== null && runtime !== null && Number.isSafeInteger(numericId) && numericId > 0,
    queryFn: () => client!.get(runtime!.workspaceId, numericId),
    retry: (count, error) => !(error instanceof GoJetApiError && (error.status === 404 || error.status === 410)) && count < 1,
  });
  const item = detailQuery.data;
  useEffect(() => {
    if (!item) return;
    setTitle(item.title); setContent(item.content); setVisibility(item.visibility); setExpiresAt(toLocalDateTime(item.expires_at)); setOneTime(item.one_time); setPassword(''); setClearPassword(false);
  }, [item?.id, item?.version]);

  const updateMutation = useMutation({
    mutationFn: () => {
      if (!runtime || !client || !item) throw new Error('Text authority is unavailable.');
      return client.update(runtime.workspaceId, numericId, {
        expected_version: item.version, title: title.trim(), content, visibility,
        password: password || undefined, clear_password: clearPassword || undefined,
        expires_at: expiresAt ? toRFC3339(expiresAt) : undefined, clear_expires_at: !expiresAt && Boolean(item.expires_at) ? true : undefined,
        one_time: oneTime, change_reason: changeReason.trim(),
      });
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['text-share', runtime?.workspaceId, numericId] });
      await queryClient.invalidateQueries({ queryKey: ['text-shares', runtime?.workspaceId] });
    },
  });
  const deleteMutation = useMutation({
    mutationFn: () => client!.remove(runtime!.workspaceId, numericId, item!.version, changeReason.trim()),
    onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: ['text-shares', runtime?.workspaceId] }); await navigate({ to: '/app/text' }); },
  });

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!readOnly && item && title.trim() && content.trim() && changeReason.trim()) updateMutation.mutate();
  }

  const detailError = detailQuery.error instanceof GoJetApiError ? detailQuery.error : null;
  const deleted = detailError?.status === 410 || detailError?.code === 'deleted';
  const conflict = updateMutation.error instanceof GoJetApiError && updateMutation.error.status === 409;
  const expired = item?.expires_at ? new Date(item.expires_at).valueOf() <= Date.now() : false;
  const pageState = deleted ? 'deleted' : conflict ? 'conflict' : expired ? 'expired' : readOnly && item ? 'read-only' : item ? 'edit' : detailQuery.isPending ? 'loading' : 'error';
  const actionError = updateMutation.error ?? deleteMutation.error;
  const actionApiError = actionError instanceof GoJetApiError ? actionError : null;

  return (
    <WorkspaceShell state={readOnly && runtime ? 'read-only-role' : 'notification-attention'} sectionLabel="Text detail">
      <section className="text-page" data-page="text-detail" data-state={pageState}>
        <header className="text-page-header"><div><p className="text-eyebrow">Text</p><h1>{item?.title ?? 'Text detail'}</h1><p>Workspace editing, public visibility and lifecycle remain server-authoritative.</p></div><Link to="/app/text" className="text-secondary-link">Back to Text</Link></header>
        {!runtime ? <InlineMessage variant="warning">Workspace identity authority is unavailable in this build.</InlineMessage> : null}
        {detailQuery.isPending && runtime ? <p role="status">Loading Text resource…</p> : null}
        {deleted ? <InlineMessage variant="warning">This Text share has been removed and cannot be restored by a stale write.</InlineMessage> : null}
        {detailQuery.isError && !deleted ? <InlineMessage variant="danger">The Text resource could not be loaded from its authoritative Workspace.</InlineMessage> : null}
        {expired ? <InlineMessage variant="warning">This Text share is expired. Public access returns HTTP 410.</InlineMessage> : null}
        {item?.consumed_at ? <InlineMessage variant="warning">This one-time Text share has been consumed. Further public access returns HTTP 410.</InlineMessage> : null}
        {conflict ? <InlineMessage variant="warning">The resource changed after this editor loaded. Reload the current server version before saving again.</InlineMessage> : null}
        {actionApiError && !conflict ? <InlineMessage variant="danger">{actionApiError.message} <strong>{actionApiError.code}</strong></InlineMessage> : null}
        {actionError && !actionApiError ? <InlineMessage variant="danger">The Text action could not be completed.</InlineMessage> : null}

        {item ? <div className="text-detail-grid">
          <Card as="section" className="text-editor-card">
            <div className="text-card-head"><div><p className="text-kicker">Resource authority</p><h2>Edit Text share</h2></div><strong className="text-status">v{item.version}</strong></div>
            <form className="text-form" onSubmit={submit}>
              <TextField id="text-detail-title" label="Title" required disabled={readOnly} value={title} onChange={(event) => setTitle(event.currentTarget.value)} />
              <label className="text-native-field" htmlFor="text-detail-content">Content<textarea id="text-detail-content" rows={12} required readOnly={readOnly} value={content} onChange={(event) => setContent(event.currentTarget.value)} /></label>
              <div className="text-form-grid">
                <label className="text-native-field" htmlFor="text-detail-visibility">Visibility<select id="text-detail-visibility" disabled={readOnly} value={visibility} onChange={(event) => setVisibility(event.currentTarget.value as TextVisibility)}><option value="private">Private</option><option value="public">Public</option></select></label>
                <TextField id="text-detail-password" label={item.password_required ? 'Replace password (optional)' : 'Password (optional)'} type="password" disabled={readOnly || clearPassword} value={password} onChange={(event) => setPassword(event.currentTarget.value)} />
                <label className="text-native-field" htmlFor="text-detail-expiry">Expires at (optional)<input id="text-detail-expiry" type="datetime-local" disabled={readOnly} value={expiresAt} onChange={(event) => setExpiresAt(event.currentTarget.value)} /></label>
              </div>
              {item.password_required ? <label className="text-check"><input id="text-detail-clear-password" type="checkbox" disabled={readOnly} checked={clearPassword} onChange={(event) => { setClearPassword(event.currentTarget.checked); if (event.currentTarget.checked) setPassword(''); }} /><span>Remove password protection</span></label> : null}
              <label className="text-check"><input id="text-detail-one-time" type="checkbox" disabled={readOnly} checked={oneTime} onChange={(event) => setOneTime(event.currentTarget.checked)} /><span>Consume once after first authorized reveal or download</span></label>
              <TextField id="text-detail-reason" label="Change reason" required disabled={readOnly} value={changeReason} onChange={(event) => setChangeReason(event.currentTarget.value)} />
              <div className="text-actions"><Button type="submit" loading={updateMutation.isPending} disabled={readOnly || !title.trim() || !content.trim() || !changeReason.trim()}>Save current version</Button>{item.visibility === 'public' && !expired && !item.consumed_at ? <a className="text-secondary-link" href={`/t/${encodeURIComponent(item.public_slug)}`}>Preview public state</a> : null}</div>
            </form>
          </Card>
          <div className="text-detail-side">
            <Card as="section" className="text-detail-card"><p className="text-kicker">Lifecycle</p><h2>Current policy</h2><dl className="text-facts"><div><dt>Visibility</dt><dd>{item.visibility}</dd></div><div><dt>Password</dt><dd>{item.password_required ? 'Required' : 'None'}</dd></div><div><dt>One-time</dt><dd>{item.one_time ? 'Yes' : 'No'}</dd></div><div><dt>Consumed</dt><dd>{item.consumed_at ? 'Yes' : 'No'}</dd></div><div><dt>Expires</dt><dd>{item.expires_at ? new Date(item.expires_at).toLocaleString() : 'Never'}</dd></div><div><dt>Updated</dt><dd>{new Date(item.updated_at).toLocaleString()}</dd></div></dl><p className="text-public-path">Public path: <code>/t/{item.public_slug}</code></p><p>Public Text remains noindex and is never eligible for the Website or Docs sitemap.</p></Card>
            <Card as="section" className="text-detail-card text-danger"><p className="text-kicker">Removal</p><h2>Delete Text share</h2><p>Removal is durable. Public access becomes HTTP 410 and stale writes cannot restore the resource.</p><Button variant="destructive" loading={deleteMutation.isPending} disabled={readOnly || !changeReason.trim()} onClick={() => deleteMutation.mutate()}>Delete Text share</Button></Card>
          </div>
        </div> : null}
      </section>
    </WorkspaceShell>
  );
}
