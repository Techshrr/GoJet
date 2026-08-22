import { useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { GoJetApiError, type QRArtifact } from '@gojet/api-client';
import { Button, Card, InlineMessage, SelectField, TextField } from '@gojet/ui';
import { WorkspaceShell } from '../shell/WorkspaceShell';
import { isReadOnly } from '../links/runtime';
import { createWorkspaceQRClient, readQRRuntime } from '../qr/runtime';

function artifactUrl(artifact: QRArtifact | undefined): string | null {
  if (!artifact) return null;
  return URL.createObjectURL(artifact.blob);
}

export default function QRDetailPage() {
  const { qrId } = useParams({ from: '/app/qr/$qrId' });
  const numericId = Number(qrId);
  const runtime = useMemo(() => readQRRuntime(), []);
  const client = useMemo(() => runtime ? createWorkspaceQRClient(runtime) : null, [runtime]);
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const readOnly = runtime ? isReadOnly(runtime) : true;
  const [format, setFormat] = useState<'png' | 'svg'>('png');
  const [previewUrl, setPreviewUrl] = useState<string | null>(null);
  const [changeReason, setChangeReason] = useState('Delete QR resource');

  const detailQuery = useQuery({
    queryKey: ['qr-code', runtime?.workspaceId, numericId],
    enabled: client !== null && runtime !== null && Number.isSafeInteger(numericId) && numericId > 0,
    queryFn: () => client!.get(runtime!.workspaceId, numericId),
    retry: (count, error) => !(error instanceof GoJetApiError && (error.status === 404 || error.status === 410)) && count < 1,
  });
  const ready = detailQuery.data?.state === 'ready';
  const previewQuery = useQuery({
    queryKey: ['qr-preview', runtime?.workspaceId, numericId, format, detailQuery.data?.source.reason],
    enabled: ready && client !== null && runtime !== null,
    queryFn: () => client!.artifact(runtime!.workspaceId, numericId, format, false),
    retry: false,
  });

  useEffect(() => {
    const next = artifactUrl(previewQuery.data);
    setPreviewUrl(next);
    return () => { if (next) URL.revokeObjectURL(next); };
  }, [previewQuery.data]);

  const downloadMutation = useMutation({
    mutationFn: async () => {
      if (!client || !runtime) throw new Error('QR authority unavailable');
      return client.artifact(runtime.workspaceId, numericId, format, true);
    },
    onSuccess: (artifact) => {
      const url = URL.createObjectURL(artifact.blob);
      const anchor = document.createElement('a');
      anchor.href = url;
      anchor.download = artifact.filename;
      anchor.click();
      URL.revokeObjectURL(url);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: async () => {
      if (!client || !runtime) throw new Error('QR authority unavailable');
      return client.remove(runtime.workspaceId, numericId, changeReason);
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['qr-codes', runtime?.workspaceId] });
      await navigate({ to: '/app/qr' });
    },
  });

  const detailError = detailQuery.error instanceof GoJetApiError ? detailQuery.error : null;
  const previewError = previewQuery.error instanceof GoJetApiError ? previewQuery.error : null;
  const deleted = detailError?.status === 410 || detailError?.code === 'deleted';
  const item = detailQuery.data;

  return (
    <WorkspaceShell state={readOnly && runtime ? 'read-only-role' : 'notification-attention'} sectionLabel="QR detail">
      <section className="qr-page" data-page="qr-detail" data-state={deleted ? 'deleted' : item?.state ?? (detailQuery.isPending ? 'loading' : 'error')}>
        <header className="qr-page-header">
          <div>
            <p className="qr-eyebrow">QR</p>
            <h1>{item?.label || (item ? `QR #${item.id}` : 'QR detail')}</h1>
            <p>{item?.source.public_url ?? 'Preview, download and lifecycle controls.'}</p>
          </div>
          <Link to="/app/qr" className="qr-secondary-link">Back to QR codes</Link>
        </header>

        {!runtime ? <InlineMessage variant="warning">Workspace identity authority is unavailable in this build.</InlineMessage> : null}
        {detailQuery.isPending && runtime ? <p role="status">Loading QR resource…</p> : null}
        {deleted ? <InlineMessage variant="warning">This QR resource was deleted. New preview and download artifacts are no longer available.</InlineMessage> : null}
        {detailQuery.isError && !deleted ? <InlineMessage variant="danger">The QR resource could not be loaded from its authoritative Workspace.</InlineMessage> : null}

        {item?.state === 'source-link-review' ? (
          <InlineMessage variant="warning">The source Link is under safety review. Preview and download are denied until the exact-current source fingerprint is allowed.</InlineMessage>
        ) : null}
        {item?.state === 'source-link-block' ? (
          <InlineMessage variant="danger">The source Link is not currently eligible for QR distribution. Preview and download remain blocked.</InlineMessage>
        ) : null}
        {previewError ? <InlineMessage variant={previewError.code === 'source_link_block' ? 'danger' : 'warning'}>{previewError.message} <strong>{previewError.code}</strong></InlineMessage> : null}
        {downloadMutation.error instanceof GoJetApiError ? <InlineMessage variant="warning">{downloadMutation.error.message} <strong>{downloadMutation.error.code}</strong></InlineMessage> : null}
        {deleteMutation.isError ? <InlineMessage variant="danger">The QR resource could not be deleted.</InlineMessage> : null}

        {item ? (
          <div className="qr-detail-grid">
            <Card as="section" className="qr-preview-card">
              <div className="qr-card-heading">
                <div><p className="qr-resource-kicker">Generated artifact</p><h2>Preview</h2></div>
                <span className="qr-state" data-state={item.state}>{item.state === 'ready' ? 'Ready' : item.state === 'source-link-review' ? 'Source under review' : 'Source blocked'}</span>
              </div>
              {ready && previewQuery.isPending ? <p role="status">Generating QR preview…</p> : null}
              {ready && previewUrl ? <img className="qr-preview-image" src={previewUrl} alt={`QR code for ${item.source.public_url ?? 'the source Link'}`} /> : null}
              {!ready ? <div className="qr-preview-denied" role="status">QR artifact distribution is unavailable in this safety state.</div> : null}
              {previewQuery.data?.sha256 ? <p className="qr-digest"><span>SHA-256</span><code>{previewQuery.data.sha256}</code></p> : null}
              <div className="qr-artifact-controls">
                <SelectField id="qr-format" label="Format" value={format} onChange={(event) => setFormat(event.currentTarget.value as 'png' | 'svg')} options={[{ value: 'png', label: 'PNG' }, { value: 'svg', label: 'SVG' }]} />
                <Button disabled={!ready || previewQuery.isPending || downloadMutation.isPending} loading={downloadMutation.isPending} onClick={() => downloadMutation.mutate()}>Download {format.toUpperCase()}</Button>
              </div>
            </Card>

            <Card as="section" className="qr-authority-card">
              <p className="qr-resource-kicker">Source authority</p>
              <h2>Bound GoJet Link</h2>
              <dl className="qr-facts">
                <div><dt>QR ID</dt><dd>{item.id}</dd></div>
                <div><dt>Source Link</dt><dd>#{item.source_link_id}</dd></div>
                <div><dt>Risk state</dt><dd>{item.source.risk_state}</dd></div>
                <div><dt>Authority reason</dt><dd>{item.source.reason}</dd></div>
                <div><dt>Updated</dt><dd>{new Date(item.updated_at).toLocaleString()}</dd></div>
              </dl>
              <p>The QR payload is the GoJet public short URL. The customer destination remains behind the live redirect, domain and destination-risk authority.</p>
              <Link to="/app/links/$linkId" params={{ linkId: String(item.source_link_id) }} className="qr-secondary-link">Open source Link</Link>
            </Card>

            <Card as="section" className="qr-delete-card">
              <p className="qr-resource-kicker">Lifecycle</p>
              <h2>Delete QR</h2>
              <p>Deletion prevents future preview/download generation for this QR resource. It does not falsely claim to revoke media that was already downloaded.</p>
              <TextField id="qr-delete-reason" label="Change reason" required value={changeReason} onChange={(event) => setChangeReason(event.currentTarget.value)} />
              <Button variant="destructive" disabled={readOnly || !changeReason.trim() || deleteMutation.isPending} loading={deleteMutation.isPending} onClick={() => deleteMutation.mutate()}>Delete QR</Button>
            </Card>
          </div>
        ) : null}
      </section>
    </WorkspaceShell>
  );
}
