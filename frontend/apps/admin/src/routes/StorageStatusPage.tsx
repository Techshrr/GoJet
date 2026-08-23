import { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { GoJetFilesClient } from '@gojet/api-client';
import { Card, InlineMessage } from '@gojet/ui';
import { AdminShell } from '../shell/AdminShell';

function readAdminHealthClient(): GoJetFilesClient | null {
  const enabled = import.meta.env.VITE_GOJET_TEST_AUTH_ENABLED === '1';
  const actor = String(import.meta.env.VITE_GOJET_TEST_ACTOR_ID ?? '').trim();
  const role = String(import.meta.env.VITE_GOJET_TEST_ADMIN_ROLE ?? '').trim().toLowerCase();
  if (!enabled || !actor || role !== 'admin') return null;
  return new GoJetFilesClient({
    headers: () => ({
      'X-GoJet-Test-Actor': actor,
      'X-GoJet-Test-Admin-Role': role,
    }),
  });
}

export default function StorageStatusPage() {
  const client = useMemo(() => readAdminHealthClient(), []);
  const healthQuery = useQuery({
    queryKey: ['admin-files-storage-health'],
    enabled: client !== null,
    queryFn: () => client!.health(),
    retry: false,
  });
  const report = healthQuery.data;
  const pageState = !client ? 'auth-unavailable' : healthQuery.isPending ? 'loading' : healthQuery.isError ? 'unavailable' : report?.status ?? 'unavailable';
  const shellState = !client ? 'admin-auth-required' : report && !report.ready ? 'partial-service-degradation' : 'normal';

  return (
    <AdminShell state={shellState}>
      <section data-page="admin-storage" data-state={pageState}>
        <p>Platform / Storage</p>
        <h1>Files storage and ClamAV</h1>
        <p>P09 dependency status only. P17 administrator completion and P22 installation closure remain later-owned.</p>
        {!client ? <InlineMessage variant="warning">Administrator authentication dependency is not available in this implementation stage.</InlineMessage> : null}
        {healthQuery.isPending && client ? <p role="status">Checking mandatory file dependencies…</p> : null}
        {healthQuery.isError ? <InlineMessage variant="danger">File dependency health is unavailable. Treat file publication readiness as degraded.</InlineMessage> : null}
        {report ? (
          <div>
            <InlineMessage variant={report.ready ? 'success' : 'warning'}>
              Mandatory Files dependencies: <strong>{report.ready ? 'Ready' : 'Not ready'}</strong> · {report.status}
            </InlineMessage>
            <Card as="section">
              <h2>Native storage</h2>
              <dl>
                <div><dt>Status</dt><dd>{report.storage.state}</dd></div>
                <div><dt>Writable</dt><dd>{report.storage.writable ? 'Yes' : 'No'}</dd></div>
              </dl>
              <p>Private filesystem locations are intentionally not exposed here.</p>
            </Card>
            <Card as="section">
              <h2>ClamAV</h2>
              <dl>
                <div><dt>Status</dt><dd>{report.clamav.state}</dd></div>
                <div><dt>Engine</dt><dd>{report.clamav.engine_version ?? 'Unavailable'}</dd></div>
                <div><dt>Signature version</dt><dd>{report.clamav.signature_version ?? 'Unavailable'}</dd></div>
                <div><dt>Signature date</dt><dd>{report.clamav.signature_date ? new Date(report.clamav.signature_date).toLocaleString() : 'Unavailable'}</dd></div>
                <div><dt>Checked</dt><dd>{new Date(report.clamav.checked_at).toLocaleString()}</dd></div>
              </dl>
              <p>Scanner socket/address details and bypass information are intentionally not exposed.</p>
            </Card>
          </div>
        ) : null}
      </section>
    </AdminShell>
  );
}
