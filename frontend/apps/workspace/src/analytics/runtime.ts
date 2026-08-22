import { GoJetAnalyticsClient } from '@gojet/api-client';
import { readWorkspaceRuntime, type WorkspaceRuntime } from '../links/runtime';

export type AnalyticsRuntime = WorkspaceRuntime & {
  analyticsPermission: 'allow' | 'deny';
};

export function readAnalyticsRuntime(): AnalyticsRuntime | null {
  const runtime = readWorkspaceRuntime();
  if (!runtime) return null;
  return {
    ...runtime,
    analyticsPermission: import.meta.env.VITE_GOJET_TEST_ANALYTICS_PERMISSION === 'allow' ? 'allow' : 'deny',
  };
}

export function createWorkspaceAnalyticsClient(runtime: AnalyticsRuntime): GoJetAnalyticsClient {
  return new GoJetAnalyticsClient({
    headers: () => ({
      'X-GoJet-Test-Actor': runtime.actorId,
      'X-GoJet-Test-Workspace': runtime.workspaceId,
      'X-GoJet-Test-Workspace-Role': runtime.role,
      'X-GoJet-Test-Analytics-Permission': runtime.analyticsPermission,
    }),
  });
}
