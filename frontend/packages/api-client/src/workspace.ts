import { GoJetApiError } from './links';
import type { ApiTransport } from './links';

export type WorkspaceRole = 'owner' | 'admin' | 'member' | 'viewer';
export type WorkspaceRecord = {
  id: string;
  name: string;
  status: 'active' | 'suspended';
  version: number;
  created_by: string;
  created_at: string;
  updated_at: string;
};
export type WorkspaceMembership = {
  id: number;
  workspace_id: string;
  user_id: string;
  email: string;
  display_name: string;
  role: WorkspaceRole;
  joined_at: string;
  updated_at: string;
};
export type WorkspaceInvitation = {
  id: number;
  workspace_id: string;
  email: string;
  role: Exclude<WorkspaceRole, 'owner'>;
  status: 'pending' | 'accepted' | 'rejected' | 'revoked' | 'expired';
  expires_at: string;
  invited_by: string;
  accepted_by?: string | null;
  created_at: string;
  updated_at: string;
};
export type InvitationInspection = {
  workspace_id: string;
  workspace_name: string;
  role: Exclude<WorkspaceRole, 'owner'>;
  status: WorkspaceInvitation['status'];
  expires_at: string;
  account_match: boolean;
};
export type WorkspaceOrganization = {
  workspace_id: string;
  name: string;
  description: string;
  version: number;
  updated_at: string;
};
export type WorkspaceCampaign = {
  id: string;
  workspace_id: string;
  name: string;
  status: 'active' | 'archived';
  version: number;
  created_by: string;
  created_at: string;
  updated_at: string;
};
export type WorkspaceTag = {
  id: number;
  workspace_id: string;
  name: string;
  normalized_name: string;
  version: number;
  created_by: string;
  created_at: string;
  updated_at: string;
};
export type WorkspaceFolder = WorkspaceTag;
export type WorkspaceNotificationState = {
  workspace_id: string;
  status: 'complete' | 'partial' | 'stale';
  data_through_at?: string | null;
  state_reason: string;
  updated_at: string;
};
export type WorkspaceNotification = {
  id: number;
  workspace_id: string;
  recipient_user_id: string;
  category: 'security' | 'domains' | 'billing' | 'support' | 'resources';
  event_key: string;
  title: string;
  summary: string;
  deep_link?: string;
  resource_type?: string;
  resource_id?: string;
  read_at?: string | null;
  created_at: string;
};
export type WorkspaceOverview = {
  workspace: WorkspaceRecord;
  membership: WorkspaceMembership;
  counts: { members: number; campaigns: number; tags: number; folders: number; unread_notifications: number };
  notification_state: WorkspaceNotificationState;
};
export type WorkspaceContext = { workspace: WorkspaceRecord; membership: WorkspaceMembership };
export type WorkspaceMembersResponse = { members: WorkspaceMembership[]; invitations: WorkspaceInvitation[] };
export type WorkspaceNotificationsResponse = { items: WorkspaceNotification[]; unread_count: number; state: WorkspaceNotificationState };
export type CreatedWorkspaceInvitation = { invitation: WorkspaceInvitation; token: string };

type ApiErrorEnvelope = { error?: { code?: string; message?: string } };
function normalizeBaseUrl(value: string | undefined): string { return value?.replace(/\/$/, '') ?? ''; }
function workspacePath(workspaceId: string): string { return `/api/workspaces/${encodeURIComponent(workspaceId)}`; }

export class GoJetWorkspaceClient {
  private readonly baseUrl: string;
  private readonly headers: (() => HeadersInit) | undefined;
  private readonly doFetch: typeof globalThis.fetch;

  constructor(transport: ApiTransport = {}) {
    this.baseUrl = normalizeBaseUrl(transport.baseUrl);
    this.headers = transport.headers;
    this.doFetch = transport.fetch ?? globalThis.fetch.bind(globalThis);
  }

  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const headers = new Headers(this.headers?.());
    headers.set('Accept', 'application/json');
    if (init.body !== undefined && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
    const response = await this.doFetch(`${this.baseUrl}${path}`, { ...init, headers });
    if (!response.ok) {
      let envelope: ApiErrorEnvelope = {};
      try { envelope = await response.json() as ApiErrorEnvelope; } catch { /* non-JSON error */ }
      throw new GoJetApiError(
        response.status,
        envelope.error?.code ?? 'request_failed',
        envelope.error?.message ?? `Request failed with HTTP ${response.status}`,
      );
    }
    if (response.status === 204) return undefined as T;
    return await response.json() as T;
  }

  listWorkspaces(): Promise<{ items: WorkspaceRecord[] }> { return this.request('/api/workspaces'); }
  createWorkspace(name: string): Promise<WorkspaceContext> {
    return this.request('/api/workspaces', { method: 'POST', body: JSON.stringify({ name }) });
  }
  getWorkspace(workspaceId: string): Promise<WorkspaceContext> { return this.request(workspacePath(workspaceId)); }
  updateWorkspace(workspaceId: string, name: string, expectedVersion: number, reason = ''): Promise<WorkspaceRecord> {
    return this.request(workspacePath(workspaceId), { method: 'PATCH', body: JSON.stringify({ name, expected_version: expectedVersion, reason }) });
  }
  overview(workspaceId: string): Promise<WorkspaceOverview> { return this.request(`${workspacePath(workspaceId)}/overview`); }

  members(workspaceId: string): Promise<WorkspaceMembersResponse> { return this.request(`${workspacePath(workspaceId)}/members`); }
  updateMember(workspaceId: string, memberId: number, role: WorkspaceRole, reason = ''): Promise<WorkspaceMembership> {
    return this.request(`${workspacePath(workspaceId)}/members/${memberId}`, { method: 'PATCH', body: JSON.stringify({ role, reason }) });
  }
  removeMember(workspaceId: string, memberId: number, reason = ''): Promise<void> {
    return this.request(`${workspacePath(workspaceId)}/members/${memberId}`, { method: 'DELETE', body: JSON.stringify({ reason }) });
  }
  invitations(workspaceId: string): Promise<{ items: WorkspaceInvitation[] }> { return this.request(`${workspacePath(workspaceId)}/invitations`); }
  invite(workspaceId: string, email: string, role: Exclude<WorkspaceRole, 'owner'>, expiresAt: string, reason = ''): Promise<CreatedWorkspaceInvitation> {
    return this.request(`${workspacePath(workspaceId)}/invitations`, { method: 'POST', body: JSON.stringify({ email, role, expires_at: expiresAt, reason }) });
  }
  revokeInvitation(workspaceId: string, invitationId: number): Promise<void> {
    return this.request(`${workspacePath(workspaceId)}/invitations/${invitationId}`, { method: 'DELETE' });
  }
  inspectInvitation(token: string): Promise<InvitationInspection> { return this.request(`/api/invitations/${encodeURIComponent(token)}`); }
  acceptInvitation(token: string): Promise<WorkspaceMembership> {
    return this.request('/api/invitations/accept', { method: 'POST', body: JSON.stringify({ token }) });
  }
  rejectInvitation(token: string): Promise<void> {
    return this.request('/api/invitations/reject', { method: 'POST', body: JSON.stringify({ token }) });
  }

  organization(workspaceId: string): Promise<WorkspaceOrganization> { return this.request(`${workspacePath(workspaceId)}/organization`); }
  updateOrganization(workspaceId: string, name: string, description: string, expectedVersion: number): Promise<WorkspaceOrganization> {
    return this.request(`${workspacePath(workspaceId)}/organization`, { method: 'PATCH', body: JSON.stringify({ name, description, expected_version: expectedVersion }) });
  }
  campaigns(workspaceId: string): Promise<{ items: WorkspaceCampaign[] }> { return this.request(`${workspacePath(workspaceId)}/campaigns`); }
  createCampaign(workspaceId: string, name: string): Promise<WorkspaceCampaign> {
    return this.request(`${workspacePath(workspaceId)}/campaigns`, { method: 'POST', body: JSON.stringify({ name }) });
  }
  updateCampaign(workspaceId: string, campaignId: string, name: string, status: WorkspaceCampaign['status'], expectedVersion: number): Promise<WorkspaceCampaign> {
    return this.request(`${workspacePath(workspaceId)}/campaigns/${encodeURIComponent(campaignId)}`, { method: 'PATCH', body: JSON.stringify({ name, status, expected_version: expectedVersion }) });
  }
  removeCampaign(workspaceId: string, campaignId: string): Promise<void> {
    return this.request(`${workspacePath(workspaceId)}/campaigns/${encodeURIComponent(campaignId)}`, { method: 'DELETE' });
  }
  tags(workspaceId: string): Promise<{ items: WorkspaceTag[] }> { return this.request(`${workspacePath(workspaceId)}/tags`); }
  createTag(workspaceId: string, name: string): Promise<WorkspaceTag> {
    return this.request(`${workspacePath(workspaceId)}/tags`, { method: 'POST', body: JSON.stringify({ name }) });
  }
  updateTag(workspaceId: string, tagId: number, name: string, expectedVersion: number): Promise<WorkspaceTag> {
    return this.request(`${workspacePath(workspaceId)}/tags/${tagId}`, { method: 'PATCH', body: JSON.stringify({ name, expected_version: expectedVersion }) });
  }
  removeTag(workspaceId: string, tagId: number): Promise<void> {
    return this.request(`${workspacePath(workspaceId)}/tags/${tagId}`, { method: 'DELETE' });
  }
  folders(workspaceId: string): Promise<{ items: WorkspaceFolder[] }> { return this.request(`${workspacePath(workspaceId)}/folders`); }
  createFolder(workspaceId: string, name: string): Promise<WorkspaceFolder> {
    return this.request(`${workspacePath(workspaceId)}/folders`, { method: 'POST', body: JSON.stringify({ name }) });
  }
  updateFolder(workspaceId: string, folderId: number, name: string, expectedVersion: number): Promise<WorkspaceFolder> {
    return this.request(`${workspacePath(workspaceId)}/folders/${folderId}`, { method: 'PATCH', body: JSON.stringify({ name, expected_version: expectedVersion }) });
  }
  removeFolder(workspaceId: string, folderId: number): Promise<void> {
    return this.request(`${workspacePath(workspaceId)}/folders/${folderId}`, { method: 'DELETE' });
  }

  notifications(workspaceId: string, category = 'all', limit = 50): Promise<WorkspaceNotificationsResponse> {
    const query = new URLSearchParams({ category, limit: String(limit) });
    return this.request(`${workspacePath(workspaceId)}/notifications?${query}`);
  }
  markNotification(workspaceId: string, notificationId: number, read: boolean): Promise<{ id: number; read: boolean }> {
    return this.request(`${workspacePath(workspaceId)}/notifications/${notificationId}/${read ? 'read' : 'unread'}`, { method: 'POST' });
  }
  markAllNotificationsRead(workspaceId: string): Promise<{ updated: number }> {
    return this.request(`${workspacePath(workspaceId)}/notifications/read-all`, { method: 'POST' });
  }
}
