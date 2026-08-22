import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { GoJetApiError } from '@gojet/api-client';
import { Card, InlineMessage } from '@gojet/ui';
import { AnalyticsReportView } from './AnalyticsReportView';
import {
  createWorkspaceAnalyticsClient,
  defaultAnalyticsQuery,
  readAnalyticsRuntime,
} from './runtime';

export function LinkAnalyticsPanel({ linkId }: { linkId: number }) {
  const runtime = useMemo(() => readAnalyticsRuntime(), []);
  const client = useMemo(() => runtime ? createWorkspaceAnalyticsClient(runtime) : null, [runtime]);
  const query = useMemo(() => defaultAnalyticsQuery(), []);
  const reportQuery = useQuery({
    queryKey: ['analytics-link', runtime?.workspaceId, linkId, query],
    enabled: client !== null && runtime !== null && Number.isSafeInteger(linkId) && linkId > 0,
    queryFn: () => client!.link(runtime!.workspaceId, linkId, query),
    retry: false,
  });
  const apiError = reportQuery.error instanceof GoJetApiError ? reportQuery.error : null;

  if (!runtime) {
    return <InlineMessage variant="warning">Analytics identity and permission are unavailable until P12/P15 provides authoritative authentication context.</InlineMessage>;
  }
  if (reportQuery.isPending) {
    return <Card as="section" className="analytics-loading"><p role="status">Loading measured link analytics…</p></Card>;
  }
  if (reportQuery.isError) {
    return <InlineMessage variant="danger">Link analytics is unavailable and is not presented as zero. {apiError ? <><strong>{apiError.code}</strong>: {apiError.message}</> : null}</InlineMessage>;
  }
  return reportQuery.data ? <AnalyticsReportView report={reportQuery.data} compact /> : null;
}
