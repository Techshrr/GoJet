import { GoJetTextClient } from '@gojet/api-client';
import { readWorkspaceRuntime, type WorkspaceRuntime } from '../links/runtime';

export type TextRuntime = WorkspaceRuntime;
export function readTextRuntime(): TextRuntime | null { return readWorkspaceRuntime(); }
export function createWorkspaceTextClient(runtime: TextRuntime): GoJetTextClient {
  return new GoJetTextClient({
    headers: () => ({
      'X-GoJet-Test-Actor': runtime.actorId,
      'X-GoJet-Test-Workspace': runtime.workspaceId,
      'X-GoJet-Test-Workspace-Role': runtime.role,
    }),
  });
}
