import { type FormEvent, useMemo, useState } from 'react';
import { Link, useNavigate } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { GoJetApiError, type QRRecord } from '@gojet/api-client';
import { Button, Card, EmptyState, InlineMessage, SelectField, TextField, useShellViewport } from '@gojet/ui';
import { WorkspaceShell } from '../shell/WorkspaceShell';
import { createWorkspaceLinksClient, isReadOnly } from '../links/runtime';
import { createWorkspaceQRClient, readQRRuntime } from '../qr/runtime';

function QRStateBadge({ item }: { item: QRRecord }) {
  const label = item.state === 'ready' ? 'Ready' : item.state === 'source-link-review' ? 'Source under review' : 'Source blocked';
  return <span className="qr-state" data-state={item.state}>{label}</span>;
}

export default function QRListPage() {
  const runtime = useMemo(() => readQRRuntime(), []);
  const qrClient = useMemo(() => runtime ? createWorkspaceQRClient(runtime) : null, [runtime]);
  const linksClient = useMemo(() => runtime ? createWorkspaceLinksClient(runtime) : null, [runtime]);
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const viewport = useShellViewport();
  const readOnly = runtime ? isReadOnly(runtime) : true;
  const [showCreate, setShowCreate] = useState(false);
  const [sourceLinkId, setSourceLinkId] = useState('');
  const [label, setLabel] = useState('');

  const qrQuery = useQuery({
    queryKey: ['qr-codes', runtime?.workspaceId],
    enabled: qrClient !== null && runtime !== null,
    queryFn: () => qrClient!.list(runtime!.workspaceId),
  });
  const linksQuery = useQuery({
    queryKey: ['links', runtime?.workspaceId, 'qr-source-picker'],
    enabled: showCreate && linksClient !== null && runtime !== null,
    queryFn: () => linksClient!.list(runtime!.workspaceId, { status: 'active', limit: 200, offset: 0 }),
  });

  const createMutation = useMutation({
    mutationFn: async () => {
      if (!runtime || !qrClient) throw new Error('QR authority unavailable');
      const numericSource = Number(sourceLinkId);
      if (!Number.isSafeInteger(numericSource) || numericSource <= 0) throw new Error('Choose a source Link');
      return qrClient.create(runtime.workspaceId, {
        source_link_id: numericSource,
        label: label.trim(),
        change_reason: 'Create QR from Workspace',
      });
    },
    onSuccess: async (created) => {
      await queryClient.invalidateQueries({ queryKey: ['qr-codes', runtime?.workspaceId] });
      await navigate({ to: '/app/qr/$qrId', params: { qrId: String(created.id) } });
    },
  });

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!readOnly && !qrQuery.data?.quota.reached) createMutation.mutate();
  }

  const apiError = createMutation.error instanceof GoJetApiError ? createMutation.error : null;
  const items = qrQuery.data?.items ?? [];
  const quota = qrQuery.data?.quota;
  const quotaReached = quota?.reached ?? false;
  const sourceOptions = (linksQuery.data?.items ?? []).map((link) => ({
    value: String(link.id),
    label: `${link.hostname}/${link.code}${link.title ? ` · ${link.title}` : ''}`,
  }));

  return (
    <WorkspaceShell state={readOnly && runtime ? 'read-only-role' : 'notification-attention'} sectionLabel="QR">
      <section className="qr-page" data-page="qr-list">
        <header className="qr-page-header">
          <div>
            <p className="qr-eyebrow">QR</p>
            <h1>QR codes</h1>
            <p>Create scannable resources that remain bound to the live safety authority of a GoJet Link.</p>
          </div>
          <Button onClick={() => setShowCreate((value) => !value)} disabled={readOnly || quotaReached}>
            {showCreate ? 'Close create form' : 'Create QR'}
          </Button>
        </header>

        {!runtime ? <InlineMessage variant="warning">Workspace identity authority is unavailable in this build.</InlineMessage> : null}
        {qrQuery.isError ? <InlineMessage variant="danger">QR resources could not be loaded from the Workspace API.</InlineMessage> : null}
        {quota ? <InlineMessage variant={quotaReached ? 'warning' : 'info'}>QR quota: <strong>{quota.used} / {quota.limit}</strong>{quotaReached ? ' — quota reached; creation is disabled.' : ''}</InlineMessage> : null}
        {apiError?.code === 'source_link_review' ? <InlineMessage variant="warning">The selected source Link is under review. QR generation and distribution remain denied until its exact-current fingerprint is allowed.</InlineMessage> : null}
        {apiError?.code === 'source_link_block' ? <InlineMessage variant="danger">The selected source Link is blocked. No QR resource was created.</InlineMessage> : null}
        {apiError && apiError.code !== 'source_link_review' && apiError.code !== 'source_link_block' ? <InlineMessage variant={apiError.status === 429 || apiError.status === 409 ? 'warning' : 'danger'}>{apiError.message} <strong>{apiError.code}</strong></InlineMessage> : null}
        {createMutation.error && !apiError ? <InlineMessage variant="danger">{createMutation.error instanceof Error ? createMutation.error.message : 'QR creation failed.'}</InlineMessage> : null}

        {showCreate && !readOnly && !quotaReached ? (
          <Card as="section" className="qr-create-card">
            <h2>Create QR</h2>
            <p>The encoded value is derived by the server from the selected GoJet Link. Raw destination URLs are not accepted here.</p>
            <form className="qr-create-form" onSubmit={submit}>
              <SelectField
                id="qr-source-link"
                label="Source Link"
                required
                value={sourceLinkId}
                onChange={(event) => setSourceLinkId(event.currentTarget.value)}
                options={[{ value: '', label: linksQuery.isPending ? 'Loading Links…' : 'Choose an active Link' }, ...sourceOptions]}
                helpText="The source must have exact-current safety allow and any applicable custom-domain authority."
              />
              <TextField id="qr-label" label="Label" maxLength={120} value={label} onChange={(event) => setLabel(event.currentTarget.value)} placeholder="Campaign poster" />
              <Button type="submit" loading={createMutation.isPending} disabled={!sourceLinkId || linksQuery.isPending}>Create QR</Button>
            </form>
          </Card>
        ) : null}

        {qrQuery.isPending && runtime ? <p role="status">Loading QR resources…</p> : null}
        {!qrQuery.isPending && !qrQuery.isError && items.length === 0 ? (
          <EmptyState
            title="No QR codes yet"
            reason={readOnly ? 'This Workspace has no QR resources available to your role.' : 'Create a QR from an active, safety-allowed GoJet Link.'}
            action={!readOnly && !quotaReached ? <Button onClick={() => setShowCreate(true)}>Create QR</Button> : undefined}
          />
        ) : null}

        {items.length > 0 ? (
          <div className={viewport === 'mobile' ? 'qr-resource-list qr-resource-list--mobile' : 'qr-resource-list'} aria-label="Workspace QR resources">
            {items.map((item) => (
              <Card as="article" className="qr-resource" key={item.id}>
                <div className="qr-resource-head">
                  <div>
                    <p className="qr-resource-kicker">QR #{item.id}</p>
                    <h2>{item.label || `${item.source.hostname ?? 'Link'}/${item.source.code ?? item.source_link_id}`}</h2>
                  </div>
                  <QRStateBadge item={item} />
                </div>
                <p className="qr-source-url">{item.source.public_url ?? `${item.source.hostname ?? 'source'}/${item.source.code ?? item.source_link_id}`}</p>
                <p>Safety: <strong>{item.source.reason}</strong></p>
                <div className="qr-resource-actions">
                  <Link to="/app/qr/$qrId" params={{ qrId: String(item.id) }} className="qr-primary-link">Open QR</Link>
                  <Link to="/app/links/$linkId" params={{ linkId: String(item.source_link_id) }} className="qr-secondary-link">Open source Link</Link>
                </div>
              </Card>
            ))}
          </div>
        ) : null}
      </section>
    </WorkspaceShell>
  );
}
