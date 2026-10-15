import { useEffect, useState } from 'react';
import { readSupportRuntime, type SupportRuntime } from './runtime';

type WorkspaceChoice = { id: string; name: string; status: string };
export function useSupportSession() {
  const [runtime, setRuntime] = useState<SupportRuntime | null>(() => readSupportRuntime());
  const [choices, setChoices] = useState<WorkspaceChoice[]>([]);
  const [identity, setIdentity] = useState<{ id: string; email: string; display_name: string } | null>(null);
  const [message, setMessage] = useState('Loading your workspaces…');
  useEffect(() => {
    if (import.meta.env.VITE_GOJET_TEST_AUTH_ENABLED === '1') return;
    let cancelled = false;
    const read = async (path: string) => {
      const response = await fetch(path, { credentials: 'same-origin', headers: { Accept: 'application/json' } });
      if (!response.ok) throw new Error('Session or workspace unavailable');
      return response.json();
    };
    Promise.all([read('/api/me'), read('/api/workspaces')]).then(([current, listing]) => {
      if (cancelled) return;
      if (!current.user?.id || !Array.isArray(listing.items ?? [])) throw new Error('Invalid session response');
      const available: WorkspaceChoice[] = (listing.items ?? []).filter((item: WorkspaceChoice) => item.status === 'active');
      setIdentity(current.user);
      setChoices(available);
      const requested = new URLSearchParams(window.location.search).get('workspace_id');
      const selected = requested ? available.find((item) => item.id === requested) : available.length === 1 ? available[0] : undefined;
      if (selected) setRuntime({ workspaceId: selected.id, actorId: current.user.id, email: current.user.email, displayName: current.user.display_name, role: 'viewer', testAuthority: false, turnstileToken: '' });
      setMessage(available.length ? 'Choose a workspace for support.' : 'No active workspace is available.');
    }).catch(() => { if (!cancelled) { setRuntime(null); setChoices([]); setMessage('Your session or workspaces could not be loaded. Reload to retry.'); } });
    return () => { cancelled = true; };
  }, []);
  const selector = import.meta.env.VITE_GOJET_TEST_AUTH_ENABLED === '1' ? null : <div>{choices.length ? <label>Support workspace<select aria-label="Support workspace" value={runtime?.workspaceId ?? ''} onChange={(event) => {
    const selected = choices.find((item) => item.id === event.currentTarget.value);
    setRuntime(selected && identity ? { workspaceId: selected.id, actorId: identity.id, email: identity.email, displayName: identity.display_name, role: 'viewer', testAuthority: false, turnstileToken: '' } : null);
  }}><option value="">Choose a workspace</option>{choices.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label> : <p role="status">{message}</p>}</div>;
  return { runtime, selector };
}
