import type { FormEvent } from 'react';
import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Card, EmptyState, InlineMessage, TextField } from '@gojet/ui';
import type { WorkspaceCampaign, WorkspaceRole } from '@gojet/api-client';
import { P12Shell, useP12Authority } from './authority';
import { createP12Client, readP12Runtime } from './runtime';

function message(error: unknown): string { return error instanceof Error ? error.message : 'The request could not be completed.'; }
function canManageWorkspace(role: WorkspaceRole | undefined): boolean { return role === 'owner' || role === 'admin'; }
function canOrganize(role: WorkspaceRole | undefined): boolean { return role === 'owner' || role === 'admin' || role === 'member'; }

export function WorkspaceOverviewPage() {
  const authority = useP12Authority();
  const data = authority.overview.data;
  const pageState = authority.overview.isPending ? 'loading' : authority.overview.isError ? 'error' : data?.notification_state.status ?? 'complete';
  return (
    <P12Shell sectionLabel="Overview">
      <section className="p12-page" data-page="workspace-overview" data-state={pageState}>
        <header className="p12-page-header"><p className="p12-eyebrow">Workspace authority</p><h1>Workspace overview</h1><p>Server-authoritative membership, organization and notification state for the active Workspace.</p></header>
        {authority.overview.isError ? <InlineMessage variant="danger">Workspace overview is unavailable from the native API.</InlineMessage> : null}
        {authority.overview.isPending ? <p role="status">Loading Workspace authority…</p> : null}
        {data ? <>
          {data.notification_state.status !== 'complete' ? <InlineMessage variant="warning">Notification data is {data.notification_state.status}: {data.notification_state.state_reason}</InlineMessage> : null}
          <div className="p12-metric-grid" aria-label="Workspace overview counts">
            <Card><span>Members</span><strong>{data.counts.members}</strong></Card>
            <Card><span>Campaigns</span><strong>{data.counts.campaigns}</strong></Card>
            <Card><span>Tags</span><strong>{data.counts.tags}</strong></Card>
            <Card><span>Folders</span><strong>{data.counts.folders}</strong></Card>
            <Card><span>Unread</span><strong>{data.counts.unread_notifications}</strong></Card>
          </div>
          <Card as="section"><h2>{data.workspace.name}</h2><dl className="p12-definition-list"><div><dt>Role</dt><dd>{data.membership.role}</dd></div><div><dt>Status</dt><dd>{data.workspace.status}</dd></div><div><dt>Version</dt><dd>{data.workspace.version}</dd></div><div><dt>Workspace ID</dt><dd><code>{data.workspace.id}</code></dd></div></dl></Card>
        </> : null}
      </section>
    </P12Shell>
  );
}

export function WorkspaceSettingsPage() {
  const authority = useP12Authority();
  const queryClient = useQueryClient();
  const context = authority.overview.data;
  const [name, setName] = useState('');
  const [reason, setReason] = useState('Workspace settings update');
  useEffect(() => { if (context?.workspace.name) setName(context.workspace.name); }, [context?.workspace.name]);
  const allowed = canManageWorkspace(context?.membership.role);
  const mutation = useMutation({
    mutationFn: () => authority.client!.updateWorkspace(authority.workspaceId, name.trim(), context!.workspace.version, reason.trim()),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['p12-overview', authority.workspaceId] });
      await queryClient.invalidateQueries({ queryKey: ['p12-workspaces'] });
    },
  });
  const state = authority.overview.isError ? 'error' : mutation.isPending ? 'saving' : authority.overview.isPending ? 'loading' : allowed ? 'edit' : 'read-only';
  return (
    <P12Shell sectionLabel="Workspace settings">
      <section className="p12-page" data-page="workspace-settings" data-state={state}>
        <header className="p12-page-header"><p className="p12-eyebrow">Workspace settings</p><h1>Workspace identity</h1><p>Rename this Workspace using optimistic versioning. Profile, security, sessions and connected accounts remain P15-owned.</p></header>
        {!authority.runtime ? <InlineMessage variant="warning">Workspace identity authority is unavailable in this build.</InlineMessage> : null}
        {mutation.isError ? <InlineMessage variant="danger">{message(mutation.error)}</InlineMessage> : null}
        {context ? <Card as="section"><form className="p12-form" onSubmit={(event) => { event.preventDefault(); if (allowed && name.trim() && reason.trim()) mutation.mutate(); }}>
          <TextField id="p12-workspace-name" label="Workspace name" value={name} onChange={(event) => setName(event.currentTarget.value)} disabled={!allowed} required />
          <TextField id="p12-workspace-reason" label="Change reason" value={reason} onChange={(event) => setReason(event.currentTarget.value)} disabled={!allowed} required />
          <Button type="submit" loading={mutation.isPending} disabled={!allowed || !name.trim() || !reason.trim()}>Save Workspace settings</Button>
        </form></Card> : <p role="status">Loading Workspace settings…</p>}
      </section>
    </P12Shell>
  );
}

export function MembersPage() {
  const authority = useP12Authority();
  const queryClient = useQueryClient();
  const role = authority.overview.data?.membership.role;
  const allowed = canManageWorkspace(role);
  const members = useQuery({ queryKey: ['p12-members', authority.workspaceId], enabled: !!authority.client && !!authority.workspaceId, queryFn: () => authority.client!.members(authority.workspaceId) });
  const [email, setEmail] = useState('');
  const [inviteRole, setInviteRole] = useState<Exclude<WorkspaceRole, 'owner'>>('member');
  const inviteMutation = useMutation({
    mutationFn: () => authority.client!.invite(authority.workspaceId, email.trim().toLowerCase(), inviteRole, new Date(Date.now() + 60 * 60_000).toISOString(), 'Workspace member invitation'),
    onSuccess: async () => { setEmail(''); await queryClient.invalidateQueries({ queryKey: ['p12-members', authority.workspaceId] }); },
  });
  const roleMutation = useMutation({
    mutationFn: ({ id, next }: { id: number; next: WorkspaceRole }) => authority.client!.updateMember(authority.workspaceId, id, next, 'Workspace role update'),
    onSuccess: async () => queryClient.invalidateQueries({ queryKey: ['p12-members', authority.workspaceId] }),
  });
  const removeMutation = useMutation({
    mutationFn: (id: number) => authority.client!.removeMember(authority.workspaceId, id, 'Workspace member removal'),
    onSuccess: async () => queryClient.invalidateQueries({ queryKey: ['p12-members', authority.workspaceId] }),
  });
  const state = members.isPending ? 'loading' : members.isError ? 'error' : allowed ? 'manage' : 'read-only';
  return (
    <P12Shell sectionLabel="Members">
      <section className="p12-page" data-page="workspace-members" data-state={state}>
        <header className="p12-page-header"><p className="p12-eyebrow">RBAC</p><h1>Members and invitations</h1><p>Roles are re-resolved from MySQL on every P12-owned request; client role claims are not authorization.</p></header>
        {members.isError ? <InlineMessage variant="danger">Members could not be loaded.</InlineMessage> : null}
        {inviteMutation.isError ? <InlineMessage variant="danger">{message(inviteMutation.error)}</InlineMessage> : null}
        {allowed ? <Card as="section"><h2>Invite member</h2><form className="p12-form p12-form-inline" onSubmit={(event: FormEvent<HTMLFormElement>) => { event.preventDefault(); if (email.trim()) inviteMutation.mutate(); }}>
          <TextField id="p12-invite-email" label="Email" type="email" value={email} onChange={(event) => setEmail(event.currentTarget.value)} required />
          <label>Role<select aria-label="Invitation role" value={inviteRole} onChange={(event) => setInviteRole(event.currentTarget.value as Exclude<WorkspaceRole, 'owner'>)}><option value="admin">Admin</option><option value="member">Member</option><option value="viewer">Viewer</option></select></label>
          <Button type="submit" loading={inviteMutation.isPending}>Create invitation</Button>
        </form>{inviteMutation.data ? <p className="p12-invite-link" role="status">Invitation link: <code>/invite/{inviteMutation.data.token}</code></p> : null}</Card> : null}
        {members.isPending ? <p role="status">Loading members…</p> : null}
        {members.data?.members.length ? <div className="p12-list" aria-label="Workspace members">{members.data.members.map((member) => {
          const mayManageMember = allowed && (role === 'owner' || member.role !== 'owner');
          return <Card as="article" key={member.id} data-member-role={member.role}><div className="p12-list-row"><div><h2>{member.display_name || member.email}</h2><p>{member.email}</p></div><strong>{member.role}</strong></div>{mayManageMember ? <div className="p12-actions"><label>Change role<select aria-label={`Role for ${member.email}`} value={member.role} onChange={(event) => roleMutation.mutate({ id: member.id, next: event.currentTarget.value as WorkspaceRole })}>{role === 'owner' ? <option value="owner">Owner</option> : null}<option value="admin">Admin</option><option value="member">Member</option><option value="viewer">Viewer</option></select></label><Button variant="ghost" onClick={() => removeMutation.mutate(member.id)}>Remove</Button></div> : null}</Card>;
        })}</div> : null}
        {members.data && members.data.members.length === 0 ? <EmptyState title="No members" reason="This Workspace has no visible memberships." /> : null}
        {allowed && members.data?.invitations.length ? <Card as="section"><h2>Invitations</h2><ul className="p12-plain-list">{members.data.invitations.map((item) => <li key={item.id}><span>{item.email} · {item.role} · {item.status}</span></li>)}</ul></Card> : null}
      </section>
    </P12Shell>
  );
}

export function OrganizationPage() {
  const authority = useP12Authority();
  const queryClient = useQueryClient();
  const role = authority.overview.data?.membership.role;
  const allowed = canManageWorkspace(role);
  const organization = useQuery({ queryKey: ['p12-organization', authority.workspaceId], enabled: !!authority.client && !!authority.workspaceId, queryFn: () => authority.client!.organization(authority.workspaceId) });
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  useEffect(() => { if (organization.data) { setName(organization.data.name); setDescription(organization.data.description); } }, [organization.data]);
  const mutation = useMutation({
    mutationFn: () => authority.client!.updateOrganization(authority.workspaceId, name.trim(), description.trim(), organization.data!.version),
    onSuccess: async () => queryClient.invalidateQueries({ queryKey: ['p12-organization', authority.workspaceId] }),
  });
  const state = organization.isPending ? 'loading' : organization.isError ? 'error' : allowed ? 'edit' : 'read-only';
  return <P12Shell sectionLabel="Organization"><section className="p12-page" data-page="workspace-organization" data-state={state}>
    <header className="p12-page-header"><p className="p12-eyebrow">Organization governance</p><h1>Organization</h1><p>Workspace-level organization metadata with optimistic concurrency.</p></header>
    {organization.isError ? <InlineMessage variant="danger">Organization metadata could not be loaded.</InlineMessage> : null}
    {mutation.isError ? <InlineMessage variant="danger">{message(mutation.error)}</InlineMessage> : null}
    {organization.data ? <Card as="section"><form className="p12-form" onSubmit={(event) => { event.preventDefault(); if (allowed && name.trim()) mutation.mutate(); }}><TextField id="p12-org-name" label="Organization name" value={name} onChange={(event) => setName(event.currentTarget.value)} disabled={!allowed} required /><label>Organization description<textarea aria-label="Organization description" rows={5} value={description} onChange={(event) => setDescription(event.currentTarget.value)} disabled={!allowed} /></label><Button type="submit" loading={mutation.isPending} disabled={!allowed || !name.trim()}>Save organization</Button></form></Card> : <p role="status">Loading organization…</p>}
  </section></P12Shell>;
}

export function CampaignsPage() {
  const authority = useP12Authority();
  const queryClient = useQueryClient();
  const allowed = canOrganize(authority.overview.data?.membership.role);
  const query = useQuery({ queryKey: ['p12-campaigns', authority.workspaceId], enabled: !!authority.client && !!authority.workspaceId, queryFn: () => authority.client!.campaigns(authority.workspaceId) });
  const [name, setName] = useState('');
  const create = useMutation({ mutationFn: () => authority.client!.createCampaign(authority.workspaceId, name.trim()), onSuccess: async () => { setName(''); await queryClient.invalidateQueries({ queryKey: ['p12-campaigns', authority.workspaceId] }); } });
  const update = useMutation({ mutationFn: ({ item, status }: { item: WorkspaceCampaign; status: WorkspaceCampaign['status'] }) => authority.client!.updateCampaign(authority.workspaceId, item.id, item.name, status, item.version), onSuccess: async () => queryClient.invalidateQueries({ queryKey: ['p12-campaigns', authority.workspaceId] }) });
  const remove = useMutation({ mutationFn: (id: string) => authority.client!.removeCampaign(authority.workspaceId, id), onSuccess: async () => queryClient.invalidateQueries({ queryKey: ['p12-campaigns', authority.workspaceId] }) });
  const state = query.isPending ? 'loading' : query.isError ? 'error' : allowed ? 'manage' : 'read-only';
  return <P12Shell sectionLabel="Campaigns"><section className="p12-page" data-page="workspace-campaigns" data-state={state}><header className="p12-page-header"><p className="p12-eyebrow">P07 continuity</p><h1>Campaigns</h1><p>Campaign IDs are the same namespace consumed by P07 analytics and conversions.</p></header>{query.isError ? <InlineMessage variant="danger">Campaigns could not be loaded.</InlineMessage> : null}{allowed ? <Card as="section"><form className="p12-form p12-form-inline" onSubmit={(event) => { event.preventDefault(); if (name.trim()) create.mutate(); }}><TextField id="p12-campaign-name" label="Campaign name" value={name} onChange={(event) => setName(event.currentTarget.value)} required /><Button type="submit" loading={create.isPending}>Create campaign</Button></form></Card> : null}<div className="p12-list" aria-label="Workspace campaigns">{query.data?.items.map((item) => <Card as="article" key={item.id}><div className="p12-list-row"><div><h2>{item.name}</h2><code>{item.id}</code></div><strong>{item.status}</strong></div>{allowed ? <div className="p12-actions"><Button variant="ghost" onClick={() => update.mutate({ item, status: item.status === 'active' ? 'archived' : 'active' })}>{item.status === 'active' ? 'Archive' : 'Activate'}</Button><Button variant="ghost" onClick={() => remove.mutate(item.id)}>Delete</Button></div> : null}</Card>)}</div>{query.data?.items.length === 0 ? <EmptyState title="No campaigns" reason="Create a campaign to organize Links and preserve analytics continuity." /> : null}</section></P12Shell>;
}

export function TagsPage() {
  const authority = useP12Authority();
  const queryClient = useQueryClient();
  const allowed = canOrganize(authority.overview.data?.membership.role);
  const tags = useQuery({ queryKey: ['p12-tags', authority.workspaceId], enabled: !!authority.client && !!authority.workspaceId, queryFn: () => authority.client!.tags(authority.workspaceId) });
  const folders = useQuery({ queryKey: ['p12-folders', authority.workspaceId], enabled: !!authority.client && !!authority.workspaceId, queryFn: () => authority.client!.folders(authority.workspaceId) });
  const [tagName, setTagName] = useState('');
  const [folderName, setFolderName] = useState('');
  const createTag = useMutation({ mutationFn: () => authority.client!.createTag(authority.workspaceId, tagName.trim()), onSuccess: async () => { setTagName(''); await queryClient.invalidateQueries({ queryKey: ['p12-tags', authority.workspaceId] }); } });
  const createFolder = useMutation({ mutationFn: () => authority.client!.createFolder(authority.workspaceId, folderName.trim()), onSuccess: async () => { setFolderName(''); await queryClient.invalidateQueries({ queryKey: ['p12-folders', authority.workspaceId] }); } });
  const removeTag = useMutation({ mutationFn: (id: number) => authority.client!.removeTag(authority.workspaceId, id), onSuccess: async () => queryClient.invalidateQueries({ queryKey: ['p12-tags', authority.workspaceId] }) });
  const removeFolder = useMutation({ mutationFn: (id: number) => authority.client!.removeFolder(authority.workspaceId, id), onSuccess: async () => queryClient.invalidateQueries({ queryKey: ['p12-folders', authority.workspaceId] }) });
  const state = tags.isPending || folders.isPending ? 'loading' : tags.isError || folders.isError ? 'error' : allowed ? 'manage' : 'read-only';
  return <P12Shell sectionLabel="Tags"><section className="p12-page" data-page="workspace-tags" data-state={state}><header className="p12-page-header"><p className="p12-eyebrow">Resource organization</p><h1>Tags and folders</h1><p>Folders remain resource-internal organization data. There is intentionally no <code>/app/folders</code> route.</p></header>{tags.isError || folders.isError ? <InlineMessage variant="danger">Tag/folder authority could not be loaded.</InlineMessage> : null}<div className="p12-two-column"><Card as="section"><h2>Tags</h2>{allowed ? <form className="p12-form" onSubmit={(event) => { event.preventDefault(); if (tagName.trim()) createTag.mutate(); }}><TextField id="p12-tag-name" label="Tag name" value={tagName} onChange={(event) => setTagName(event.currentTarget.value)} required /><Button type="submit" loading={createTag.isPending}>Create tag</Button></form> : null}<ul className="p12-plain-list">{tags.data?.items.map((item) => <li key={item.id}><span>{item.name}</span>{allowed ? <Button variant="ghost" onClick={() => removeTag.mutate(item.id)}>Delete tag</Button> : null}</li>)}</ul></Card><Card as="section"><h2>Folders</h2>{allowed ? <form className="p12-form" onSubmit={(event) => { event.preventDefault(); if (folderName.trim()) createFolder.mutate(); }}><TextField id="p12-folder-name" label="Folder name" value={folderName} onChange={(event) => setFolderName(event.currentTarget.value)} required /><Button type="submit" loading={createFolder.isPending}>Create folder</Button></form> : null}<ul className="p12-plain-list">{folders.data?.items.map((item) => <li key={item.id}><span>{item.name}</span>{allowed ? <Button variant="ghost" onClick={() => removeFolder.mutate(item.id)}>Delete folder</Button> : null}</li>)}</ul></Card></div></section></P12Shell>;
}

export function NotificationsPage() {
  const authority = useP12Authority();
  const queryClient = useQueryClient();
  const query = useQuery({ queryKey: ['p12-notifications-page', authority.workspaceId], enabled: !!authority.client && !!authority.workspaceId, queryFn: () => authority.client!.notifications(authority.workspaceId) });
  const mark = useMutation({ mutationFn: ({ id, read }: { id: number; read: boolean }) => authority.client!.markNotification(authority.workspaceId, id, read), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: ['p12-notifications-page', authority.workspaceId] }); await queryClient.invalidateQueries({ queryKey: ['p12-notifications', authority.workspaceId] }); await queryClient.invalidateQueries({ queryKey: ['p12-overview', authority.workspaceId] }); } });
  const all = useMutation({ mutationFn: () => authority.client!.markAllNotificationsRead(authority.workspaceId), onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: ['p12-notifications-page', authority.workspaceId] }); await queryClient.invalidateQueries({ queryKey: ['p12-notifications', authority.workspaceId] }); await queryClient.invalidateQueries({ queryKey: ['p12-overview', authority.workspaceId] }); } });
  const state = query.isPending ? 'loading' : query.isError ? 'error' : query.data?.state.status ?? 'complete';
  return <P12Shell sectionLabel="Notifications"><section className="p12-page" data-page="workspace-notifications" data-state={state}><header className="p12-page-header"><p className="p12-eyebrow">Notification core</p><h1>Notifications</h1><p>Recipient-scoped read state, dedupe and deep-link authorization are server-authoritative.</p></header>{query.isError ? <InlineMessage variant="danger">Notifications could not be loaded from the Workspace API.</InlineMessage> : null}{query.data && query.data.state.status !== 'complete' ? <InlineMessage variant="warning">Data is {query.data.state.status}: {query.data.state.state_reason}</InlineMessage> : null}{query.data ? <div className="p12-page-actions"><strong>{query.data.unread_count} unread</strong><Button onClick={() => all.mutate()} disabled={query.data.unread_count === 0} loading={all.isPending}>Mark all read</Button></div> : null}<div className="p12-list" aria-label="Workspace notifications">{query.data?.items.map((item) => <Card as="article" key={item.id} className={item.read_at ? 'p12-notification-read' : ''}><div className="p12-list-row"><div><p className="p12-eyebrow">{item.category}</p><h2>{item.title}</h2><p>{item.summary}</p></div><strong>{item.read_at ? 'Read' : 'Unread'}</strong></div><div className="p12-actions">{item.deep_link ? <a href={item.deep_link}>Open</a> : null}<Button variant="ghost" onClick={() => mark.mutate({ id: item.id, read: !item.read_at })}>Mark {item.read_at ? 'unread' : 'read'}</Button></div></Card>)}</div>{query.data?.items.length === 0 ? <EmptyState title="No notifications" reason="There is no Workspace activity for this recipient." /> : null}</section></P12Shell>;
}

export function InvitationPage() {
  const runtime = useMemo(() => readP12Runtime(), []);
  const client = useMemo(() => runtime ? createP12Client(runtime) : null, [runtime]);
  const token = useMemo(() => decodeURIComponent(window.location.pathname.split('/').filter(Boolean).pop() ?? ''), []);
  const query = useQuery({ queryKey: ['p12-invitation', token, runtime?.email], enabled: !!client && !!token, queryFn: () => client!.inspectInvitation(token) });
  const accept = useMutation({ mutationFn: () => client!.acceptInvitation(token) });
  const reject = useMutation({ mutationFn: () => client!.rejectInvitation(token) });
  const state = !runtime ? 'authentication-required' : query.isPending ? 'loading' : query.isError ? 'error' : accept.isSuccess ? 'accepted' : reject.isSuccess ? 'rejected' : query.data?.status ?? 'valid';
  const actionable = query.data?.status === 'pending' && query.data.account_match && !accept.isSuccess && !reject.isSuccess;
  return <main className="p12-invite-page" data-page="invitation" data-state={state}><section><p className="p12-eyebrow">GoJet invitation</p><h1>Workspace invitation</h1>{!runtime ? <InlineMessage variant="warning">Sign in before inspecting this invitation.</InlineMessage> : null}{query.isPending ? <p role="status">Checking invitation…</p> : null}{query.isError ? <InlineMessage variant="danger">This invitation is unavailable.</InlineMessage> : null}{query.data ? <Card><h2>{query.data.workspace_name}</h2><dl className="p12-definition-list"><div><dt>Role</dt><dd>{query.data.role}</dd></div><div><dt>Status</dt><dd>{query.data.status}</dd></div><div><dt>Expires</dt><dd>{new Date(query.data.expires_at).toLocaleString()}</dd></div><div><dt>Account match</dt><dd>{query.data.account_match ? 'Yes' : 'No'}</dd></div></dl>{!query.data.account_match ? <InlineMessage variant="warning">This invitation belongs to a different signed-in account.</InlineMessage> : null}{actionable ? <div className="p12-actions"><Button onClick={() => accept.mutate()} loading={accept.isPending}>Accept invitation</Button><Button variant="ghost" onClick={() => reject.mutate()} loading={reject.isPending}>Reject invitation</Button></div> : null}{accept.isSuccess ? <InlineMessage variant="success">Invitation accepted. Membership is active.</InlineMessage> : null}{reject.isSuccess ? <InlineMessage variant="info">Invitation rejected.</InlineMessage> : null}{accept.isError ? <InlineMessage variant="danger">{message(accept.error)}</InlineMessage> : null}{reject.isError ? <InlineMessage variant="danger">{message(reject.error)}</InlineMessage> : null}</Card> : null}</section></main>;
}
