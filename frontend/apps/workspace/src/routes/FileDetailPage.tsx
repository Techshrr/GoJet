import { useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { GoJetApiError } from '@gojet/api-client';
import { Button, Card, InlineMessage, TextField } from '@gojet/ui';
import { WorkspaceShell } from '../shell/WorkspaceShell';
import { isReadOnly } from '../links/runtime';
import { createWorkspaceFilesClient, readFilesRuntime } from '../files/runtime';
import { FileState } from '../files/FileState';

export default function FileDetailPage() {
  const { fileId } = useParams({ from: '/app/files/$fileId' });
  const numericId = Number(fileId);
  const runtime = useMemo(() => readFilesRuntime(), []);
  const client = useMemo(() => runtime ? createWorkspaceFilesClient(runtime) : null, [runtime]);
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const readOnly = runtime ? isReadOnly(runtime) : true;
  const [changeReason, setChangeReason] = useState('Update file lifecycle');

  const detailQuery = useQuery({
    queryKey: ['file', runtime?.workspaceId, numericId],
    enabled: client !== null && runtime !== null && Number.isSafeInteger(numericId) && numericId > 0,
    queryFn: () => client!.get(runtime!.workspaceId, numericId),
    retry: (count, error) => !(error instanceof GoJetApiError && (error.status === 404 || error.status === 410)) && count < 1,
  });

  const publishMutation = useMutation({
    mutationFn: () => client!.publish(runtime!.workspaceId, numericId, changeReason),
    onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: ['file', runtime?.workspaceId, numericId] }); await queryClient.invalidateQueries({ queryKey: ['files', runtime?.workspaceId] }); },
  });
  const rescanMutation = useMutation({
    mutationFn: () => client!.rescan(runtime!.workspaceId, numericId, changeReason),
    onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: ['file', runtime?.workspaceId, numericId] }); await queryClient.invalidateQueries({ queryKey: ['files', runtime?.workspaceId] }); },
  });
  const downloadMutation = useMutation({
    mutationFn: () => client!.download(runtime!.workspaceId, numericId),
    onSuccess: (artifact) => {
      const url = URL.createObjectURL(artifact.blob);
      const anchor = document.createElement('a'); anchor.href = url; anchor.download = artifact.filename; anchor.click(); URL.revokeObjectURL(url);
    },
  });
  const deleteMutation = useMutation({
    mutationFn: () => client!.remove(runtime!.workspaceId, numericId, changeReason),
    onSuccess: async () => { await queryClient.invalidateQueries({ queryKey: ['files', runtime?.workspaceId] }); await navigate({ to: '/app/files' }); },
  });

  const item = detailQuery.data;
  const detailError = detailQuery.error instanceof GoJetApiError ? detailQuery.error : null;
  const deleted = detailError?.status === 410 || detailError?.code === 'deleted';
  const actionError = [publishMutation.error, rescanMutation.error, downloadMutation.error, deleteMutation.error].find(Boolean);
  const actionApiError = actionError instanceof GoJetApiError ? actionError : null;
  const pageState = deleted ? 'deleted' : item?.scan_state ?? (detailQuery.isPending ? 'loading' : 'error');

  return (
    <WorkspaceShell state={readOnly && runtime ? 'read-only-role' : 'notification-attention'} sectionLabel="File detail">
      <section className="files-page" data-page="file-detail" data-state={pageState}>
        <header className="files-page-header">
          <div><p className="files-eyebrow">Files</p><h1 className="files-name">{item?.original_name ?? 'File detail'}</h1><p>Scan authority, publication and retention remain separate server-controlled states.</p></div>
          <Link to="/app/files" className="files-secondary-link">Back to files</Link>
        </header>
        {!runtime ? <InlineMessage variant="warning">Workspace identity authority is unavailable in this build.</InlineMessage> : null}
        {detailQuery.isPending && runtime ? <p role="status">Loading file resource…</p> : null}
        {deleted ? <InlineMessage variant="warning">This file was removed. New Workspace or public bytes are no longer available.</InlineMessage> : null}
        {detailQuery.isError && !deleted ? <InlineMessage variant="danger">The file resource could not be loaded from its authoritative Workspace.</InlineMessage> : null}
        {actionApiError ? <InlineMessage variant={actionApiError.status === 409 ? 'warning' : 'danger'}>{actionApiError.message} <strong>{actionApiError.code}</strong></InlineMessage> : null}
        {actionError && !actionApiError ? <InlineMessage variant="danger">The file action could not be completed.</InlineMessage> : null}

        {item ? (
          <div className="file-detail-grid">
            <Card as="section" className="file-detail-card">
              <div className="file-detail-heading"><div><p className="files-kicker">Security authority</p><h2>Current file state</h2></div></div>
              <FileState item={item} />
              <dl className="file-facts">
                <div><dt>File ID</dt><dd>{item.id}</dd></div><div><dt>Scan generation</dt><dd>{item.scan_generation}</dd></div>
                <div><dt>Detected MIME</dt><dd>{item.detected_mime}</dd></div><div><dt>Declared MIME</dt><dd>{item.declared_mime}</dd></div>
                <div><dt>Published</dt><dd>{item.published ? 'Yes' : 'No'}</dd></div><div><dt>Password</dt><dd>{item.password_required ? 'Required' : 'Not required'}</dd></div>
                <div><dt>Downloads</dt><dd>{item.download_count}{item.download_limit ? ` / ${item.download_limit}` : ''}</dd></div><div><dt>Updated</dt><dd>{new Date(item.updated_at).toLocaleString()}</dd></div>
              </dl>
              <p className="file-digest"><strong>SHA-256</strong><br />{item.content_sha256}</p>
              {item.published && item.scan_state === 'safe' ? <p className="file-public-path">Public page: <a href={`/f/${encodeURIComponent(item.public_slug)}`}>/f/{item.public_slug}</a></p> : null}
              <TextField id="file-action-reason" label="Change reason" required value={changeReason} onChange={(event) => setChangeReason(event.currentTarget.value)} />
              <div className="file-actions">
                <Button disabled={readOnly || item.scan_state !== 'safe' || item.published || !changeReason.trim()} loading={publishMutation.isPending} onClick={() => publishMutation.mutate()}>Publish safe file</Button>
                <Button variant="secondary" disabled={readOnly || !changeReason.trim()} loading={rescanMutation.isPending} onClick={() => rescanMutation.mutate()}>Rescan</Button>
                <Button variant="secondary" disabled={item.scan_state !== 'safe'} loading={downloadMutation.isPending} onClick={() => downloadMutation.mutate()}>Download Workspace copy</Button>
              </div>
            </Card>
            <Card as="section" className="file-detail-card file-danger">
              <p className="files-kicker">Lifecycle</p><h2>Remove file</h2>
              <p>Removal stops future GoJet distribution. It does not claim to revoke copies that a recipient already downloaded.</p>
              <Button variant="destructive" disabled={readOnly || !changeReason.trim()} loading={deleteMutation.isPending} onClick={() => deleteMutation.mutate()}>Delete file</Button>
            </Card>
          </div>
        ) : null}
      </section>
    </WorkspaceShell>
  );
}
