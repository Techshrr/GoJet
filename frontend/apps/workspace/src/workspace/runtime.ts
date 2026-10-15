import { GoJetWorkspaceClient } from '@gojet/api-client';

export type P12Runtime = {
  actorId: string;
  email: string;
  displayName: string;
  workspaceId: string;
  testAuthority: boolean;
};

const STORAGE_KEY = 'gojet.p12.active-workspace';

export function readP12Runtime(): P12Runtime | null {
  if (import.meta.env.VITE_GOJET_TEST_AUTH_ENABLED !== '1') return null;
  const actorId = import.meta.env.VITE_GOJET_TEST_ACTOR_ID?.trim();
  const email = import.meta.env.VITE_GOJET_TEST_EMAIL?.trim().toLowerCase();
  const displayName = import.meta.env.VITE_GOJET_TEST_DISPLAY_NAME?.trim() || actorId || '';
  const configuredWorkspace = import.meta.env.VITE_GOJET_TEST_WORKSPACE_ID?.trim();
  if (!actorId || !email || !configuredWorkspace) return null;
  let workspaceId = configuredWorkspace;
  try {
    const selected = window.sessionStorage.getItem(STORAGE_KEY)?.trim();
    if (selected) workspaceId = selected;
  } catch { /* browser storage unavailable */ }
  return { actorId, email, displayName, workspaceId, testAuthority: true };
}

export function selectP12Workspace(workspaceId: string): void {
  const value = workspaceId.trim();
  if (!value) return;
  try { window.sessionStorage.setItem(STORAGE_KEY, value); } catch { /* browser storage unavailable */ }
}

export function clearP12WorkspaceSelection(): void {
  try { window.sessionStorage.removeItem(STORAGE_KEY); } catch { /* browser storage unavailable */ }
}

export function createP12Client(runtime: P12Runtime): GoJetWorkspaceClient {
  return new GoJetWorkspaceClient({
    fetch: async (input, init) => {
      const headers = new Headers(init?.headers);
      if (!runtime.testAuthority && !['GET', 'HEAD', 'OPTIONS'].includes((init?.method ?? 'GET').toUpperCase())) {
        const response = await fetch('/api/me', { credentials: 'same-origin', headers: { Accept: 'application/json' } });
        if (!response.ok) throw new Error('Authenticated session unavailable');
        const current = await response.json() as { user?: { id: string }; csrf_token?: string };
        if (current.user?.id !== runtime.actorId || !current.csrf_token) throw new Error('Authenticated session changed');
        headers.set('X-CSRF-Token', current.csrf_token);
      }
      return fetch(input, { ...init, headers, credentials: 'same-origin' });
    },
    headers: () => runtime.testAuthority ? ({
      'X-GoJet-Test-Actor': runtime.actorId,
      'X-GoJet-Test-Email': runtime.email,
      'X-GoJet-Test-Display-Name': runtime.displayName,
    }) : ({}),
  });
}

export async function loadP12Session(): Promise<P12Runtime> {
  const response = await fetch('/api/me', { credentials: 'same-origin', headers: { Accept: 'application/json' } });
  if (!response.ok) throw new Error('Authenticated session unavailable');
  const current = await response.json() as { user?: { id: string; email: string; display_name: string } };
  if (!current.user?.id) throw new Error('Invalid authenticated session');
  let workspaceId = '';
  try { workspaceId = window.sessionStorage.getItem(STORAGE_KEY)?.trim() ?? ''; } catch { /* storage unavailable */ }
  return { actorId: current.user.id, email: current.user.email, displayName: current.user.display_name,
    workspaceId, testAuthority: false };
}
