import type { ReactNode } from 'react';
import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { WorkspaceShell } from '../shell/WorkspaceShell';
import { createP12Client, readP12Runtime, selectP12Workspace } from './runtime';

export function useP12Authority() {
  const runtime = useMemo(() => readP12Runtime(), []);
  const client = useMemo(() => runtime ? createP12Client(runtime) : null, [runtime]);
  const workspaceId = runtime?.workspaceId ?? '';
  const workspaces = useQuery({
    queryKey: ['p12-workspaces', runtime?.actorId],
    enabled: client !== null,
    queryFn: () => client!.listWorkspaces(),
  });
  const overview = useQuery({
    queryKey: ['p12-overview', workspaceId, runtime?.actorId],
    enabled: client !== null && workspaceId !== '',
    queryFn: () => client!.overview(workspaceId),
  });
  const notifications = useQuery({
    queryKey: ['p12-notifications', workspaceId, runtime?.actorId],
    enabled: client !== null && workspaceId !== '',
    queryFn: () => client!.notifications(workspaceId, 'all', 8),
  });
  return { runtime, client, workspaceId, workspaces, overview, notifications };
}

type P12ShellProps = { sectionLabel: string; children: ReactNode };

export function P12Shell({ sectionLabel, children }: P12ShellProps) {
  const authority = useP12Authority();
  const overview = authority.overview.data;
  const role = overview?.membership.role;
  const shellState = !authority.runtime || authority.overview.isError
    ? 'api-offline'
    : authority.overview.isPending
      ? 'loading-workspace'
      : overview?.workspace.status === 'suspended'
        ? 'workspace-suspended'
        : role === 'viewer'
          ? 'read-only-role'
          : 'notification-attention';
  const options = authority.workspaces.data?.items.map((item) => ({ id: item.id, name: item.name })) ?? [];
  const recent = authority.notifications.data?.items ?? [];
  const workspaceLabel = (overview?.workspace.name ?? authority.workspaceId) || 'Workspace';
  return (
    <WorkspaceShell
      state={shellState}
      sectionLabel={sectionLabel}
      workspaceLabel={workspaceLabel}
      workspaceId={authority.workspaceId}
      workspaceOptions={options}
      unreadCount={authority.notifications.data?.unread_count ?? overview?.counts.unread_notifications}
      onWorkspaceChange={(workspaceId) => {
        selectP12Workspace(workspaceId);
        window.location.reload();
      }}
      notificationsContent={recent.length ? (
        <div className="p12-shell-notifications-wrap">
          <ul className="p12-shell-notifications" aria-label="Recent notifications">
            {recent.slice(0, 5).map((item) => <li key={item.id}><strong>{item.title}</strong><span>{item.summary}</span></li>)}
          </ul>
          <a href="/app/notifications">View all notifications</a>
        </div>
      ) : <div><p>No unread Workspace notifications.</p><a href="/app/notifications">View all notifications</a></div>}
    >
      {children}
    </WorkspaceShell>
  );
}
