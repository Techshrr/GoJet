import { type FormEvent, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { GoJetApiError, type AnalyticsGranularity, type AnalyticsQueryInput } from '@gojet/api-client';
import { Button, Card, InlineMessage, SelectField, TextField } from '@gojet/ui';
import { AnalyticsReportView } from '../analytics/AnalyticsReportView';
import {
  createWorkspaceAnalyticsClient,
  defaultAnalyticsQuery,
  readAnalyticsRuntime,
} from '../analytics/runtime';
import { WorkspaceShell } from '../shell/WorkspaceShell';

function toLocalInput(iso: string): string {
  const date = new Date(iso);
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return local.toISOString().slice(0, 16);
}

function toISO(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  return date.toISOString();
}

export default function AnalyticsPage() {
  const runtime = useMemo(() => readAnalyticsRuntime(), []);
  const client = useMemo(() => runtime ? createWorkspaceAnalyticsClient(runtime) : null, [runtime]);
  const initial = useMemo(() => defaultAnalyticsQuery(), []);
  const [fromInput, setFromInput] = useState(toLocalInput(initial.from));
  const [toInput, setToInput] = useState(toLocalInput(initial.to));
  const [timezone, setTimezone] = useState(initial.timezone ?? 'UTC');
  const [granularity, setGranularity] = useState<AnalyticsGranularity>(initial.granularity ?? 'day');
  const [applied, setApplied] = useState<AnalyticsQueryInput>(initial);
  const [validationMessage, setValidationMessage] = useState<string | null>(null);

  const reportQuery = useQuery({
    queryKey: ['analytics-overview', runtime?.workspaceId, applied],
    enabled: client !== null && runtime !== null,
    queryFn: () => client!.overview(runtime!.workspaceId, applied),
    retry: false,
  });

  function applyFilters(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const from = toISO(fromInput);
    const to = toISO(toInput);
    if (!from || !to || new Date(from).getTime() >= new Date(to).getTime()) {
      setValidationMessage('From must be earlier than To. Analytics ranges use an inclusive From and exclusive To boundary.');
      return;
    }
    setValidationMessage(null);
    setApplied({ from, to, timezone: timezone.trim() || 'UTC', granularity });
  }

  const apiError = reportQuery.error instanceof GoJetApiError ? reportQuery.error : null;

  return (
    <WorkspaceShell state={!runtime ? 'api-offline' : 'notification-attention'} sectionLabel="Analytics">
      <section className="analytics-page" data-page="analytics" data-testid="analytics-page">
        <header className="analytics-page-header">
          <div>
            <p className="analytics-eyebrow">INSIGHTS</p>
            <h1>Analytics</h1>
            <p>Measured click and conversion data from the authoritative analytics pipeline. No predictive or client-generated totals are shown.</p>
          </div>
        </header>

        {!runtime ? (
          <InlineMessage variant="warning">Production Workspace identity and analytics permission are unavailable until P12/P15 provides authoritative authentication context.</InlineMessage>
        ) : null}

        <Card as="section" className="analytics-filter-card">
          <div className="analytics-section-heading">
            <div><h2>Report range</h2><p>From is inclusive; To is exclusive. Timezone controls calendar bucket boundaries.</p></div>
          </div>
          <form className="analytics-filter-form" onSubmit={applyFilters}>
            <TextField id="analytics-from" label="From" type="datetime-local" required value={fromInput} onChange={(event) => setFromInput(event.currentTarget.value)} />
            <TextField id="analytics-to" label="To" type="datetime-local" required value={toInput} onChange={(event) => setToInput(event.currentTarget.value)} />
            <TextField id="analytics-timezone" label="Timezone" required value={timezone} onChange={(event) => setTimezone(event.currentTarget.value)} helpText="Use an IANA timezone such as UTC, Asia/Singapore or America/New_York." />
            <SelectField id="analytics-granularity" label="Granularity" value={granularity} onChange={(event) => setGranularity(event.currentTarget.value as AnalyticsGranularity)} options={[
              { value: 'hour', label: 'Hour' },
              { value: 'day', label: 'Day' },
            ]} />
            <Button type="submit">Apply filters</Button>
          </form>
          {validationMessage ? <InlineMessage variant="danger">{validationMessage}</InlineMessage> : null}
        </Card>

        {reportQuery.isPending && runtime ? <Card as="section" className="analytics-loading" aria-live="polite"><p role="status">Loading measured analytics…</p></Card> : null}
        {reportQuery.isError ? (
          <InlineMessage variant="danger">Analytics data is unavailable. This persistent state is not presented as zero data. {apiError ? <><strong>{apiError.code}</strong>: {apiError.message}</> : null}</InlineMessage>
        ) : null}
        {reportQuery.data ? <AnalyticsReportView report={reportQuery.data} /> : null}
      </section>
    </WorkspaceShell>
  );
}
