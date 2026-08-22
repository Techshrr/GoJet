import type { AnalyticsDimensionCount, AnalyticsReport } from '@gojet/api-client';
import { Card, EmptyState, InlineMessage } from '@gojet/ui';

function formatValue(value: string): string {
  return value || 'Unknown';
}

function DimensionList({ title, items }: { title: string; items: AnalyticsDimensionCount[] }) {
  return (
    <Card as="section" className="analytics-dimension-card">
      <h3>{title}</h3>
      {items.length === 0 ? <p className="analytics-muted">No measured values in this range.</p> : (
        <ul className="analytics-dimension-list">
          {items.slice(0, 6).map((item) => (
            <li key={`${title}-${item.value}`}>
              <span>{formatValue(item.value)}</span>
              <strong>{item.clicks}</strong>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

export function AnalyticsReportView({ report, compact = false }: { report: AnalyticsReport; compact?: boolean }) {
  return (
    <div className="analytics-report" data-analytics-state={report.state}>
      {report.state === 'partial' ? (
        <InlineMessage variant="warning">Analytics data is partial. Displayed totals are measured data only and must not be interpreted as a complete zero or complete period.</InlineMessage>
      ) : null}
      {report.state === 'stale' ? (
        <InlineMessage variant="warning">Analytics data is stale. The last measured data remains visible while ingestion or reconciliation catches up.</InlineMessage>
      ) : null}
      {report.retention_limited ? (
        <InlineMessage variant="info">This range starts before the retained analytics window. Earlier history is unavailable; totals begin at the effective retention cutoff.</InlineMessage>
      ) : null}

      {report.state === 'empty' ? (
        <EmptyState title="No measured activity" reason="This complete analytics range contains zero measured clicks. Partial and unavailable states are shown separately." />
      ) : (
        <>
          <div className="analytics-metrics" aria-label="Analytics totals">
            <Card as="section" className="analytics-metric-card"><span>Total clicks</span><strong>{report.total_clicks}</strong></Card>
            <Card as="section" className="analytics-metric-card"><span>Conversions</span><strong>{report.total_conversions}</strong></Card>
            <Card as="section" className="analytics-metric-card"><span>Data state</span><strong>{report.state}</strong></Card>
          </div>

          <Card as="section" className="analytics-buckets-card">
            <div className="analytics-section-heading">
              <div><h2>{compact ? 'Measured activity' : 'Activity over time'}</h2><p>{report.timezone} · {report.granularity} buckets</p></div>
              <span className="analytics-state-label" data-state={report.state}>State: {report.state}</span>
            </div>
            {report.buckets.length === 0 ? <p className="analytics-muted">No measured buckets are available for this scope.</p> : (
              <div className="analytics-table-region" role="region" aria-label="Measured click buckets" tabIndex={0}>
                <table className="analytics-table">
                  <caption>Measured click totals by {report.granularity}</caption>
                  <thead><tr><th scope="col">Bucket</th><th scope="col">Clicks</th></tr></thead>
                  <tbody>{report.buckets.map((bucket) => <tr key={bucket.key}><td>{bucket.key}</td><td>{bucket.clicks}</td></tr>)}</tbody>
                </table>
              </div>
            )}
          </Card>

          {!compact ? (
            <div className="analytics-dimensions" aria-label="Measured dimensions">
              <DimensionList title="Countries" items={report.dimensions.country} />
              <DimensionList title="Devices" items={report.dimensions.device} />
              <DimensionList title="Languages" items={report.dimensions.language} />
              <DimensionList title="Sources" items={report.dimensions.source} />
              <DimensionList title="Campaigns" items={report.dimensions.campaign} />
            </div>
          ) : null}
        </>
      )}

      <Card as="section" className="analytics-provenance-card">
        <h3>Data provenance</h3>
        <dl>
          <div><dt>Requested from</dt><dd>{report.requested_from}</dd></div>
          <div><dt>Effective from</dt><dd>{report.effective_from}</dd></div>
          <div><dt>Through</dt><dd>{report.data_through_at ?? 'No measured watermark'}</dd></div>
          <div><dt>State reason</dt><dd>{report.state_reason}</dd></div>
        </dl>
      </Card>
    </div>
  );
}
