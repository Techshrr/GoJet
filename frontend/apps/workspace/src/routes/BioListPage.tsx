import { type FormEvent, useMemo, useState } from 'react';
import { Link, useNavigate } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { GoJetApiError, type BioCreateInput } from '@gojet/api-client';
import { Button, Card, EmptyState, InlineMessage, TextField } from '@gojet/ui';
import { WorkspaceShell } from '../shell/WorkspaceShell';
import { isReadOnly } from '../links/runtime';
import { createWorkspaceBioClient, readBioRuntime } from '../bio/runtime';

export default function BioListPage() {
  const runtime = useMemo(() => readBioRuntime(), []);
  const client = useMemo(() => runtime ? createWorkspaceBioClient(runtime) : null, [runtime]);
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const readOnly = runtime ? isReadOnly(runtime) : true;
  const [title, setTitle] = useState('');
  const [bio, setBio] = useState('');
  const [changeReason, setChangeReason] = useState('Create Bio page');

  const listQuery = useQuery({
    queryKey: ['bio-pages', runtime?.workspaceId],
    enabled: client !== null && runtime !== null,
    queryFn: () => client!.list(runtime!.workspaceId),
  });
  const createMutation = useMutation({
    mutationFn: () => {
      if (!runtime || !client) throw new Error('Workspace Bio authority is unavailable.');
      const input: BioCreateInput = { title: title.trim(), bio: bio.trim(), links: [], change_reason: changeReason.trim() };
      return client.create(runtime.workspaceId, input);
    },
    onSuccess: async (created) => {
      await queryClient.invalidateQueries({ queryKey: ['bio-pages', runtime?.workspaceId] });
      await navigate({ to: '/app/bio/$pageId', params: { pageId: String(created.id) } });
    },
  });

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!readOnly && title.trim() && changeReason.trim()) createMutation.mutate();
  }

  const items = listQuery.data?.items ?? [];
  const apiError = createMutation.error instanceof GoJetApiError ? createMutation.error : null;
  const quotaReached = listQuery.data?.quota.reached === true || apiError?.code === 'quota_reached' || apiError?.status === 429;
  const pageState = createMutation.isPending ? 'edit' : quotaReached ? 'quota-reached' : listQuery.isPending ? 'loading' : listQuery.isError ? 'error' : readOnly ? 'read-only' : items.length === 0 ? 'empty' : 'edit';

  return (
    <WorkspaceShell state={readOnly && runtime ? 'read-only-role' : 'notification-attention'} sectionLabel="Bio">
      <section className="bio-page" data-page="bio-list" data-state={pageState}>
        <header className="bio-page-header"><div><p className="bio-eyebrow">Bio</p><h1>Bio pages</h1><p>Create Workspace-owned profile pages, manage ordered child links, and publish only through server-authoritative destination safety.</p></div></header>
        {!runtime ? <InlineMessage variant="warning">Workspace identity authority is unavailable in this build.</InlineMessage> : null}
        {listQuery.isError ? <InlineMessage variant="danger">Bio pages could not be loaded from the authoritative Workspace API.</InlineMessage> : null}
        {quotaReached ? <InlineMessage variant="warning">Workspace Bio quota reached. Remove a page before creating another.</InlineMessage> : null}
        {apiError && !quotaReached ? <InlineMessage variant={apiError.status === 409 ? 'warning' : 'danger'}>{apiError.message} <strong>{apiError.code}</strong></InlineMessage> : null}
        {createMutation.error && !apiError ? <InlineMessage variant="danger">The Bio page could not be created.</InlineMessage> : null}

        {!readOnly ? <Card as="section" className="bio-editor-card">
          <div><p className="bio-kicker">Workspace authority</p><h2>Create Bio page</h2><p>Create the profile first, then add and review ordered child links in the detail editor. Bio remains permanently noindex and outside sitemaps.</p></div>
          <form className="bio-form" onSubmit={submit}>
            <TextField id="bio-create-title" label="Page title" required value={title} onChange={(event) => setTitle(event.currentTarget.value)} />
            <label className="bio-native-field" htmlFor="bio-create-copy">Profile text<textarea id="bio-create-copy" rows={5} value={bio} onChange={(event) => setBio(event.currentTarget.value)} /></label>
            <TextField id="bio-create-reason" label="Change reason" required value={changeReason} onChange={(event) => setChangeReason(event.currentTarget.value)} />
            <Button type="submit" loading={createMutation.isPending} disabled={!title.trim() || !changeReason.trim() || quotaReached}>Create Bio page</Button>
          </form>
        </Card> : null}

        {listQuery.isPending && runtime ? <p role="status">Loading Bio pages…</p> : null}
        {!listQuery.isPending && !listQuery.isError && items.length === 0 ? <EmptyState title="No Bio pages yet" reason={readOnly ? 'No Bio pages are available to your role.' : 'Create a Bio page to begin.'} /> : null}
        {items.length > 0 ? <div className="bio-list" aria-label="Workspace Bio pages">{items.map((item) => {
          const unresolved = item.links.filter((child) => child.risk_status !== 'allowed').length;
          return <Card as="article" className="bio-card" key={item.id} data-bio-lifecycle={item.status}>
            <div className="bio-card-head"><div><p className="bio-kicker">Bio #{item.id}</p><h2>{item.title}</h2></div><strong className="bio-status">{item.status}</strong></div>
            {item.bio ? <p className="bio-preview-copy">{item.bio.slice(0, 180)}</p> : <p className="bio-preview-copy">No profile text.</p>}
            <dl className="bio-meta"><div><dt>Version</dt><dd>{item.version}</dd></div><div><dt>Links</dt><dd>{item.links.length}</dd></div><div><dt>Needs review</dt><dd>{unresolved}</dd></div><div><dt>Updated</dt><dd>{new Date(item.updated_at).toLocaleString()}</dd></div></dl>
            <div className="bio-actions"><Link to="/app/bio/$pageId" params={{ pageId: String(item.id) }} className="bio-primary-link">Open Bio</Link>{item.status !== 'draft' ? <a href={`/p/${encodeURIComponent(item.slug)}`} className="bio-secondary-link">Open public state</a> : null}</div>
          </Card>;
        })}</div> : null}
      </section>
    </WorkspaceShell>
  );
}
