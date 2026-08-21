import { type FormEvent, useMemo, useState } from 'react';
import { Link } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { GoJetApiError, type CreatedWorkspaceDomain } from '@gojet/api-client';
import { Button, Card, InlineMessage, TextField } from '@gojet/ui';
import { WorkspaceShell } from '../shell/WorkspaceShell';
import { createWorkspaceDomainsClient } from '../domains/runtime';
import { isReadOnly, readWorkspaceRuntime } from '../links/runtime';

const steps = [
  'Entitlement',
  'Hostname',
  'TXT ownership',
  'Ingress DNS',
  'HTTPS',
  'Domain risk',
  'Ready',
] as const;

function AxisState({ label, value, ready }: { label: string; value: string; ready: boolean }) {
  return (
    <div className="domains-axis-detail" data-ready={ready ? 'true' : 'false'}>
      <span>{label}</span><strong>{value}</strong><span>{ready ? 'Ready' : 'Action required'}</span>
    </div>
  );
}

export default function DomainCreatePage() {
  const runtime = useMemo(() => readWorkspaceRuntime(), []);
  const client = useMemo(() => runtime ? createWorkspaceDomainsClient(runtime) : null, [runtime]);
  const readOnly = runtime ? isReadOnly(runtime) : true;
  const queryClient = useQueryClient();
  const [step, setStep] = useState(0);
  const [hostname, setHostname] = useState('');
  const [reason, setReason] = useState('Add custom domain');
  const [created, setCreated] = useState<CreatedWorkspaceDomain | null>(null);

  const authorityQuery = useQuery({
    queryKey: ['domains', runtime?.workspaceId],
    enabled: Boolean(client && runtime),
    queryFn: () => client!.list(runtime!.workspaceId),
  });
  const detailQuery = useQuery({
    queryKey: ['domain', runtime?.workspaceId, created?.domain.id],
    enabled: Boolean(client && runtime && created?.domain.id),
    queryFn: () => client!.get(runtime!.workspaceId, created!.domain.id),
    refetchOnWindowFocus: false,
  });

  const createMutation = useMutation({
    mutationFn: async () => {
      if (!client || !runtime) throw new Error('Domain client unavailable');
      return client.create(runtime.workspaceId, { hostname, change_reason: reason });
    },
    onSuccess: async (result) => {
      setCreated(result);
      setStep(2);
      await queryClient.invalidateQueries({ queryKey: ['domains', runtime?.workspaceId] });
    },
  });

  const entitlement = authorityQuery.data?.entitlement;
  const allowed = Boolean(entitlement?.mutation_allowed && entitlement.remaining > 0 && !readOnly);
  const domain = detailQuery.data?.domain;
  const apiError = createMutation.error instanceof GoJetApiError ? createMutation.error : null;

  function submitHostname(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (allowed && hostname.trim() && reason.trim()) createMutation.mutate();
  }

  if (!runtime) {
    return (
      <WorkspaceShell sectionLabel="Add domain">
        <section className="domains-page"><InlineMessage variant="warning">Production Workspace identity is unavailable until P12/P15 provides authoritative authentication context.</InlineMessage></section>
      </WorkspaceShell>
    );
  }

  if (authorityQuery.isPending) {
    return <WorkspaceShell sectionLabel="Add domain"><section className="domains-page"><p role="status">Checking Workspace domain authority…</p></section></WorkspaceShell>;
  }

  if (authorityQuery.isError || !entitlement) {
    return <WorkspaceShell sectionLabel="Add domain"><section className="domains-page"><InlineMessage variant="danger">Domain entitlement authority is unavailable. The wizard is not mounted while authority cannot be established.</InlineMessage><Link to="/app/domains" className="domains-secondary-link">Back to domains</Link></section></WorkspaceShell>;
  }

  if (!allowed) {
    return (
      <WorkspaceShell state={readOnly ? 'read-only-role' : 'notification-attention'} sectionLabel="Add domain">
        <section className="domains-page" data-page="domain-create-denied">
          <header className="domains-page-header"><div><p className="domains-eyebrow">DOMAINS</p><h1>Add domain unavailable</h1><p>The server-authoritative Workspace state does not permit a new custom-domain mutation.</p></div></header>
          <InlineMessage variant={entitlement.state === 'suspended' || entitlement.state === 'revoked' ? 'danger' : 'warning'}>
            Current state: <strong>{entitlement.state}</strong>. Source: <strong>{entitlement.source}</strong>. Remaining capacity: <strong>{entitlement.remaining}</strong>. Deep links do not bypass this authority check.
          </InlineMessage>
          {entitlement.deadline ? <InlineMessage variant="warning">Existing routing grace ends at {new Date(entitlement.deadline).toLocaleString()}; new domains are denied throughout the grace window.</InlineMessage> : null}
          <Link to="/app/domains" className="domains-secondary-link">Back to domains</Link>
        </section>
      </WorkspaceShell>
    );
  }

  return (
    <WorkspaceShell sectionLabel="Add domain">
      <section className="domains-page" data-page="domain-create" data-wizard-mounted="true">
        <header className="domains-page-header">
          <div><p className="domains-eyebrow">DOMAINS</p><h1>Add custom domain</h1><p>Seven separate authority steps. Passing one step never implies another axis is ready.</p></div>
          <Link to="/app/domains" className="domains-secondary-link">Cancel</Link>
        </header>

        <nav className="domains-steps" aria-label="Custom domain setup steps">
          <ol>{steps.map((label, index) => <li key={label} data-current={index === step ? 'true' : 'false'} data-complete={index < step ? 'true' : 'false'}><span>{index + 1}</span><strong>{label}</strong></li>)}</ol>
        </nav>

        {apiError ? <InlineMessage variant={apiError.status === 409 ? 'warning' : 'danger'}>{apiError.message} <strong>{apiError.code}</strong></InlineMessage> : null}

        {step === 0 ? (
          <Card as="section" className="domains-wizard-card">
            <p className="domains-eyebrow">STEP 1 OF 7</p><h2>Entitlement</h2>
            <p>The current Workspace authority is read directly from persisted plan/manual-approval sources.</p>
            <dl className="domains-kv-grid"><div><dt>State</dt><dd>{entitlement.state}</dd></div><div><dt>Source</dt><dd>{entitlement.source}</dd></div><div><dt>Limit</dt><dd>{entitlement.domain_limit}</dd></div><div><dt>Remaining</dt><dd>{entitlement.remaining}</dd></div></dl>
            <div className="domains-wizard-actions"><Button onClick={() => setStep(1)}>Continue to hostname</Button></div>
          </Card>
        ) : null}

        {step === 1 ? (
          <form onSubmit={submitHostname}>
            <Card as="section" className="domains-wizard-card">
              <p className="domains-eyebrow">STEP 2 OF 7</p><h2>Hostname</h2>
              <p>GoJet normalizes the hostname server-side, rejects platform hostnames and enforces global canonical uniqueness without exposing another Workspace.</p>
              <TextField id="domain-hostname" label="Custom hostname" value={hostname} onChange={(event) => setHostname(event.currentTarget.value)} placeholder="go.example.com" required />
              <TextField id="domain-change-reason" label="Change reason" value={reason} onChange={(event) => setReason(event.currentTarget.value)} required />
              <div className="domains-wizard-actions"><Button variant="outline" type="button" onClick={() => setStep(0)}>Back</Button><Button type="submit" disabled={createMutation.isPending}>{createMutation.isPending ? 'Creating…' : 'Create domain and continue'}</Button></div>
            </Card>
          </form>
        ) : null}

        {step === 2 && created ? (
          <Card as="section" className="domains-wizard-card">
            <p className="domains-eyebrow">STEP 3 OF 7</p><h2>TXT ownership</h2>
            <InlineMessage variant="warning">This plaintext verification value is displayed only from the creation response. It is not returned by list/detail APIs and cannot be reconstructed from the stored verifier.</InlineMessage>
            <div className="domains-record"><span>TXT name</span><code data-secret-kind="ownership-name">{created.ownership_txt_name}</code></div>
            <div className="domains-record"><span>TXT value</span><code data-secret-kind="ownership-value">{created.ownership_txt_value}</code></div>
            <AxisState label="Ownership" value={domain?.ownership_status ?? created.domain.ownership_status} ready={domain?.ownership_status === 'verified'} />
            <div className="domains-wizard-actions"><Button variant="outline" onClick={() => detailQuery.refetch()}>Refresh status</Button><Button onClick={() => setStep(3)}>Continue to DNS</Button></div>
          </Card>
        ) : null}

        {step === 3 && created ? (
          <Card as="section" className="domains-wizard-card">
            <p className="domains-eyebrow">STEP 4 OF 7</p><h2>Ingress DNS</h2>
            <p>Ownership and ingress are independent. Point the hostname at the server-owned GoJet ingress target; current DNS must validate before this axis becomes ready.</p>
            <AxisState label="Ingress DNS" value={domain?.ingress_dns_status ?? 'pending'} ready={domain?.ingress_dns_status === 'valid'} />
            {domain?.ingress_dns_status !== 'valid' ? <InlineMessage variant="warning">Ingress DNS is not currently valid. This problem remains in-page until the persisted authority changes.</InlineMessage> : <InlineMessage variant="success">Current ingress DNS authority is valid.</InlineMessage>}
            <div className="domains-wizard-actions"><Button variant="outline" onClick={() => setStep(2)}>Back</Button><Button variant="outline" onClick={() => detailQuery.refetch()}>Refresh status</Button><Button onClick={() => setStep(4)}>Continue to HTTPS</Button></div>
          </Card>
        ) : null}

        {step === 4 && created ? (
          <Card as="section" className="domains-wizard-card">
            <p className="domains-eyebrow">STEP 5 OF 7</p><h2>HTTPS</h2>
            <p>GoJet requires a current TLS handshake and hostname verification. DNS readiness does not imply HTTPS readiness.</p>
            <AxisState label="HTTPS" value={domain?.https_status ?? 'pending'} ready={domain?.https_status === 'active'} />
            {domain?.https_status !== 'active' ? <InlineMessage variant="warning">HTTPS is not active. Customer routing remains unavailable even if DNS is correct.</InlineMessage> : <InlineMessage variant="success">HTTPS is active for the current hostname.</InlineMessage>}
            <div className="domains-wizard-actions"><Button variant="outline" onClick={() => setStep(3)}>Back</Button><Button variant="outline" onClick={() => detailQuery.refetch()}>Refresh status</Button><Button onClick={() => setStep(5)}>Continue to risk</Button></div>
          </Card>
        ) : null}

        {step === 5 && created ? (
          <Card as="section" className="domains-wizard-card">
            <p className="domains-eyebrow">STEP 6 OF 7</p><h2>Domain risk</h2>
            <p>Only a current <strong>allow</strong> decision satisfies this independent domain-risk axis. Provider evidence and thresholds are not exposed here.</p>
            <AxisState label="Domain risk" value={domain?.risk_status ?? 'missing'} ready={domain?.risk_status === 'allow'} />
            {domain?.risk_status !== 'allow' ? <InlineMessage variant="danger">Domain risk is not currently allow. Link assignment and custom-host routing fail closed.</InlineMessage> : <InlineMessage variant="success">Current domain-risk authority is allow.</InlineMessage>}
            <div className="domains-wizard-actions"><Button variant="outline" onClick={() => setStep(4)}>Back</Button><Button variant="outline" onClick={() => detailQuery.refetch()}>Refresh status</Button><Button onClick={() => setStep(6)}>Continue to Ready</Button></div>
          </Card>
        ) : null}

        {step === 6 && created ? (
          <Card as="section" className="domains-wizard-card">
            <p className="domains-eyebrow">STEP 7 OF 7</p><h2>Ready</h2>
            <div className="domains-ready-grid">
              <AxisState label="Ownership" value={domain?.ownership_status ?? 'pending'} ready={domain?.ownership_status === 'verified'} />
              <AxisState label="Ingress DNS" value={domain?.ingress_dns_status ?? 'pending'} ready={domain?.ingress_dns_status === 'valid'} />
              <AxisState label="HTTPS" value={domain?.https_status ?? 'pending'} ready={domain?.https_status === 'active'} />
              <AxisState label="Domain risk" value={domain?.risk_status ?? 'missing'} ready={domain?.risk_status === 'allow'} />
            </div>
            {domain?.ready_for_new_links ? <InlineMessage variant="success">All current mutation authorities are ready for new Link assignment.</InlineMessage> : <InlineMessage variant="warning">This domain is not ready for new Links. The incomplete authority remains visible above; no success state is inferred from earlier steps.</InlineMessage>}
            <p><strong>Routing state:</strong> {domain?.routing_state ?? 'pending'}</p>
            <div className="domains-wizard-actions"><Button variant="outline" onClick={() => setStep(5)}>Back</Button><Button variant="outline" onClick={() => detailQuery.refetch()}>Refresh status</Button><Link className="domains-primary-link" to="/app/domains/$domainId" params={{ domainId: String(created.domain.id) }}>Open domain detail</Link></div>
          </Card>
        ) : null}
      </section>
    </WorkspaceShell>
  );
}
