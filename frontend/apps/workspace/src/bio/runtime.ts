import { workspaceTransport } from '../links/runtime';
import { GoJetBioClient } from '@gojet/api-client';
import { readWorkspaceRuntime, type WorkspaceRuntime } from '../links/runtime';

export type BioRuntime = WorkspaceRuntime;
export function readBioRuntime(): BioRuntime | null { return readWorkspaceRuntime(); }
export function createWorkspaceBioClient(runtime: BioRuntime): GoJetBioClient {
  return new GoJetBioClient(workspaceTransport(runtime));
}
