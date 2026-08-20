import { useMemo, useState } from 'react';
import { Link } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { BulkLinkAction, LinkListFilters, LinkRecord } from '@gojet/api-client';
import {
  Button,
  Card,
  Checkbox,
  DataTable,
  EmptyState,
  InlineMessage,
  SelectField,
  TextField,
  useShellViewport,
} from '@gojet/ui';
import { WorkspaceShell } from '../shell/WorkspaceShell';
import { createWorkspaceLinksClient, isReadOnly, readWorkspaceRuntime } from '../links/runtime';

function dateStart(value: string): string | undefined {
  return value ? new Date(`${value}T00:00:00Z`).toISOString() : undefined;
}

function dateEnd(value: string): string | undefined {
  return value ? new Date(`${value}T23:59:59Z`).toISOString() : undefined;
}

function LinkState({ link }: { link: LinkRecord }) {
  return <span className="links-state" data-state={link.status}>{link.status}</span>;
}

export default function LinksListPage() {
  const runtime = useMemo(() => readWorkspaceRuntime(), []);
  const client = useMemo(() => runtime ? createWorkspaceLinksClient(runtime) : null, [runtime]);
  const queryClient = useQueryClient();
  const viewport = useShellViewport();
  const [q, setQ] = useState('');
  const [hostname, setHostname] = useState('');
  const [status, setStatus] = useState('');
  const [fromDate, setFromDate] = useState('');
  const [toDate, setToDate] = useState('');
  const [selected, setSelected] = useState<Record<number, number>>({});
  const [exportState, setExportState] = useState<string | null>(null);

  const filters = useMemo<LinkListFilters>(() => {
    const next: LinkListFilters = { limit: 100, offset: 0 };
    if (q) next.q = q;
    if (hostname) next.hostname = hostname;
    if (status === 'active' || status === 'paused' || status === 'deleted') next.status = status;
    const updatedFrom = dateStart(fromDate);
    const updatedTo = dateEnd(toDate);
    if (updatedFrom) next.updated_from = updatedFrom;
    if (updatedTo) next.updated_to = updatedTo;
    return next;
  }, [q, hostname, status, fromDate, toDate]);

  const linksQuery = useQuery({
    queryKey: ['links', runtime?.workspaceId, filters],
    enabled: client !== null && runtime !== null,
    queryFn: () => client!.list(runtime!.workspaceId, filters),
  });

  const bulkMutation = useMutation({
    mutationFn: async (action: BulkLinkAction) => {
      if (!client || !runtime) throw new Error('Workspace authority unavailable');
      const items = Object.entries(selected).map(([id, version]) => ({ id: Number(id), version }));
      return client.bulk(runtime.workspaceId, action, items, `Bulk ${action} from Links list`);
    },
    onSuccess: async () => {
      setSelected({});
      await queryClient.invalidateQueries({ queryKey: ['links', runtime?.workspaceId] });
    },
  });

  const exportMutation = useMutation({
    mutationFn: async () => {
      if (!client || !runtime) throw new Error('Workspace authority unavailable');
      return client.exportCsv(runtime.workspaceId);
    },
    onSuccess: (csv) => {
      const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement('a');
      anchor.href = url;
      anchor.download = 'gojet-links.csv';
      anchor.click();
      URL.revokeObjectURL(url);
      setExportState('Export ready');
    },
  });

  const readOnly = runtime ? isReadOnly(runtime) : true;
  const items = linksQuery.data?.items ?? [];
  const selectedCount = Object.keys(selected).length;
  const filtered = Boolean(q || hostname || status || fromDate || toDate);

  function toggle(link: LinkRecord, checked: boolean) {
    setSelected((current) => {
      const next = { ...current };
      if (checked) next[link.id] = link.version;
      else delete next[link.id];
      return next;
    });
  }

  return (
    <WorkspaceShell state={readOnly && runtime ? 'read-only-role' : 'notification-attention'} sectionLabel="Links">
      <section className="links-page" data-page="links-list">
        <header className="links-page-header">
          <div>
            <p className="links-eyebrow">LINKS</p>
            <h1>Links</h1>
            <p>Manage destinations, routing controls and link lifecycle from one workspace.</p>
          </div>
          <div className="links-page-actions">
            <Button variant="outline" onClick={() => exportMutation.mutate()} disabled={!runtime || exportMutation.isPending}>Export</Button>
            <Link to="/app/links/new" className="links-primary-link" aria-disabled={readOnly}>Create link</Link>
          </div>
        </header>

        {!runtime ? <InlineMessage variant="warning">Workspace and authentication authority is unavailable in this build. P12/P15 must provide the production identity context.</InlineMessage> : null}
        {linksQuery.isError ? <InlineMessage variant="danger">Links could not be loaded from the Workspace API.</InlineMessage> : null}
        {bulkMutation.isError ? <InlineMessage variant="danger">The bulk request failed. Refresh the list before retrying.</InlineMessage> : null}
        {exportState ? <InlineMessage variant="success">{exportState}</InlineMessage> : null}

        <section className="links-filters" aria-label="Link filters">
          <TextField id="links-search" label="Search" value={q} onChange={(event) => setQ(event.currentTarget.value)} placeholder="Code, title or destination" />
          <TextField id="links-hostname" label="Domain" value={hostname} onChange={(event) => setHostname(event.currentTarget.value)} placeholder="go.example.com" />
          <SelectField id="links-status" label="Status" value={status} onChange={(event) => setStatus(event.currentTarget.value)} options={[
            { value: '', label: 'Active and paused' },
            { value: 'active', label: 'Active' },
            { value: 'paused', label: 'Paused' },
            { value: 'deleted', label: 'Deleted' },
          ]} />
          <TextField id="links-from" label="Updated from" type="date" value={fromDate} onChange={(event) => setFromDate(event.currentTarget.value)} />
          <TextField id="links-to" label="Updated to" type="date" value={toDate} onChange={(event) => setToDate(event.currentTarget.value)} />
          <SelectField id="links-campaign" label="Campaign" disabled value="" options={[{ value: '', label: 'Available with Campaigns' }]} helpText="Campaign filtering is owned by P07/P12 and is not fabricated in P05." />
          <SelectField id="links-tag" label="Tag" disabled value="" options={[{ value: '', label: 'Available with Tags' }]} helpText="Tag filtering is owned by P12 and remains unavailable in P05." />
        </section>

        <div className="links-summary" aria-live="polite">
          <strong>{linksQuery.data?.total ?? 0}</strong> links
          {filtered ? <span> · filtered</span> : null}
        </div>

        {selectedCount > 0 ? (
          <div className="links-selection" role="region" aria-label="Bulk link actions">
            <span>{selectedCount} selected</span>
            <Button variant="outline" disabled={readOnly || bulkMutation.isPending} onClick={() => bulkMutation.mutate('pause')}>Pause</Button>
            <Button variant="outline" disabled={readOnly || bulkMutation.isPending} onClick={() => bulkMutation.mutate('activate')}>Activate</Button>
            <Button variant="destructive" disabled={readOnly || bulkMutation.isPending} onClick={() => bulkMutation.mutate('delete')}>Delete</Button>
          </div>
        ) : null}

        {linksQuery.isPending && runtime ? <p role="status">Loading links…</p> : null}
        {!linksQuery.isPending && items.length === 0 ? (
          <EmptyState
            title={filtered ? 'No links match these filters' : 'No links yet'}
            reason={filtered ? 'Adjust the filters to broaden the result set.' : 'Create the first link for this workspace.'}
            action={!readOnly ? <Link to="/app/links/new" className="links-primary-link">Create link</Link> : undefined}
          />
        ) : null}

        {items.length > 0 && viewport !== 'mobile' ? (
          <DataTable caption="Workspace links">
            <thead><tr><th scope="col">Select</th><th scope="col">Link</th><th scope="col">Destination</th><th scope="col">Status</th><th scope="col">Clicks</th><th scope="col">Updated</th></tr></thead>
            <tbody>
              {items.map((link) => (
                <tr key={link.id}>
                  <td><Checkbox label={`Select ${link.code}`} checked={selected[link.id] !== undefined} onChange={(event) => toggle(link, event.currentTarget.checked)} disabled={readOnly} /></td>
                  <td><Link to="/app/links/$linkId" params={{ linkId: String(link.id) }}>{link.hostname}/{link.code}</Link><small>{link.title || 'Untitled link'}</small></td>
                  <td className="links-destination">{link.primary_destination}</td>
                  <td><LinkState link={link} /></td>
                  <td>{link.click_count}</td>
                  <td>{new Date(link.updated_at).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </DataTable>
        ) : null}

        {items.length > 0 && viewport === 'mobile' ? (
          <div className="links-resource-list" aria-label="Workspace links">
            {items.map((link) => (
              <Card as="article" key={link.id} className="links-resource-row">
                <div className="links-resource-row-head"><Link to="/app/links/$linkId" params={{ linkId: String(link.id) }}>{link.hostname}/{link.code}</Link><LinkState link={link} /></div>
                <p>{link.title || 'Untitled link'}</p>
                <p className="links-destination">{link.primary_destination}</p>
                <div className="links-resource-meta"><span>{link.click_count} clicks</span><Checkbox label="Select" checked={selected[link.id] !== undefined} onChange={(event) => toggle(link, event.currentTarget.checked)} disabled={readOnly} /></div>
              </Card>
            ))}
          </div>
        ) : null}
      </section>
    </WorkspaceShell>
  );
}
