import { workspaceTransport } from '../links/runtime';
import { GoJetAnalyticsClient, type AnalyticsQueryInput } from '@gojet/api-client';
import { readWorkspaceRuntime, type WorkspaceRuntime } from '../links/runtime';

export type AnalyticsRuntime = WorkspaceRuntime & {
  analyticsPermission: 'allow' | 'deny';
};

export function readAnalyticsRuntime(): AnalyticsRuntime | null {
  const runtime = readWorkspaceRuntime();
  if (!runtime) return null;
  return {
    ...runtime,
    analyticsPermission: !runtime.testAuthority ? 'allow' : import.meta.env.VITE_GOJET_TEST_ANALYTICS_PERMISSION === 'allow' ? 'allow' : 'deny',
  };
}

export function createWorkspaceAnalyticsClient(runtime: AnalyticsRuntime): GoJetAnalyticsClient {
  const transport = workspaceTransport(runtime);
  return new GoJetAnalyticsClient({ ...transport, headers: () => ({ ...transport.headers?.(), ...(runtime.testAuthority ? { 'X-GoJet-Test-Analytics-Permission': runtime.analyticsPermission } : {}) }) });
}

export function defaultAnalyticsQuery(now = new Date()): AnalyticsQueryInput {
  const to = new Date(now);
  const from = new Date(to.getTime() - 7 * 24 * 60 * 60 * 1000);
  return {
    from: from.toISOString(),
    to: to.toISOString(),
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC',
    granularity: 'day',
  };
}
