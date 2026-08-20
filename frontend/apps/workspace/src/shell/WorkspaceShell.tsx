import type { MouseEvent, ReactNode } from 'react';
import { useRef, useState } from 'react';
import { Link } from '@tanstack/react-router';
import { Button, Dialog, InlineMessage } from '@gojet/ui';
import { useShellViewport } from '@gojet/shell-runtime';
import type { ShellState } from '@gojet/utils';

const groups = [
  ['CREATE', [['Links', '/app/links'], ['QR Codes', '/app/qr'], ['Files', '/app/files']]],
  ['INSIGHTS', [['Analytics', '/app/analytics']]],
  ['MANAGE', [['Domains', '/app/domains']]],
  ['DEVELOPER', [['Developer', '/app/developer']]],
  ['WORKSPACE', [['Members', '/app/members'], ['Settings', '/app/settings']]],
] as const;

type OverlayName = 'create' | 'command' | 'notifications';

export function WorkspaceShell({ children, state = 'loading-workspace' }: { children: ReactNode; state?: ShellState<'workspace'> }) {
  const [overlay, setOverlay] = useState<OverlayName | null>(null);
  const lastTrigger = useRef<HTMLButtonElement | null>(null);
  const viewport = useShellViewport();
  const openOverlay = (name: OverlayName, event: MouseEvent<HTMLButtonElement>) => {
    lastTrigger.current = event.currentTarget;
    setOverlay(name);
  };
  const closeOverlay = () => {
    setOverlay(null);
    requestAnimationFrame(() => lastTrigger.current?.focus());
  };
  const title = overlay === 'create' ? 'Global Create' : overlay === 'command' ? 'Command palette' : 'Notifications';
  const copy = overlay === 'create' ? 'Link · QR · File · Text · Bio' : overlay === 'command' ? 'Navigation and permitted actions' : 'Recent security, domain, billing and support activity.';

  return (
    <div className="workspace-shell" data-shell="workspace" data-state={state} data-viewport={viewport}>
      <aside className="workspace-sidebar">
        <Link to="/app" className="workspace-logo">GoJet</Link>
        <button className="workspace-switcher" type="button">Acme Workspace</button>
        <Button onClick={(event) => openOverlay('create', event)}>Create</Button>
        <nav aria-label="Workspace navigation">
          {groups.map(([group, items]) => <section key={group}><h2>{group}</h2>{items.map(([label, to]) => <Link key={to} to={to}>{label}</Link>)}</section>)}
        </nav>
        <a href="/docs/en/">Support</a><span>Signed in user</span>
      </aside>
      <div className="workspace-main">
        <header className="workspace-header">
          <nav aria-label="Breadcrumb">Workspace / Overview</nav>
          <div>
            <Button variant="ghost" onClick={(event) => openOverlay('command', event)}>Command</Button>
            <a href="/docs/en/">Help</a>
            <Button variant="ghost" onClick={(event) => openOverlay('notifications', event)} aria-label="Notifications, 2 unread">Notifications · 2</Button>
            <button type="button">Avatar</button>
          </div>
        </header>
        {state === 'api-offline' && <InlineMessage variant="warning">API is offline. Local navigation remains available.</InlineMessage>}
        {state === 'workspace-suspended' && <InlineMessage variant="danger">Workspace is suspended. Creation actions are unavailable.</InlineMessage>}
        {state === 'read-only-role' && <InlineMessage variant="info">You have read-only access to this workspace.</InlineMessage>}
        <main className="workspace-content" aria-busy={state === 'loading-workspace'}>{children}</main>
      </div>
      <Dialog open={overlay !== null} title={title} description="Only one workspace overlay may be active at a time." onClose={closeOverlay}>
        <p>{copy}</p>
      </Dialog>
    </div>
  );
}
