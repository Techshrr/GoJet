import { readWorkspaceRuntime, workspaceTransport } from '../links/runtime';
import type { ChangeEvent, MouseEvent, ReactNode } from 'react';
import { useId, useRef, useState } from 'react';
import { Link } from '@tanstack/react-router';
import { Button, Dialog, InlineMessage, useShellViewport } from '@gojet/ui';
import type { ShellState } from '@gojet/utils';

const groups = [
  ['CREATE', [['Links', '/app/links'], ['QR Codes', '/app/qr'], ['Files', '/app/files'], ['Text', '/app/text'], ['Bio', '/app/bio']]],
  ['INSIGHTS', [['Analytics', '/app/analytics']]],
  ['MANAGE', [['Domains', '/app/domains'], ['Organization', '/app/organization'], ['Campaigns', '/app/campaigns'], ['Tags', '/app/tags']]],
  ['DEVELOPER', [['API keys', '/app/api-keys'], ['Webhooks', '/app/webhooks']]],
  ['WORKSPACE', [['Billing', '/app/billing'], ['Members', '/app/members'], ['Notifications', '/app/notifications'], ['Workspace settings', '/app/settings/workspace'], ['Settings', '/app/settings/profile']]],
] as const;

type OverlayName = 'create' | 'command' | 'notifications';
export type WorkspaceSwitcherOption = { id: string; name: string };
type WorkspaceShellProps = {
  children: ReactNode;
  state?: ShellState<'workspace'> | 'normal';
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
  state = 'normal',
  sectionLabel = 'Overview',
  workspaceLabel = 'Workspace',
  workspaceId,
  workspaceOptions,
  onWorkspaceChange,
  unreadCount,
  notificationsContent,
}: WorkspaceShellProps) {
  const current = readWorkspaceRuntime();
  const [signOutError,setSignOutError] = useState('');
  async function signOut() { try {
    if (!current) return;
    const response = await workspaceTransport(current).fetch!('/api/auth/logout',{method:'POST'});
    if (!response.ok) throw new Error('Sign out failed. Try again.');
    window.location.replace('/login');
  } catch(error) {setSignOutError(error instanceof Error?error.message:'Sign out failed.');} }
  const [overlay, setOverlay] = useState<OverlayName | null>(null);
  const switcherHintId = useId();
  const lastTrigger = useRef<HTMLButtonElement | null>(null);
  const viewport = useShellViewport();
  const openOverlay = (name: OverlayName, event: MouseEvent<HTMLButtonElement>) => { lastTrigger.current = event.currentTarget; setOverlay(name); };
  const closeOverlay = () => { setOverlay(null); requestAnimationFrame(() => lastTrigger.current?.focus()); };
  const title = overlay === 'create' ? 'Create something new' : overlay === 'command' ? 'Command palette' : 'Notifications';

  const notificationLabel = unreadCount === undefined ? 'Notifications' : `Notifications, ${unreadCount} unread`;
  const visibleNotificationCount = unreadCount === undefined ? '' : ` · ${unreadCount}`;
  const switcherOptions = workspaceOptions?.length ? workspaceOptions : null;
  const changeWorkspace = (event: ChangeEvent<HTMLSelectElement>) => onWorkspaceChange?.(event.currentTarget.value);

  return (
    <div className="workspace-shell" data-shell="workspace" data-state={state} data-viewport={viewport}>
      <aside className="workspace-sidebar">
        <Link to="/app" className="workspace-logo">Linketic</Link>
        {switcherOptions ? (
          <label className="workspace-switcher-field">
            <span>Workspace</span>
            <span id={`${switcherHintId}-desktop`}>Selecting a workspace reloads this page.</span>
            <select className="workspace-switcher" aria-label="Workspace switcher" aria-describedby={`${switcherHintId}-desktop`} value={workspaceId ?? ''} onChange={changeWorkspace}>
              {switcherOptions.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
            </select>
          </label>
        ) : <a className="workspace-switcher" href="/app">{current?.workspaceName || workspaceLabel}</a>}
        <Button onClick={(event) => openOverlay('create', event)}>Create</Button>
        <nav aria-label="Workspace navigation">
          {groups.map(([group, items]) => <section key={group}><h2>{group}</h2>{items.map(([label, to]) => <Link key={to} to={to}>{label}</Link>)}</section>)}
        </nav>
        <Link to="/app/support">Help & support</Link><a href="/">Back to website</a>
      </aside>
      <div className="workspace-main">
        <header className="workspace-header">
          <nav aria-label="Breadcrumb">{workspaceLabel} / {sectionLabel}</nav>
          <div>
            <Button variant="ghost" onClick={(event) => openOverlay('command', event)}>Command</Button>
            <Link to="/app/support">Help</Link>
            <Button variant="ghost" onClick={(event) => openOverlay('notifications', event)} aria-label={notificationLabel}>Notifications{visibleNotificationCount}</Button>
            <a className="workspace-account" href="/app/settings/profile">{current?.displayName || 'My account'}</a><button className="workspace-signout" type="button" onClick={() => void signOut()}>Sign out</button>
          </div>
        </header>
        <details className="workspace-mobile-navigation" data-mobile-navigation onKeyDown={event => {
          if (event.key === 'Escape') {
            event.preventDefault(); event.currentTarget.open = false;
            event.currentTarget.querySelector('summary')?.focus();
          }
        }} onClick={event => {
          if (event.target instanceof Element && event.target.closest('a[href]')) event.currentTarget.open = false;
        }}>
          <summary>Workspace navigation</summary>
          {switcherOptions ? <label className="workspace-switcher-field"><span>Workspace</span>
            <span id={`${switcherHintId}-mobile`}>Selecting a workspace reloads this page.</span>
            <select className="workspace-switcher" aria-label="Workspace switcher" aria-describedby={`${switcherHintId}-mobile`} value={workspaceId ?? ''} onChange={changeWorkspace}>
              {switcherOptions.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}
            </select></label> : <span>{workspaceLabel}</span>}
          <Button onClick={event => openOverlay('create', event)}>Create</Button>
          <nav aria-label="Mobile Workspace navigation">
            {groups.map(([group, items]) => <section key={group}><h2>{group}</h2>{items.map(([label, to]) => <Link key={to} to={to}>{label}</Link>)}</section>)}
            <Link to="/app">Overview</Link><Link to="/app/support">Support</Link>
          </nav>
        </details>
        {state === 'api-offline' && <InlineMessage variant="warning">API is offline. Local navigation remains available.</InlineMessage>}
        {state === 'workspace-suspended' && <InlineMessage variant="danger">Workspace is suspended. Creation actions are unavailable.</InlineMessage>}
        {state === 'read-only-role' && <InlineMessage variant="info">You have read-only access to this workspace.</InlineMessage>}
        {signOutError && <p role="alert">{signOutError}</p>}<main className="workspace-content" aria-busy={state === 'loading-workspace'}>{children}</main>
      </div>
      <Dialog open={overlay !== null} title={title} description={overlay === 'notifications' ? 'Your latest updates' : 'Choose where to go next'} onClose={closeOverlay}>
        {overlay === 'notifications' ? (notificationsContent || <a href="/app/notifications">View all notifications</a>) : <div className="workspace-command-links">{(overlay === 'create' ? groups[0][1] : groups.flatMap<readonly [string, string]>(([,items])=>items)).map(([label,to])=><a key={to} href={overlay==='create' && to==='/app/links'?'/app/links/new':to}>{label}</a>)}</div>}
      </Dialog>
    </div>
  );
}
