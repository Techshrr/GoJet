import { GoJetBioClient } from '@gojet/api-client';
import { readWorkspaceRuntime, type WorkspaceRuntime } from '../links/runtime';

export type BioRuntime = WorkspaceRuntime;
export function readBioRuntime(): BioRuntime | null { return readWorkspaceRuntime(); }
export function createWorkspaceBioClient(runtime: BioRuntime): GoJetBioClient {
  return new GoJetBioClient({
    headers: () => ({
      'X-GoJet-Test-Actor': runtime.actorId,
      'X-GoJet-Test-Workspace': runtime.workspaceId,
      'X-GoJet-Test-Workspace-Role': runtime.role,
    }),
  });
}
