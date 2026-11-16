import { useEffect, useState, type ReactNode } from 'react';
import { GoJetWorkspaceClient } from '@gojet/api-client';
import { setLiveWorkspace, workspaceTransport } from '../links/runtime';

export function SessionGate({ children }: { children: ReactNode }) {
  const [ready, setReady] = useState(import.meta.env.VITE_GOJET_TEST_AUTH_ENABLED === '1');
  const [empty, setEmpty] = useState(false); const [name, setName] = useState('My workspace');
  const [error, setError] = useState(''); const [busy, setBusy] = useState(false);
  const [actor, setActor] = useState<{ id: string; email: string; display_name: string } | null>(null);
  useEffect(() => {
    if (ready) return; let active = true;
    void (async () => {
      const response = await fetch('/api/me', { credentials: 'same-origin', cache: 'no-store' });
      if (response.status === 401) { window.location.replace('/login'); return; }
      if (!response.ok) throw new Error('We couldn’t load your account. Please try again.');
      const session = await response.json() as { user: { id: string; email: string; display_name: string } };
      if (!active) return; setActor(session.user);
      const runtime = { actorId: session.user.id, workspaceId: '', role: 'viewer' as const, testAuthority: false };
      const client = new GoJetWorkspaceClient(workspaceTransport(runtime));
      const result = await client.listWorkspaces();
      if (!active) return;
      if (!result.items.length) { setEmpty(true); return; }
      let remembered = ''; try { remembered = sessionStorage.getItem('gojet.p12.active-workspace') || ''; } catch { /* optional preference */ }
      const chosen = result.items.find((item) => item.id === remembered) ?? result.items[0]!;
      const overview = await client.overview(chosen.id);
      if (!active) return;
      setLiveWorkspace({ ...runtime, workspaceId: chosen.id, role: overview.membership.role, email: session.user.email, displayName: session.user.display_name, workspaceName: chosen.name });
      setReady(true);
    })().catch((caught) => { if (active) setError(caught instanceof Error ? caught.message : 'Your account could not be loaded.'); });
    return () => { active = false; };
  }, [ready]);
  async function create(event: React.FormEvent) {
    event.preventDefault(); if (!actor || busy) return; setBusy(true); setError('');
    try {
      const client = new GoJetWorkspaceClient(workspaceTransport({ actorId: actor.id, workspaceId: '', role: 'owner', testAuthority: false }));
      await client.createWorkspace(name.trim()); window.location.replace('/app');
    } catch (caught) { setError(caught instanceof Error ? caught.message : 'Could not create the workspace.'); setBusy(false); }
  }
  if (ready) return children;
  return <main className="workspace-welcome"><a className="site-brand" href="/">Linketic</a><section><h1>{empty ? `Welcome${actor?.display_name ? ', ' + actor.display_name : ''}` : 'Opening your account'}</h1>{error && <p role="alert">{error}</p>}{empty ? <><p>Create a workspace to organize your links, QR codes and files. You can invite your team later.</p><form onSubmit={create}><label htmlFor="workspace-name">Workspace name</label><input id="workspace-name" autoComplete="organization" required maxLength={128} value={name} onChange={(event) => setName(event.target.value)} /><button disabled={busy}>{busy ? 'Creating…' : 'Create workspace'}</button></form></> : error ? <button onClick={() => window.location.reload()}>Try again</button> : <p role="status">Loading your workspace…</p>}</section></main>;
}
