import type { FormEvent } from 'react';
import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Card, EmptyState, InlineMessage, TextField } from '@gojet/ui';
import type { WorkspaceRole } from '@gojet/api-client';
import { P12Shell, useP12Authority } from './authority';

function message(error: unknown): string { return error instanceof Error ? error.message : 'The request could not be completed.'; }
function canManage(role: WorkspaceRole | undefined): boolean { return role === 'owner' || role === 'admin'; }

export default function MembersPage() {
  const authority = useP12Authority();
  const queryClient = useQueryClient();
  const role = authority.overview.data?.membership.role;
  const allowed = canManage(role);
  const members = useQuery({
    queryKey: ['p12-members', authority.workspaceId],
    enabled: !!authority.client && !!authority.workspaceId,
    queryFn: () => authority.client!.members(authority.workspaceId),
  });
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
  const lastOwnerProtected = [roleMutation.error, removeMutation.error].some((error) => error instanceof Error && error.message.toLowerCase().includes('last workspace owner'));
  const state = members.isPending ? 'loading' : members.isError ? 'error' : lastOwnerProtected ? 'last-owner-protected' : allowed ? 'manage' : 'read-only';

  return (
    <P12Shell sectionLabel="Members">
      <section className="p12-page" data-page="workspace-members" data-state={state}>
        <header className="p12-page-header"><p className="p12-eyebrow">RBAC</p><h1>Members and invitations</h1><p>Roles are re-resolved from MySQL on every P12-owned request; client role claims are not authorization.</p></header>
        {members.isError ? <InlineMessage variant="danger">Members could not be loaded.</InlineMessage> : null}
        {inviteMutation.isError ? <InlineMessage variant="danger">{message(inviteMutation.error)}</InlineMessage> : null}
        {roleMutation.isError ? <InlineMessage variant="danger">{message(roleMutation.error)}</InlineMessage> : null}
        {removeMutation.isError ? <InlineMessage variant="danger">{message(removeMutation.error)}</InlineMessage> : null}
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
