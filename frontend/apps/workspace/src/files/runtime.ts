import { workspaceTransport } from '../links/runtime';
import { GoJetFilesClient } from '@gojet/api-client';
import { readWorkspaceRuntime, type WorkspaceRuntime } from '../links/runtime';

export type FilesRuntime = WorkspaceRuntime;
export function readFilesRuntime(): FilesRuntime | null { return readWorkspaceRuntime(); }
export function createWorkspaceFilesClient(runtime: FilesRuntime): GoJetFilesClient {
  return new GoJetFilesClient(workspaceTransport(runtime));
}
