import { type FormEvent, useMemo, useState } from 'react';
import { Link, useNavigate } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { GoJetApiError, type TextCreateInput, type TextVisibility } from '@gojet/api-client';
import { Button, Card, EmptyState, InlineMessage, TextField } from '@gojet/ui';
import { WorkspaceShell } from '../shell/WorkspaceShell';
import { isReadOnly } from '../links/runtime';
import { createWorkspaceTextClient, readTextRuntime } from '../text/runtime';

function toRFC3339(value: string): string | null {
  if (!value) return null;
  const parsed = new Date(value);
  return Number.isNaN(parsed.valueOf()) ? null : parsed.toISOString();
}

export default function TextListPage() {
  const runtime = useMemo(() => readTextRuntime(), []);
  const client = useMemo(() => runtime ? createWorkspaceTextClient(runtime) : null, [runtime]);
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const readOnly = runtime ? isReadOnly(runtime) : true;
  const [title, setTitle] = useState('');
  const [content, setContent] = useState('');
  const [visibility, setVisibility] = useState<TextVisibility>('private');
  const [password, setPassword] = useState('');
  const [expiresAt, setExpiresAt] = useState('');
  const [oneTime, setOneTime] = useState(false);
  const [changeReason, setChangeReason] = useState('Create Text share');

  const listQuery = useQuery({
    queryKey: ['text-shares', runtime?.workspaceId],
    enabled: client !== null && runtime !== null,
    queryFn: () => client!.list(runtime!.workspaceId),
  });
  const createMutation = useMutation({
    mutationFn: () => {
      if (!runtime || !client) throw new Error('Workspace Text authority is unavailable.');
      const input: TextCreateInput = { title: title.trim(), content, visibility, one_time: oneTime, change_reason: changeReason.trim() };
      if (password) input.password = password;
      const expiry = toRFC3339(expiresAt);
      if (expiry) input.expires_at = expiry;
      return client.create(runtime.workspaceId, input);
    },
    onSuccess: async (created) => {
      await queryClient.invalidateQueries({ queryKey: ['text-shares', runtime?.workspaceId] });
      await navigate({ to: '/app/text/$shareId', params: { shareId: String(created.id) } });
    },
  });

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!readOnly && title.trim() && content.trim() && changeReason.trim()) createMutation.mutate();
  }

  const items = listQuery.data?.items ?? [];
  const apiError = createMutation.error instanceof GoJetApiError ? createMutation.error : null;
  const quotaReached = listQuery.data?.quota.reached === true || apiError?.code === 'quota_reached' || apiError?.status === 429;
  const pageState = createMutation.isPending ? 'edit' : quotaReached ? 'quota-reached' : listQuery.isPending ? 'loading' : listQuery.isError ? 'error' : readOnly ? 'read-only' : items.length === 0 ? 'empty' : 'edit';

  return (
    <WorkspaceShell state={readOnly && runtime ? 'read-only-role' : 'notification-attention'} sectionLabel="Text">
      <section className="text-page" data-page="text-list" data-state={pageState}>
        <header className="text-page-header"><div><p className="text-eyebrow">Text</p><h1>Text sharing</h1><p>Create private or public text resources with server-authoritative access, expiry and one-time lifecycle controls.</p></div></header>
        {!runtime ? <InlineMessage variant="warning">Workspace identity authority is unavailable in this build.</InlineMessage> : null}
        {listQuery.isError ? <InlineMessage variant="danger">Text resources could not be loaded from the authoritative Workspace API.</InlineMessage> : null}
        {quotaReached ? <InlineMessage variant="warning">Workspace Text quota reached. Remove a resource before creating another.</InlineMessage> : null}
        {apiError && !quotaReached ? <InlineMessage variant={apiError.status === 409 ? 'warning' : 'danger'}>{apiError.message} <strong>{apiError.code}</strong></InlineMessage> : null}
        {createMutation.error && !apiError ? <InlineMessage variant="danger">The Text resource could not be created.</InlineMessage> : null}

        {!readOnly ? <Card as="section" className="text-editor-card">
          <div><p className="text-kicker">Workspace authority</p><h2>Create Text share</h2><p>Public visibility does not make this content indexable. All `/t/` resources remain noindex and outside sitemaps.</p></div>
          <form className="text-form" onSubmit={submit}>
            <TextField id="text-create-title" label="Title" required value={title} onChange={(event) => setTitle(event.currentTarget.value)} />
            <label className="text-native-field" htmlFor="text-create-content">Content<textarea id="text-create-content" required rows={8} value={content} onChange={(event) => setContent(event.currentTarget.value)} /></label>
            <div className="text-form-grid">
              <label className="text-native-field" htmlFor="text-create-visibility">Visibility<select id="text-create-visibility" value={visibility} onChange={(event) => setVisibility(event.currentTarget.value as TextVisibility)}><option value="private">Private</option><option value="public">Public</option></select></label>
              <TextField id="text-create-password" label="Password (optional)" type="password" value={password} onChange={(event) => setPassword(event.currentTarget.value)} />
              <label className="text-native-field" htmlFor="text-create-expiry">Expires at (optional)<input id="text-create-expiry" type="datetime-local" value={expiresAt} onChange={(event) => setExpiresAt(event.currentTarget.value)} /></label>
            </div>
            <label className="text-check"><input id="text-create-one-time" type="checkbox" checked={oneTime} onChange={(event) => setOneTime(event.currentTarget.checked)} /><span>Consume once after the first authorized reveal or download</span></label>
            <TextField id="text-create-reason" label="Change reason" required value={changeReason} onChange={(event) => setChangeReason(event.currentTarget.value)} />
            <Button type="submit" loading={createMutation.isPending} disabled={!title.trim() || !content.trim() || !changeReason.trim() || quotaReached}>Create Text share</Button>
          </form>
        </Card> : null}

        {listQuery.isPending && runtime ? <p role="status">Loading Text resources…</p> : null}
        {!listQuery.isPending && !listQuery.isError && items.length === 0 ? <EmptyState title="No Text shares yet" reason={readOnly ? 'No Text resources are available to your role.' : 'Create a Text share to begin.'} /> : null}
        {items.length > 0 ? <div className="text-list" aria-label="Workspace Text shares">{items.map((item) => {
          const expired = item.expires_at ? new Date(item.expires_at).valueOf() <= Date.now() : false;
          const lifecycle = item.consumed_at ? 'Consumed' : expired ? 'Expired' : item.visibility === 'public' ? 'Public' : 'Private';
          return <Card as="article" className="text-card" key={item.id} data-text-lifecycle={lifecycle.toLowerCase()}>
            <div className="text-card-head"><div><p className="text-kicker">Text #{item.id}</p><h2>{item.title}</h2></div><strong className="text-status">{lifecycle}</strong></div>
            <p className="text-preview">{item.content.slice(0, 180)}</p>
            <dl className="text-meta"><div><dt>Version</dt><dd>{item.version}</dd></div><div><dt>Password</dt><dd>{item.password_required ? 'Required' : 'None'}</dd></div><div><dt>One-time</dt><dd>{item.one_time ? 'Yes' : 'No'}</dd></div><div><dt>Updated</dt><dd>{new Date(item.updated_at).toLocaleString()}</dd></div></dl>
            <div className="text-actions"><Link to="/app/text/$shareId" params={{ shareId: String(item.id) }} className="text-primary-link">Open Text</Link>{item.visibility === 'public' && !item.consumed_at && !expired ? <a href={`/t/${encodeURIComponent(item.public_slug)}`} className="text-secondary-link">Open public page</a> : null}</div>
          </Card>;
        })}</div> : null}
      </section>
    </WorkspaceShell>
  );
}
