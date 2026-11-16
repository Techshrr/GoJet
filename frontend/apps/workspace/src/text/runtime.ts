import { workspaceTransport } from '../links/runtime';
import { GoJetTextClient } from '@gojet/api-client';
import { readWorkspaceRuntime, type WorkspaceRuntime } from '../links/runtime';

export type TextRuntime = WorkspaceRuntime;
export function readTextRuntime(): TextRuntime | null { return readWorkspaceRuntime(); }
export function createWorkspaceTextClient(runtime: TextRuntime): GoJetTextClient {
  return new GoJetTextClient(workspaceTransport(runtime));
}
