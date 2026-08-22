import { type FormEvent, useMemo, useState } from 'react';
import { Link, useNavigate } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { GoJetApiError } from '@gojet/api-client';
import { Button, Card, EmptyState, InlineMessage, TextField } from '@gojet/ui';
import { WorkspaceShell } from '../shell/WorkspaceShell';
import { isReadOnly } from '../links/runtime';
import { createWorkspaceFilesClient, readFilesRuntime } from '../files/runtime';
import { FileState } from '../files/FileState';

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
}

export default function FilesListPage() {
  const runtime = useMemo(() => readFilesRuntime(), []);
  const client = useMemo(() => runtime ? createWorkspaceFilesClient(runtime) : null, [runtime]);
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const readOnly = runtime ? isReadOnly(runtime) : true;
  const [file, setFile] = useState<File | null>(null);
  const [changeReason, setChangeReason] = useState('Upload file to Workspace');

  const listQuery = useQuery({
    queryKey: ['files', runtime?.workspaceId],
    enabled: client !== null && runtime !== null,
    queryFn: () => client!.list(runtime!.workspaceId),
  });
  const uploadMutation = useMutation({
    mutationFn: async () => {
      if (!runtime || !client || !file) throw new Error('Choose a file to upload.');
      return client.upload(runtime.workspaceId, { file, change_reason: changeReason.trim() });
    },
    onSuccess: async (created) => {
      await queryClient.invalidateQueries({ queryKey: ['files', runtime?.workspaceId] });
      await navigate({ to: '/app/files/$fileId', params: { fileId: String(created.id) } });
    },
  });

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!readOnly && file && changeReason.trim()) uploadMutation.mutate();
  }

  const items = listQuery.data?.items ?? [];
  const apiError = uploadMutation.error instanceof GoJetApiError ? uploadMutation.error : null;
  const quotaReached = apiError?.code === 'quota_reached' || apiError?.status === 429;
  const pageState = uploadMutation.isPending ? 'uploading' : quotaReached ? 'quota-reached' : listQuery.isPending ? 'loading' : items.length === 0 ? 'empty' : 'ready';

  return (
    <WorkspaceShell state={readOnly && runtime ? 'read-only-role' : 'notification-attention'} sectionLabel="Files">
      <section className="files-page" data-page="files-list" data-state={pageState}>
        <header className="files-page-header">
          <div>
            <p className="files-eyebrow">Files</p>
            <h1>File sharing</h1>
            <p>Every upload remains private until the mandatory ClamAV pipeline returns a current clean verdict and an authorized user publishes it.</p>
          </div>
        </header>

        {!runtime ? <InlineMessage variant="warning">Workspace identity authority is unavailable in this build.</InlineMessage> : null}
        {listQuery.isError ? <InlineMessage variant="danger">Files could not be loaded from the authoritative Workspace API.</InlineMessage> : null}
        {quotaReached ? <InlineMessage variant="warning">Workspace file quota reached. New uploads remain disabled until capacity is available.</InlineMessage> : null}
        {apiError && !quotaReached ? <InlineMessage variant="danger">{apiError.message} <strong>{apiError.code}</strong></InlineMessage> : null}
        {uploadMutation.error && !apiError ? <InlineMessage variant="danger">{uploadMutation.error instanceof Error ? uploadMutation.error.message : 'Upload failed.'}</InlineMessage> : null}

        {!readOnly ? (
          <Card as="section" className="files-upload-card">
            <div><p className="files-kicker">Private quarantine</p><h2>Upload file</h2><p>The original filename is metadata only. GoJet assigns a randomized private storage identity before scanning.</p></div>
            <form className="files-upload-form" onSubmit={submit}>
              <label className="files-native-field" htmlFor="files-upload-input">
                File
                <input id="files-upload-input" type="file" required onChange={(event) => setFile(event.currentTarget.files?.[0] ?? null)} />
              </label>
              <TextField id="files-upload-reason" label="Change reason" required value={changeReason} onChange={(event) => setChangeReason(event.currentTarget.value)} />
              <Button type="submit" loading={uploadMutation.isPending} disabled={!file || !changeReason.trim() || quotaReached}>Upload privately</Button>
            </form>
            {uploadMutation.isPending ? <p role="status">Uploading into private quarantine…</p> : null}
          </Card>
        ) : null}

        {listQuery.isPending && runtime ? <p role="status">Loading files…</p> : null}
        {!listQuery.isPending && !listQuery.isError && items.length === 0 ? (
          <EmptyState title="No files yet" reason={readOnly ? 'This Workspace has no file resources available to your role.' : 'Upload a file to begin the private quarantine and security-scan workflow.'} />
        ) : null}

        {items.length > 0 ? (
          <div className="files-list" aria-label="Workspace files">
            {items.map((item) => (
              <Card as="article" className="files-card" key={item.id} data-scan-state={item.scan_state}>
                <div className="files-card-head">
                  <div><p className="files-kicker">File #{item.id}</p><h2 className="files-name">{item.original_name}</h2></div>
                  <FileState item={item} compact />
                </div>
                <dl className="files-meta">
                  <div><dt>Size</dt><dd>{formatBytes(item.size_bytes)}</dd></div>
                  <div><dt>Detected type</dt><dd>{item.detected_mime}</dd></div>
                  <div><dt>Generation</dt><dd>{item.scan_generation}</dd></div>
                  <div><dt>Distribution</dt><dd>{item.published ? 'Published' : 'Private'}</dd></div>
                </dl>
                <div className="files-actions">
                  <Link to="/app/files/$fileId" params={{ fileId: String(item.id) }} className="files-primary-link">Open file</Link>
                  {item.published && item.scan_state === 'safe' ? <a className="files-secondary-link" href={`/f/${encodeURIComponent(item.public_slug)}`}>Open public page</a> : null}
                </div>
              </Card>
            ))}
          </div>
        ) : null}
      </section>
    </WorkspaceShell>
  );
}
