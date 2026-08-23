import { GoJetFilesClient } from '@gojet/api-client';
import { readWorkspaceRuntime, type WorkspaceRuntime } from '../links/runtime';

export type FilesRuntime = WorkspaceRuntime;
export function readFilesRuntime(): FilesRuntime | null { return readWorkspaceRuntime(); }
export function createWorkspaceFilesClient(runtime: FilesRuntime): GoJetFilesClient {
  return new GoJetFilesClient({
    headers: () => ({
      'X-GoJet-Test-Actor': runtime.actorId,
      'X-GoJet-Test-Workspace': runtime.workspaceId,
      'X-GoJet-Test-Workspace-Role': runtime.role,
    }),
  });
}
