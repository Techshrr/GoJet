import { type FormEvent, useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { GoJetApiError, type BioChildInput, type BioChildRecord, type BioUpdateInput } from '@gojet/api-client';
import { Button, Card, InlineMessage, TextField } from '@gojet/ui';
import { WorkspaceShell } from '../shell/WorkspaceShell';
import { isReadOnly } from '../links/runtime';
import { createWorkspaceBioClient, readBioRuntime } from '../bio/runtime';

type DraftChild = BioChildInput;

function reindex(children: DraftChild[]): DraftChild[] {
  return children.map((child, position) => ({ ...child, position }));
}

function currentRisk(child: DraftChild, authority: BioChildRecord[]): BioChildRecord['risk_status'] {
  if (!child.id) return 'review';
  const current = authority.find((item) => item.id === child.id);
  if (!current || current.destination_url !== child.destination_url) return 'review';
  return current.risk_status;
}

export default function BioDetailPage() {
  const { pageId } = useParams({ from: '/app/bio/$pageId' });
  const numericId = Number(pageId);
  const runtime = useMemo(() => readBioRuntime(), []);
  const client = useMemo(() => runtime ? createWorkspaceBioClient(runtime) : null, [runtime]);
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const readOnly = runtime ? isReadOnly(runtime) : true;
  const [title, setTitle] = useState('');
  const [bio, setBio] = useState('');
  const [children, setChildren] = useState<DraftChild[]>([]);
  const [changeReason, setChangeReason] = useState('Update Bio page');

  const detailQuery = useQuery({
    queryKey: ['bio-page', runtime?.workspaceId, numericId],
    enabled: client !== null && runtime !== null && Number.isSafeInteger(numericId) && numericId > 0,
    queryFn: () => client!.get(runtime!.workspaceId, numericId),
    retry: (count, error) => !(error instanceof GoJetApiError && (error.status === 404 || error.status === 410)) && count < 1,
  });
  const item = detailQuery.data;

  useEffect(() => {
    if (!item) return;
    setTitle(item.title);
    setBio(item.bio);
    setChildren(item.links.map((child) => ({ id: child.id, position: child.position, label: child.label, destination_url: child.destination_url })));
  }, [item?.id, item?.version]);

  const canonicalDraft = useMemo(() => reindex(children), [children]);
  const dirty = item ? title.trim() !== item.title || bio.trim() !== item.bio || JSON.stringify(canonicalDraft) !== JSON.stringify(item.links.map((child) => ({ id: child.id, position: child.position, label: child.label, destination_url: child.destination_url }))) : false;
  const draftRisks = item ? canonicalDraft.map((child) => currentRisk(child, item.links)) : [];
  const unresolvedCount = draftRisks.filter((state) => state !== 'allowed').length;

  const updateMutation = useMutation({
    mutationFn: () => {
      if (!runtime || !client || !item) throw new Error('Bio authority is unavailable.');
      const input: BioUpdateInput = {
        expected_version: item.version,
        title: title.trim(),
        bio: bio.trim(),
        links: canonicalDraft.map((child) => ({ ...(child.id ? { id: child.id } : {}), position: child.position, label: child.label.trim(), destination_url: child.destination_url.trim() })),
        change_reason: changeReason.trim(),
      };
      return client.update(runtime.workspaceId, numericId, input);
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['bio-page', runtime?.workspaceId, numericId] });
      await queryClient.invalidateQueries({ queryKey: ['bio-pages', runtime?.workspaceId] });
    },
  });
  const publishMutation = useMutation({
    mutationFn: () => client!.publish(runtime!.workspaceId, numericId, item!.version, changeReason.trim()),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['bio-page', runtime?.workspaceId, numericId] });
      await queryClient.invalidateQueries({ queryKey: ['bio-pages', runtime?.workspaceId] });
    },
  });
  const pauseMutation = useMutation({
    mutationFn: () => client!.pause(runtime!.workspaceId, numericId, item!.version, changeReason.trim()),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['bio-page', runtime?.workspaceId, numericId] });
      await queryClient.invalidateQueries({ queryKey: ['bio-pages', runtime?.workspaceId] });
    },
  });
  const deleteMutation = useMutation({
    mutationFn: () => client!.remove(runtime!.workspaceId, numericId, item!.version, changeReason.trim()),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['bio-pages', runtime?.workspaceId] });
      await navigate({ to: '/app/bio' });
    },
  });

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const childrenValid = canonicalDraft.every((child) => child.label.trim() && child.destination_url.trim());
    if (!readOnly && item && title.trim() && changeReason.trim() && childrenValid) updateMutation.mutate();
  }

  function addChild() {
    setChildren((current) => reindex([...current, { position: current.length, label: '', destination_url: '' }]));
  }
  function updateChild(position: number, field: 'label' | 'destination_url', value: string) {
    setChildren((current) => reindex(current.map((child, index) => index === position ? { ...child, [field]: value } : child)));
  }
  function removeChild(position: number) {
    setChildren((current) => reindex(current.filter((_, index) => index !== position)));
  }
  function moveChild(position: number, direction: -1 | 1) {
    setChildren((current) => {
      const target = position + direction;
      if (target < 0 || target >= current.length) return current;
      const currentItem = current[position];
      const targetItem = current[target];
      if (!currentItem || !targetItem) return current;
      const next = [...current];
      next[position] = targetItem;
      next[target] = currentItem;
      return reindex(next);
    });
  }

  const detailError = detailQuery.error instanceof GoJetApiError ? detailQuery.error : null;
  const deleted = detailError?.status === 410 || detailError?.code === 'deleted';
  const actionError = updateMutation.error ?? publishMutation.error ?? pauseMutation.error ?? deleteMutation.error;
  const actionApiError = actionError instanceof GoJetApiError ? actionError : null;
  const conflict = actionApiError?.status === 409 && actionApiError.code === 'version_conflict';
  const publishBlocked = actionApiError?.code === 'child_link_risk_unresolved';
  const pageState = deleted ? 'deleted' : conflict ? 'conflict' : publishBlocked ? 'risk-blocked' : readOnly && item ? 'read-only' : item ? item.status : detailQuery.isPending ? 'loading' : 'error';
  const anyMutation = updateMutation.isPending || publishMutation.isPending || pauseMutation.isPending || deleteMutation.isPending;

  return (
    <WorkspaceShell state={readOnly && runtime ? 'read-only-role' : 'notification-attention'} sectionLabel="Bio detail">
      <section className="bio-page" data-page="bio-detail" data-state={pageState}>
        <header className="bio-page-header"><div><p className="bio-eyebrow">Bio</p><h1>{item?.title ?? 'Bio detail'}</h1><p>Edit profile content and ordered child links. Public navigation is derived from the current server risk authority, never from editor state alone.</p></div><Link to="/app/bio" className="bio-secondary-link">Back to Bio</Link></header>
        {!runtime ? <InlineMessage variant="warning">Workspace identity authority is unavailable in this build.</InlineMessage> : null}
        {detailQuery.isPending && runtime ? <p role="status">Loading Bio page…</p> : null}
        {deleted ? <InlineMessage variant="warning">This Bio page has been removed. Public page/API return HTTP 410 and stale writes cannot restore it.</InlineMessage> : null}
        {detailQuery.isError && !deleted ? <InlineMessage variant="danger">The Bio page could not be loaded from its authoritative Workspace.</InlineMessage> : null}
        {conflict ? <InlineMessage variant="warning">The Bio page changed after this editor loaded. Reload the current server version before saving or transitioning state.</InlineMessage> : null}
        {publishBlocked ? <InlineMessage variant="warning">Publishing failed closed because one or more child destinations do not have a current allow decision. Save/review the current destinations before publishing.</InlineMessage> : null}
        {actionApiError && !conflict && !publishBlocked ? <InlineMessage variant="danger">{actionApiError.message} <strong>{actionApiError.code}</strong></InlineMessage> : null}
        {actionError && !actionApiError ? <InlineMessage variant="danger">The Bio action could not be completed.</InlineMessage> : null}
        {item?.status === 'published' && item.links.some((child) => child.risk_status !== 'allowed') ? <InlineMessage variant="warning">This published Bio has child links in review/blocked safety state. The page remains public, but those child destinations are non-navigable.</InlineMessage> : null}

        {item ? <div className="bio-detail-grid">
          <Card as="section" className="bio-editor-card">
            <div className="bio-card-head"><div><p className="bio-kicker">Resource authority</p><h2>Edit Bio page</h2></div><strong className="bio-status">{item.status} · v{item.version}</strong></div>
            <form className="bio-form" onSubmit={submit}>
              <TextField id="bio-detail-title" label="Page title" required disabled={readOnly} value={title} onChange={(event) => setTitle(event.currentTarget.value)} />
              <label className="bio-native-field" htmlFor="bio-detail-copy">Profile text<textarea id="bio-detail-copy" rows={6} readOnly={readOnly} value={bio} onChange={(event) => setBio(event.currentTarget.value)} /></label>

              <section className="bio-links-editor" aria-labelledby="bio-links-heading">
                <div className="bio-links-heading"><div><p className="bio-kicker">Ordered child links</p><h3 id="bio-links-heading">Destinations</h3></div><Button type="button" variant="ghost" disabled={readOnly || children.length >= 100} onClick={addChild}>Add link</Button></div>
                {canonicalDraft.length === 0 ? <p className="bio-muted">No child links. A Bio page can be published without outbound navigation.</p> : null}
                {canonicalDraft.map((child, position) => {
                  const risk = currentRisk(child, item.links);
                  return <Card as="article" className="bio-child-editor" key={child.id ? `child-${child.id}` : `new-${position}`} data-risk-status={risk}>
                    <div className="bio-child-head"><strong>Link {position + 1}</strong><span className="bio-risk-badge">{risk}</span></div>
                    <TextField id={`bio-child-label-${position}`} label="Label" required disabled={readOnly} value={child.label} onChange={(event) => updateChild(position, 'label', event.currentTarget.value)} />
                    <TextField id={`bio-child-url-${position}`} label="Destination URL" required disabled={readOnly} value={child.destination_url} onChange={(event) => updateChild(position, 'destination_url', event.currentTarget.value)} />
                    <div className="bio-child-actions"><Button type="button" variant="ghost" disabled={readOnly || position === 0} onClick={() => moveChild(position, -1)}>Move up</Button><Button type="button" variant="ghost" disabled={readOnly || position === canonicalDraft.length - 1} onClick={() => moveChild(position, 1)}>Move down</Button><Button type="button" variant="ghost" disabled={readOnly} onClick={() => removeChild(position)}>Remove</Button></div>
                    {risk !== 'allowed' ? <p className="bio-risk-copy">This destination is not an active public navigation target. A changed/new destination returns to review until current risk authority allows it.</p> : null}
                  </Card>;
                })}
              </section>

              <TextField id="bio-detail-reason" label="Change reason" required disabled={readOnly} value={changeReason} onChange={(event) => setChangeReason(event.currentTarget.value)} />
              <div className="bio-actions"><Button type="submit" loading={updateMutation.isPending} disabled={readOnly || !dirty || !title.trim() || !changeReason.trim() || canonicalDraft.some((child) => !child.label.trim() || !child.destination_url.trim())}>Save current version</Button>{item.status !== 'draft' ? <a className="bio-secondary-link" href={`/p/${encodeURIComponent(item.slug)}`}>Open public state</a> : null}</div>
            </form>
          </Card>

          <div className="bio-detail-side">
            <Card as="section" className="bio-detail-card">
              <p className="bio-kicker">Public preview</p><h2>{title.trim() || 'Untitled Bio'}</h2>{bio.trim() ? <p className="bio-profile-copy">{bio}</p> : null}
              {canonicalDraft.length > 0 ? <ol className="bio-preview-links">{canonicalDraft.map((child, position) => {
                const risk = currentRisk(child, item.links);
                return <li key={child.id ? `preview-${child.id}` : `preview-new-${position}`}>{risk === 'allowed' && item.status === 'published' && !dirty ? <a href={child.destination_url} rel="ugc nofollow">{child.label || `Link ${position + 1}`}</a> : <span data-risk-status={risk}>{child.label || `Link ${position + 1}`} · {item.status === 'paused' ? 'paused' : risk}</span>}</li>;
              })}</ol> : <p className="bio-muted">No outbound links.</p>}
              <p className="bio-public-path">Public path: <code>/p/{item.slug}</code></p><p>Bio is permanently noindex, has no canonical/hreflang/structured-data authority, and is excluded from sitemaps.</p>
            </Card>

            <Card as="section" className="bio-detail-card">
              <p className="bio-kicker">Publication</p><h2>Lifecycle authority</h2>
              <dl className="bio-facts"><div><dt>Status</dt><dd>{item.status}</dd></div><div><dt>Current links</dt><dd>{item.links.length}</dd></div><div><dt>Needs review</dt><dd>{unresolvedCount}</dd></div><div><dt>Published</dt><dd>{item.published_at ? new Date(item.published_at).toLocaleString() : 'Never'}</dd></div><div><dt>Updated</dt><dd>{new Date(item.updated_at).toLocaleString()}</dd></div></dl>
              {dirty ? <InlineMessage variant="info">Save editor changes before changing publication state.</InlineMessage> : null}
              <div className="bio-actions">{item.status !== 'published' ? <Button loading={publishMutation.isPending} disabled={readOnly || dirty || unresolvedCount > 0 || !changeReason.trim() || anyMutation} onClick={() => publishMutation.mutate()}>Publish Bio</Button> : <Button loading={pauseMutation.isPending} disabled={readOnly || dirty || !changeReason.trim() || anyMutation} onClick={() => pauseMutation.mutate()}>Pause Bio</Button>}</div>
              {item.status !== 'published' && unresolvedCount > 0 ? <p className="bio-risk-copy">Initial publish is fail-closed until every stored child has a current allowed decision. A previously published page may later remain public with review/blocked children rendered non-navigable.</p> : null}
            </Card>

            <Card as="section" className="bio-detail-card bio-danger"><p className="bio-kicker">Removal</p><h2>Delete Bio page</h2><p>Removal is durable. Public page/API become HTTP 410 and stale versions cannot restore authority.</p><Button variant="destructive" loading={deleteMutation.isPending} disabled={readOnly || dirty || !changeReason.trim() || anyMutation} onClick={() => deleteMutation.mutate()}>Delete Bio page</Button></Card>
          </div>
        </div> : null}
      </section>
    </WorkspaceShell>
  );
}
