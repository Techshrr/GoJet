import { GoJetDomainsClient } from '@gojet/api-client';
import type { WorkspaceRuntime } from '../links/runtime';

export function createWorkspaceDomainsClient(runtime: WorkspaceRuntime): GoJetDomainsClient {
  return new GoJetDomainsClient({
    headers: () => ({
      'X-GoJet-Test-Actor': runtime.actorId,
      'X-GoJet-Test-Workspace': runtime.workspaceId,
      'X-GoJet-Test-Workspace-Role': runtime.role,
    }),
  });
}
