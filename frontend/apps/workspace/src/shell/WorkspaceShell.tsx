import type { ChangeEvent, MouseEvent, ReactNode } from 'react';
import { useRef, useState } from 'react';
import { Link } from '@tanstack/react-router';
import { Button, Dialog, InlineMessage, useShellViewport } from '@gojet/ui';
import type { ShellState } from '@gojet/utils';

const groups = [
  ['CREATE', [['Links', '/app/links'], ['QR Codes', '/app/qr'], ['Files', '/app/files'], ['Text', '/app/text'], ['Bio', '/app/bio']]],
  ['INSIGHTS', [['Analytics', '/app/analytics']]],
  ['MANAGE', [['Domains', '/app/domains'], ['Organization', '/app/organization'], ['Campaigns', '/app/campaigns'], ['Tags', '/app/tags']]],
  ['DEVELOPER', [['Developer', '/app/developer']]],
  ['WORKSPACE', [['Billing', '/app/billing'], ['Members', '/app/members'], ['Notifications', '/app/notifications'], ['Workspace settings', '/app/settings/workspace'], ['Settings', '/app/settings']]],
] as const;

type OverlayName = 'create' | 'command' | 'notifications';
export type WorkspaceSwitcherOption = { id: string; name: string };
type WorkspaceShellProps = {
  children: ReactNode;
  state?: ShellState<'workspace'>;
  sectionLabel?: string;
  workspaceLabel?: string;
  workspaceId?: string;
  workspaceOptions?: WorkspaceSwitcherOption[];
  onWorkspaceChange?: (workspaceId: string) => void;
  unreadCount?: number | undefined;
  notificationsContent?: ReactNode;
};

export function WorkspaceShell({
  children,
  state = 'loading-workspace',
  sectionLabel = 'Overview',
  workspaceLabel = 'Workspace',
  workspaceId,
  workspaceOptions,
  onWorkspaceChange,
  unreadCount,
  notificationsContent,
}: WorkspaceShellProps) {
  const [overlay, setOverlay] = useState<OverlayName | null>(null);
  const lastTrigger = useRef<HTMLButtonElement | null>(null);
  const viewport = useShellViewport();
  const openOverlay = (name: OverlayName, event: MouseEvent<HTMLButtonElement>) => { lastTrigger.current = event.currentTarget; setOverlay(name); };
  const closeOverlay = () => { setOverlay(null); requestAnimationFrame(() => lastTrigger.current?.focus()); };
  const title = overlay === 'create' ? 'Global Create' : overlay === 'command' ? 'Command palette' : 'Notifications';
  const copy = overlay === 'create' ? 'Link · QR · File · Text · Bio' : overlay === 'command' ? 'Navigation and permitted actions' : 'Recent security, domain, billing, support and resource activity.';
  const notificationLabel = unreadCount === undefined ? 'Notifications' : `Notifications, ${unreadCount} unread`;
  const visibleNotificationCount = unreadCount === undefined ? '' : ` · ${unreadCount}`;
  const switcherOptions = workspaceOptions?.length ? workspaceOptions : null;
  const changeWorkspace = (event: ChangeEvent<HTMLSelectElement>) => onWorkspaceChange?.(event.currentTarget.value);

  return (
    <div className="workspace-shell" data-shell="workspace" data-state={state} data-viewport={viewport}>
      <aside className="workspace-sidebar">
        <Link to="/app" className="workspace-logo">GoJet</Link>
        {switcherOptions ? (
          <label className="workspace-switcher-field">
            <span>Workspace</span>
            <select className="workspace-switcher" aria-label="Workspace switcher" value={workspaceId ?? ''} onChange={changeWorkspace}>
              {switcherOptions.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
            </select>
          </label>
        ) : <button className="workspace-switcher" type="button">{workspaceLabel}</button>}
        <Button onClick={(event) => openOverlay('create', event)}>Create</Button>
        <nav aria-label="Workspace navigation">
          {groups.map(([group, items]) => <section key={group}><h2>{group}</h2>{items.map(([label, to]) => <Link key={to} to={to}>{label}</Link>)}</section>)}
        </nav>
        <Link to="/app/support">Support</Link><span>Signed in user</span>
      </aside>
      <div className="workspace-main">
        <header className="workspace-header">
          <nav aria-label="Breadcrumb">{workspaceLabel} / {sectionLabel}</nav>
          <div>
            <Button variant="ghost" onClick={(event) => openOverlay('command', event)}>Command</Button>
            <Link to="/app/support">Help</Link>
            <Button variant="ghost" onClick={(event) => openOverlay('notifications', event)} aria-label={notificationLabel}>Notifications{visibleNotificationCount}</Button>
            <button type="button">Avatar</button>
          </div>
        </header>
        {state === 'api-offline' && <InlineMessage variant="warning">API is offline. Local navigation remains available.</InlineMessage>}
        {state === 'workspace-suspended' && <InlineMessage variant="danger">Workspace is suspended. Creation actions are unavailable.</InlineMessage>}
        {state === 'read-only-role' && <InlineMessage variant="info">You have read-only access to this workspace.</InlineMessage>}
        <main className="workspace-content" aria-busy={state === 'loading-workspace'}>{children}</main>
      </div>
      <Dialog open={overlay !== null} title={title} description="Only one workspace overlay may be active at a time." onClose={closeOverlay}>
        {overlay === 'notifications' && notificationsContent ? notificationsContent : <p>{copy}</p>}
      </Dialog>
    </div>
  );
}
