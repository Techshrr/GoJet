import { GoJetLinksClient, type ApiTransport } from '@gojet/api-client';

export type WorkspaceRuntime = {
  workspaceId: string;
  actorId: string;
  role: 'owner' | 'admin' | 'member' | 'viewer';
  testAuthority: boolean;
  email?: string;
  displayName?: string;
  workspaceName?: string;
};

function readRole(value: string | undefined): WorkspaceRuntime['role'] {
  if (value === 'owner' || value === 'admin' || value === 'member' || value === 'viewer') return value;
  return 'viewer';
}

export function readWorkspaceRuntime(): WorkspaceRuntime | null {
  if (import.meta.env.VITE_GOJET_TEST_AUTH_ENABLED !== '1') return liveWorkspace;
  const workspaceId = import.meta.env.VITE_GOJET_TEST_WORKSPACE_ID?.trim();
  const actorId = import.meta.env.VITE_GOJET_TEST_ACTOR_ID?.trim();
  if (!workspaceId || !actorId) return null;
  return {
    workspaceId,
    actorId,
    role: readRole(import.meta.env.VITE_GOJET_TEST_WORKSPACE_ROLE),
    testAuthority: true,
  };
}

export function createWorkspaceLinksClient(runtime: WorkspaceRuntime): GoJetLinksClient {
  return new GoJetLinksClient(workspaceTransport(runtime));
}

export function isReadOnly(runtime: WorkspaceRuntime): boolean {
  return runtime.role === 'viewer';
}

let liveWorkspace: WorkspaceRuntime | null = null;
export function setLiveWorkspace(runtime: WorkspaceRuntime) { liveWorkspace = runtime; }
export function workspaceTransport(runtime: WorkspaceRuntime): ApiTransport {
  return {
    headers: () => runtime.testAuthority ? ({ 'X-GoJet-Test-Actor': runtime.actorId, 'X-GoJet-Test-Workspace': runtime.workspaceId, 'X-GoJet-Test-Workspace-Role': runtime.role }) : ({}),
    fetch: async (input, init) => {
      const headers = new Headers(init?.headers);
      if (!runtime.testAuthority && !['GET','HEAD','OPTIONS'].includes((init?.method ?? 'GET').toUpperCase())) {
        const response = await fetch('/api/me', { credentials: 'same-origin', cache: 'no-store' });
        if (!response.ok) throw new Error('Please sign in again.');
        const current = await response.json() as { user: { id: string }; csrf_token: string };
        if (current.user.id !== runtime.actorId || !current.csrf_token) throw new Error('Your session changed. Reload this page.');
        headers.set('X-CSRF-Token', current.csrf_token);
      }
      return fetch(input, { ...init, headers, credentials: 'same-origin' });
    },
  };
}
