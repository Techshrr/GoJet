import { useMemo, useState } from 'react';
import { Link } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { GoJetApiError } from '@gojet/api-client';
import { Button, Card, EmptyState, InlineMessage, TextField } from '@gojet/ui';
import { isReadOnly } from '../links/runtime';
import { createWorkspaceQRClient, readQRRuntime } from './runtime';

export function LinkQRPanel({ linkId }: { linkId: number }) {
  const runtime = useMemo(() => readQRRuntime(), []);
  const client = useMemo(() => runtime ? createWorkspaceQRClient(runtime) : null, [runtime]);
  const queryClient = useQueryClient();
  const readOnly = runtime ? isReadOnly(runtime) : true;
  const [label, setLabel] = useState('');

  const listQuery = useQuery({
    queryKey: ['qr-codes', runtime?.workspaceId],
    enabled: client !== null && runtime !== null,
    queryFn: () => client!.list(runtime!.workspaceId),
  });
  const createMutation = useMutation({
    mutationFn: async () => {
      if (!client || !runtime) throw new Error('QR authority unavailable');
      return client.create(runtime.workspaceId, {
        source_link_id: linkId,
        label: label.trim(),
        change_reason: 'Create QR from Link detail',
      });
    },
    onSuccess: async () => {
      setLabel('');
      await queryClient.invalidateQueries({ queryKey: ['qr-codes', runtime?.workspaceId] });
    },
  });

  const items = (listQuery.data?.items ?? []).filter((item) => item.source_link_id === linkId);
  const error = createMutation.error instanceof GoJetApiError ? createMutation.error : null;
  const quotaReached = listQuery.data?.quota.reached ?? false;

  return (
    <Card as="section" className="qr-link-panel">
      <div className="qr-card-heading">
        <div><p className="qr-resource-kicker">P08 QR</p><h2>QR resources</h2></div>
        {listQuery.data ? <span>{items.length} for this Link</span> : null}
      </div>
      <p>QR artifacts are derived from this GoJet public Link and remain subject to its live destination-risk and domain authority.</p>
      {!runtime ? <InlineMessage variant="warning">Workspace identity authority is unavailable.</InlineMessage> : null}
      {listQuery.isError ? <InlineMessage variant="danger">QR resources could not be loaded.</InlineMessage> : null}
      {error?.code === 'source_link_review' ? <InlineMessage variant="warning">This exact-current source fingerprint is under review. QR creation and distribution are denied.</InlineMessage> : null}
      {error?.code === 'source_link_block' ? <InlineMessage variant="danger">This source Link is blocked. No QR resource was created.</InlineMessage> : null}
      {error && error.code !== 'source_link_review' && error.code !== 'source_link_block' ? <InlineMessage variant="warning">{error.message} <strong>{error.code}</strong></InlineMessage> : null}
      {quotaReached ? <InlineMessage variant="warning">Workspace QR quota is reached. Existing resources remain visible, but creation is disabled.</InlineMessage> : null}

      {!readOnly && !quotaReached ? (
        <div className="qr-link-create">
          <TextField id={`qr-link-label-${linkId}`} label="New QR label" maxLength={120} value={label} onChange={(event) => setLabel(event.currentTarget.value)} placeholder="Optional label" />
          <Button loading={createMutation.isPending} onClick={() => createMutation.mutate()}>Create QR for this Link</Button>
        </div>
      ) : null}

      {listQuery.isPending ? <p role="status">Loading QR resources…</p> : null}
      {!listQuery.isPending && items.length === 0 ? <EmptyState title="No QR resources for this Link" reason="Create one only after the source Link has exact-current safety allow." /> : null}
      {items.length > 0 ? (
        <div className="qr-link-list">
          {items.map((item) => (
            <article key={item.id} className="qr-link-row">
              <div><strong>{item.label || `QR #${item.id}`}</strong><span>{item.state}</span></div>
              <Link to="/app/qr/$qrId" params={{ qrId: String(item.id) }} className="qr-primary-link">Open QR</Link>
            </article>
          ))}
        </div>
      ) : null}
    </Card>
  );
}
