import { GoJetQRClient } from '@gojet/api-client';
import { readWorkspaceRuntime, type WorkspaceRuntime } from '../links/runtime';

export type QRRuntime = WorkspaceRuntime;

export function readQRRuntime(): QRRuntime | null {
  return readWorkspaceRuntime();
}

export function createWorkspaceQRClient(runtime: QRRuntime): GoJetQRClient {
  return new GoJetQRClient({
    headers: () => ({
      'X-GoJet-Test-Actor': runtime.actorId,
      'X-GoJet-Test-Workspace': runtime.workspaceId,
      'X-GoJet-Test-Workspace-Role': runtime.role,
    }),
  });
}
