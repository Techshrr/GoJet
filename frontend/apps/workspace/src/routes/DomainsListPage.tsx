import { useMemo } from 'react';
import { Link } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import type { WorkspaceDomainEntitlement, WorkspaceDomainRecord } from '@gojet/api-client';
import { Card, DataTable, EmptyState, InlineMessage, useShellViewport } from '@gojet/ui';
import { WorkspaceShell } from '../shell/WorkspaceShell';
import { createWorkspaceDomainsClient } from '../domains/runtime';
import { isReadOnly, readWorkspaceRuntime } from '../links/runtime';

function stateCopy(entitlement: WorkspaceDomainEntitlement): { variant: 'info' | 'warning' | 'danger' | 'success'; text: string } {
  switch (entitlement.state) {
    case 'requested': return { variant: 'info', text: `Custom-domain access was requested${entitlement.support_ticket_id ? ` under ticket ${entitlement.support_ticket_id}` : ''}. A request is not active entitlement; Add domain remains unavailable until approval.` };
    case 'grace': return { variant: 'warning', text: `Plan entitlement ended normally. Existing enabled routing remains available only until ${entitlement.deadline ? new Date(entitlement.deadline).toLocaleString() : 'the recorded grace deadline'}; new domain mutations and assignments are disabled now.` };
    case 'suspended': return { variant: 'danger', text: `Custom-domain authority is suspended${entitlement.security_category ? ` (${entitlement.security_category})` : ''}. Routing and mutations fail closed with no billing grace.` };
    case 'revoked': return { variant: 'danger', text: 'Custom-domain authority is revoked. Existing custom-host routing and all mutations fail closed.' };
    case 'expired': return { variant: 'warning', text: 'The previous custom-domain entitlement has expired and no current source is valid. Add domain is unavailable.' };
    case 'locked': return { variant: 'info', text: 'This Workspace has no active Business-plan or recorded manual custom-domain approval. Support requests alone do not grant entitlement.' };
    case 'partial-axis': return { variant: 'warning', text: 'Entitlement is active, but one or more domains still have non-ready Ownership, DNS, HTTPS or Risk authority. The affected state remains visible below.' };
    default: return { variant: 'success', text: 'Custom-domain entitlement is active. New domains are allowed while server-enforced capacity remains.' };
  }
}

function axisLabel(label: string, value: string) {
  return <span className="domains-axis" data-state={value}><span>{label}</span><strong>{value}</strong></span>;
}

function DomainState({ domain }: { domain: WorkspaceDomainRecord }) {
  return (
    <div className="domains-axis-group" aria-label={`Authority for ${domain.hostname}`}>
      {axisLabel('Ownership', domain.ownership_status)}
      {axisLabel('DNS', domain.ingress_dns_status)}
      {axisLabel('HTTPS', domain.https_status)}
      {axisLabel('Risk', domain.risk_status)}
    </div>
  );
}

export default function DomainsListPage() {
  const runtime = useMemo(() => readWorkspaceRuntime(), []);
  const client = useMemo(() => runtime ? createWorkspaceDomainsClient(runtime) : null, [runtime]);
  const readOnly = runtime ? isReadOnly(runtime) : true;
  const viewport = useShellViewport();
  const query = useQuery({
    queryKey: ['domains', runtime?.workspaceId],
    enabled: Boolean(client && runtime),
    queryFn: () => client!.list(runtime!.workspaceId),
  });

  const entitlement = query.data?.entitlement;
  const items = query.data?.items ?? [];
  const notice = entitlement ? stateCopy(entitlement) : null;
  const canAdd = Boolean(entitlement?.mutation_allowed && entitlement.remaining > 0 && !readOnly);

  return (
    <WorkspaceShell state={readOnly && runtime ? 'read-only-role' : 'notification-attention'} sectionLabel="Domains">
      <section className="domains-page" data-page="domains-list">
        <header className="domains-page-header">
          <div>
            <p className="domains-eyebrow">DOMAINS</p>
            <h1>Custom domains</h1>
            <p>Ownership, ingress DNS, HTTPS and domain risk stay independent. A green-looking hostname never collapses these authorities into one flag.</p>
          </div>
          {canAdd ? <Link to="/app/domains/new" className="domains-primary-link">Add domain</Link> : null}
        </header>

        {!runtime ? <InlineMessage variant="warning">Production Workspace identity is unavailable until P12/P15 provides authoritative authentication context.</InlineMessage> : null}
        {query.isError ? <InlineMessage variant="danger">Domain authority could not be loaded from the Workspace API. No local fallback state is used.</InlineMessage> : null}
        {notice ? <InlineMessage variant={notice.variant}>{notice.text}</InlineMessage> : null}

        {entitlement ? (
          <Card as="section" className="domains-entitlement" aria-labelledby="domains-entitlement-title">
            <div><p className="domains-eyebrow">ENTITLEMENT</p><h2 id="domains-entitlement-title">Workspace authority</h2></div>
            <dl className="domains-kv-grid">
              <div><dt>State</dt><dd><strong>{entitlement.state}</strong></dd></div>
              <div><dt>Source</dt><dd>{entitlement.source}</dd></div>
              <div><dt>Domain limit</dt><dd>{entitlement.domain_limit}</dd></div>
              <div><dt>Allocated</dt><dd>{entitlement.allocated}</dd></div>
              <div><dt>Remaining</dt><dd>{entitlement.remaining}</dd></div>
              <div><dt>New mutations</dt><dd>{entitlement.mutation_allowed ? 'Allowed' : 'Denied'}</dd></div>
              <div><dt>Existing routing</dt><dd>{entitlement.existing_routing_allowed ? 'Allowed' : 'Denied'}</dd></div>
              {entitlement.deadline ? <div><dt>Deadline</dt><dd>{new Date(entitlement.deadline).toLocaleString()}</dd></div> : null}
            </dl>
          </Card>
        ) : null}

        {query.isPending && runtime ? <p role="status">Loading domain authority…</p> : null}
        {!query.isPending && query.data && items.length === 0 ? (
          <EmptyState
            title={canAdd ? 'No custom domains yet' : 'No custom domains available'}
            reason={canAdd ? 'Add a hostname to begin the seven-step authority flow.' : 'The current Workspace authority does not permit creating a domain.'}
            action={canAdd ? <Link to="/app/domains/new" className="domains-primary-link">Add your first domain</Link> : undefined}
          />
        ) : null}

        {items.length > 0 && viewport !== 'mobile' ? (
          <DataTable caption="Workspace custom domains">
            <thead><tr><th scope="col">Domain</th><th scope="col">Routing</th><th scope="col">Authority axes</th><th scope="col">New links</th><th scope="col">Updated</th></tr></thead>
            <tbody>{items.map((domain) => (
              <tr key={domain.id}>
                <td><Link to="/app/domains/$domainId" params={{ domainId: String(domain.id) }}>{domain.display_hostname || domain.hostname}</Link></td>
                <td><span className="domains-state" data-state={domain.routing_state}>{domain.routing_state}</span></td>
                <td><DomainState domain={domain} /></td>
                <td>{domain.ready_for_new_links ? 'Ready' : 'Not ready'}</td>
                <td>{new Date(domain.updated_at).toLocaleString()}</td>
              </tr>
            ))}</tbody>
          </DataTable>
        ) : null}

        {items.length > 0 && viewport === 'mobile' ? (
          <div className="domains-resource-list" aria-label="Workspace custom domains">
            {items.map((domain) => (
              <Card as="article" className="domains-resource-card" key={domain.id}>
                <div className="domains-resource-head"><Link to="/app/domains/$domainId" params={{ domainId: String(domain.id) }}>{domain.display_hostname || domain.hostname}</Link><span className="domains-state" data-state={domain.routing_state}>{domain.routing_state}</span></div>
                <DomainState domain={domain} />
                <p><strong>New links:</strong> {domain.ready_for_new_links ? 'Ready' : 'Not ready'}</p>
              </Card>
            ))}
          </div>
        ) : null}
      </section>
    </WorkspaceShell>
  );
}
