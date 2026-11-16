import { workspaceTransport } from '../links/runtime';
import { GoJetDomainsClient } from '@gojet/api-client';
import type { WorkspaceRuntime } from '../links/runtime';

export function createWorkspaceDomainsClient(runtime: WorkspaceRuntime): GoJetDomainsClient {
  return new GoJetDomainsClient(workspaceTransport(runtime));
}
