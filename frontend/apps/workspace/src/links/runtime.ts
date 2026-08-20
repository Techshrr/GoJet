import { GoJetLinksClient } from '@gojet/api-client';

export type WorkspaceRuntime = {
  workspaceId: string;
  actorId: string;
  role: 'owner' | 'admin' | 'member' | 'viewer';
  testAuthority: boolean;
};

function readRole(value: string | undefined): WorkspaceRuntime['role'] {
  if (value === 'owner' || value === 'admin' || value === 'member' || value === 'viewer') return value;
  return 'viewer';
}

export function readWorkspaceRuntime(): WorkspaceRuntime | null {
  if (import.meta.env.VITE_GOJET_TEST_AUTH_ENABLED !== '1') return null;
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
  return new GoJetLinksClient({
    headers: () => ({
      'X-GoJet-Test-Actor': runtime.actorId,
      'X-GoJet-Test-Workspace': runtime.workspaceId,
      'X-GoJet-Test-Workspace-Role': runtime.role,
    }),
  });
}

export function isReadOnly(runtime: WorkspaceRuntime): boolean {
  return runtime.role === 'viewer';
}
