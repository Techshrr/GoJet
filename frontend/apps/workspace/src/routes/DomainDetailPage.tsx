import { useMemo, useState } from 'react';
import { Link, useParams } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import type { WorkspaceDomainDetailResponse, WorkspaceDomainRecord } from '@gojet/api-client';
import { Card, DataTable, EmptyState, InlineMessage, Tabs } from '@gojet/ui';
import { WorkspaceShell } from '../shell/WorkspaceShell';
import { createWorkspaceDomainsClient } from '../domains/runtime';
import { isReadOnly, readWorkspaceRuntime } from '../links/runtime';

const tabs = [
  { id: 'overview', label: 'Overview' },
  { id: 'entitlement', label: 'Entitlement' },
  { id: 'ownership', label: 'Ownership' },
  { id: 'dns', label: 'DNS' },
  { id: 'https', label: 'HTTPS' },
  { id: 'risk', label: 'Risk' },
  { id: 'resources', label: 'Assigned resources' },
  { id: 'revalidation', label: 'Revalidation' },
  { id: 'settings', label: 'Settings' },
] as const;

type TabId = typeof tabs[number]['id'];

function Axis({ label, value, ready, checkedAt }: { label: string; value: string; ready: boolean; checkedAt: string | undefined }) {
  return (
    <Card as="section" className="domains-axis-card" data-axis={label.toLowerCase().replaceAll(' ', '-')} data-ready={ready ? 'true' : 'false'}>
      <div><h3>{label}</h3><span className="domains-state" data-state={value}>{value}</span></div>
      <p><strong>{ready ? 'Ready' : 'Not ready'}</strong>{checkedAt ? ` · checked ${new Date(checkedAt).toLocaleString()}` : ' · no current successful check recorded'}</p>
    </Card>
  );
}

function PersistentProblems({ detail }: { detail: WorkspaceDomainDetailResponse }) {
  const domain = detail.domain;
  const entitlement = detail.entitlement;
  const messages: Array<{ variant: 'warning' | 'danger'; text: string }> = [];
  if (!entitlement.mutation_allowed) {
    messages.push({ variant: entitlement.state === 'suspended' || entitlement.state === 'revoked' ? 'danger' : 'warning', text: `Entitlement state is ${entitlement.state}. New domain mutations and Link assignments are denied.` });
  }
  if (domain.ownership_status !== 'verified') messages.push({ variant: 'danger', text: `Ownership is ${domain.ownership_status}. A verified current TXT proof is required.` });
  if (domain.ingress_dns_status !== 'valid') messages.push({ variant: 'warning', text: `Ingress DNS is ${domain.ingress_dns_status}. Current CNAME authority must be valid.` });
  if (domain.https_status !== 'active') messages.push({ variant: 'warning', text: `HTTPS is ${domain.https_status}. A current successful TLS/hostname check is required.` });
  if (domain.risk_status !== 'allow') messages.push({ variant: 'danger', text: `Domain risk is ${domain.risk_status}. Only a current allow decision can satisfy this axis.` });
  if (domain.routing_state === 'suspended' || domain.routing_state === 'revoked' || domain.routing_state === 'removed') messages.push({ variant: 'danger', text: `Routing state is ${domain.routing_state}. Custom-host redirect fails closed.` });
  return <>{messages.map((message) => <InlineMessage key={message.text} variant={message.variant}>{message.text}</InlineMessage>)}</>;
}

function Overview({ domain }: { domain: WorkspaceDomainRecord }) {
  return (
    <>
      <Card as="section" className="domains-detail-card">
        <h2>Overview</h2>
        <dl className="domains-kv-grid">
          <div><dt>Hostname</dt><dd>{domain.display_hostname || domain.hostname}</dd></div>
          <div><dt>Routing state</dt><dd>{domain.routing_state}</dd></div>
          <div><dt>Ready for new links</dt><dd>{domain.ready_for_new_links ? 'Yes' : 'No'}</dd></div>
          <div><dt>Ready for routing</dt><dd>{domain.ready_for_routing ? 'Yes' : 'No'}</dd></div>
          <div><dt>Ownership token version</dt><dd>{domain.ownership_token_version}</dd></div>
          <div><dt>Created</dt><dd>{new Date(domain.created_at).toLocaleString()}</dd></div>
        </dl>
      </Card>
      <div className="domains-axis-cards">
        <Axis label="Ownership" value={domain.ownership_status} ready={domain.ownership_status === 'verified'} checkedAt={domain.ownership_verified_at} />
        <Axis label="Ingress DNS" value={domain.ingress_dns_status} ready={domain.ingress_dns_status === 'valid'} checkedAt={domain.ingress_dns_checked_at} />
        <Axis label="HTTPS" value={domain.https_status} ready={domain.https_status === 'active'} checkedAt={domain.https_checked_at} />
        <Axis label="Domain risk" value={domain.risk_status} ready={domain.risk_status === 'allow'} checkedAt={domain.risk_checked_at} />
      </div>
    </>
  );
}

export default function DomainDetailPage() {
  const { domainId } = useParams({ from: '/app/domains/$domainId' });
  const numericId = Number(domainId);
  const runtime = useMemo(() => readWorkspaceRuntime(), []);
  const client = useMemo(() => runtime ? createWorkspaceDomainsClient(runtime) : null, [runtime]);
  const readOnly = runtime ? isReadOnly(runtime) : true;
  const [activeTab, setActiveTab] = useState<TabId>('overview');
  const query = useQuery({
    queryKey: ['domain', runtime?.workspaceId, numericId],
    enabled: Boolean(client && runtime && Number.isSafeInteger(numericId) && numericId > 0),
    queryFn: () => client!.get(runtime!.workspaceId, numericId),
  });
  const detail = query.data;
  const domain = detail?.domain;

  return (
    <WorkspaceShell state={readOnly && runtime ? 'read-only-role' : 'notification-attention'} sectionLabel="Domain detail">
      <section className="domains-page" data-page="domain-detail">
        <header className="domains-page-header">
          <div><p className="domains-eyebrow">DOMAINS</p><h1>{domain?.display_hostname || domain?.hostname || 'Domain detail'}</h1><p>Every authority remains visible independently. Secret verifier material and provider evidence are deliberately excluded.</p></div>
          <Link to="/app/domains" className="domains-secondary-link">Back to domains</Link>
        </header>

        {!runtime ? <InlineMessage variant="warning">Production Workspace identity is unavailable until P12/P15 provides authoritative authentication context.</InlineMessage> : null}
        {query.isPending && runtime ? <p role="status">Loading domain authority…</p> : null}
        {query.isError ? <InlineMessage variant="danger">This custom domain could not be loaded. No cached local authority state is shown as current.</InlineMessage> : null}
        {detail ? <PersistentProblems detail={detail} /> : null}

        {detail && domain ? (
          <>
            <Tabs tabs={tabs.map((tab) => ({ ...tab }))} activeId={activeTab} onChange={(id) => setActiveTab(id as TabId)} ariaLabel="Custom domain detail sections" />
            <div id={`${activeTab}--panel`} role="tabpanel" aria-labelledby={`${activeTab}--tab`} className="domains-tab-panel">
              {activeTab === 'overview' ? <Overview domain={domain} /> : null}

              {activeTab === 'entitlement' ? (
                <Card as="section" className="domains-detail-card"><h2>Entitlement</h2><dl className="domains-kv-grid">
                  <div><dt>State</dt><dd>{detail.entitlement.state}</dd></div><div><dt>Source</dt><dd>{detail.entitlement.source}</dd></div><div><dt>Status</dt><dd>{detail.entitlement.status}</dd></div><div><dt>Limit</dt><dd>{detail.entitlement.domain_limit}</dd></div><div><dt>Allocated</dt><dd>{detail.entitlement.allocated}</dd></div><div><dt>Remaining</dt><dd>{detail.entitlement.remaining}</dd></div><div><dt>New mutations</dt><dd>{detail.entitlement.mutation_allowed ? 'Allowed' : 'Denied'}</dd></div><div><dt>Existing routing</dt><dd>{detail.entitlement.existing_routing_allowed ? 'Allowed' : 'Denied'}</dd></div>{detail.entitlement.deadline ? <div><dt>Deadline</dt><dd>{new Date(detail.entitlement.deadline).toLocaleString()}</dd></div> : null}
                </dl></Card>
              ) : null}

              {activeTab === 'ownership' ? (
                <Card as="section" className="domains-detail-card"><h2>Ownership</h2><p><strong>Status:</strong> {domain.ownership_status}</p><p><strong>Token version:</strong> {domain.ownership_token_version}</p><p><strong>Verified at:</strong> {domain.ownership_verified_at ? new Date(domain.ownership_verified_at).toLocaleString() : 'Not verified'}</p><InlineMessage variant="info">The plaintext TXT verification value is intentionally unavailable on this detail route. Only the one-time create response may show it.</InlineMessage></Card>
              ) : null}

              {activeTab === 'dns' ? (
                <Card as="section" className="domains-detail-card"><h2>DNS</h2><p><strong>Ingress DNS status:</strong> {domain.ingress_dns_status}</p><p><strong>Last checked:</strong> {domain.ingress_dns_checked_at ? new Date(domain.ingress_dns_checked_at).toLocaleString() : 'Not checked'}</p>{domain.ingress_dns_status !== 'valid' ? <InlineMessage variant="warning">Current ingress DNS is not valid. This persistent state blocks readiness independently of Ownership, HTTPS and Risk.</InlineMessage> : <InlineMessage variant="success">Current ingress DNS is valid.</InlineMessage>}</Card>
              ) : null}

              {activeTab === 'https' ? (
                <Card as="section" className="domains-detail-card"><h2>HTTPS</h2><p><strong>HTTPS status:</strong> {domain.https_status}</p><p><strong>Last checked:</strong> {domain.https_checked_at ? new Date(domain.https_checked_at).toLocaleString() : 'Not checked'}</p>{domain.https_status !== 'active' ? <InlineMessage variant="warning">HTTPS is not active. Redirect readiness remains fail closed even if DNS is valid.</InlineMessage> : <InlineMessage variant="success">HTTPS is active for the current hostname.</InlineMessage>}</Card>
              ) : null}

              {activeTab === 'risk' ? (
                <Card as="section" className="domains-detail-card"><h2>Risk</h2><p><strong>Domain-risk status:</strong> {domain.risk_status}</p><p><strong>Policy version:</strong> {domain.risk_policy_version || 'Unavailable'}</p><p><strong>Last checked:</strong> {domain.risk_checked_at ? new Date(domain.risk_checked_at).toLocaleString() : 'Not checked'}</p><InlineMessage variant={domain.risk_status === 'allow' ? 'success' : 'danger'}>{domain.risk_status === 'allow' ? 'Current domain-risk authority is allow.' : 'Only a current allow decision satisfies readiness. Scanner evidence, thresholds and provider internals are not exposed.'}</InlineMessage></Card>
              ) : null}

              {activeTab === 'resources' ? (
                <Card as="section" className="domains-detail-card"><h2>Assigned resources</h2>{detail.assigned_links.length === 0 ? <EmptyState title="No assigned Links" reason="A Link can be assigned only while current entitlement and all required domain trust axes authorize the mutation." /> : <ul className="domains-resource-links">{detail.assigned_links.map((link) => <li key={link.id}><Link to="/app/links/$linkId" params={{ linkId: String(link.id) }}>{domain.hostname}/{link.code}</Link><span>{link.status}</span></li>)}</ul>}</Card>
              ) : null}

              {activeTab === 'revalidation' ? (
                <Card as="section" className="domains-detail-card"><h2>Revalidation</h2>{detail.revalidations.length === 0 ? <EmptyState title="No revalidation history yet" reason="Periodic authority results appear here after server-owned checks run." /> : <DataTable caption="Custom domain revalidation history"><thead><tr><th scope="col">Axis</th><th scope="col">Result</th><th scope="col">Policy</th><th scope="col">Checked</th><th scope="col">Next due</th></tr></thead><tbody>{detail.revalidations.map((item, index) => <tr key={`${item.axis}-${item.checked_at}-${index}`}><td>{item.axis}</td><td><span className="domains-state" data-state={item.result}>{item.result}</span></td><td>{item.policy_version}</td><td>{new Date(item.checked_at).toLocaleString()}</td><td>{item.next_due_at ? new Date(item.next_due_at).toLocaleString() : 'Not scheduled'}</td></tr>)}</tbody></DataTable>}</Card>
              ) : null}

              {activeTab === 'settings' ? (
                <Card as="section" className="domains-detail-card"><h2>Settings</h2><p>Hostname identity, ownership rotation, suspension and removal are server-authoritative mutations. This P06 browser slice does not fabricate controls that are not yet wired through the production Workspace permission provider.</p><dl className="domains-kv-grid"><div><dt>Canonical hostname</dt><dd>{domain.hostname}</dd></div><div><dt>Routing state</dt><dd>{domain.routing_state}</dd></div><div><dt>Security category</dt><dd>{domain.security_category || 'None'}</dd></div></dl></Card>
              ) : null}
            </div>
          </>
        ) : null}
      </section>
    </WorkspaceShell>
  );
}
