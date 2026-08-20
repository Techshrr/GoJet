import { type FormEvent, useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  GoJetApiError,
  type LinkABVariant,
  type LinkRecord,
  type LinkRoutingRule,
  type LinkUpdateInput,
} from '@gojet/api-client';
import {
  Button,
  Card,
  Checkbox,
  EmptyState,
  InlineMessage,
  SelectField,
  Tabs,
  TextField,
} from '@gojet/ui';
import { WorkspaceShell } from '../shell/WorkspaceShell';
import { createWorkspaceLinksClient, isReadOnly, readWorkspaceRuntime } from '../links/runtime';

const tabs = [
  { id: 'overview', label: 'Overview' },
  { id: 'analytics', label: 'Analytics' },
  { id: 'routing', label: 'Routing' },
  { id: 'ab', label: 'A/B Test' },
  { id: 'utm', label: 'UTM' },
  { id: 'access', label: 'Access' },
  { id: 'qr', label: 'QR' },
  { id: 'settings', label: 'Settings' },
  { id: 'history', label: 'History' },
] as const;

type TabId = typeof tabs[number]['id'];

function updateInput(link: LinkRecord, reason: string): LinkUpdateInput {
  return {
    expected_version: link.version,
    hostname: link.hostname,
    domain_kind: link.domain_kind,
    code: link.code,
    title: link.title,
    primary_destination: link.primary_destination,
    redirect_status: link.redirect_status,
    status: link.status === 'paused' ? 'paused' : 'active',
    routing: link.routing,
    ab: link.ab,
    utm: link.utm,
    access: link.access,
    expires_at: link.expires_at ?? null,
    click_limit: link.click_limit ?? null,
    one_time: link.one_time,
    change_reason: reason,
  };
}

export default function LinkDetailPage() {
  const { linkId } = useParams({ from: '/app/links/$linkId' });
  const numericId = Number(linkId);
  const runtime = useMemo(() => readWorkspaceRuntime(), []);
  const client = useMemo(() => runtime ? createWorkspaceLinksClient(runtime) : null, [runtime]);
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const readOnly = runtime ? isReadOnly(runtime) : true;
  const [activeTab, setActiveTab] = useState<TabId>('overview');
  const [draft, setDraft] = useState<LinkRecord | null>(null);
  const [reason, setReason] = useState('Update link');
  const [riskNotice, setRiskNotice] = useState<string | null>(null);

  const linkQuery = useQuery({
    queryKey: ['link', runtime?.workspaceId, numericId],
    enabled: client !== null && runtime !== null && Number.isSafeInteger(numericId) && numericId > 0,
    queryFn: () => client!.get(runtime!.workspaceId, numericId),
  });
  const historyQuery = useQuery({
    queryKey: ['link-history', runtime?.workspaceId, numericId],
    enabled: activeTab === 'history' && client !== null && runtime !== null && numericId > 0,
    queryFn: () => client!.history(runtime!.workspaceId, numericId),
  });

  useEffect(() => {
    if (linkQuery.data) setDraft(linkQuery.data);
  }, [linkQuery.data]);

  const saveMutation = useMutation({
    mutationFn: async () => {
      if (!client || !runtime || !draft) throw new Error('Link editor unavailable');
      return client.update(runtime.workspaceId, draft.id, updateInput(draft, reason));
    },
    onSuccess: async (updated) => {
      setDraft(updated);
      setRiskNotice('Reachable-target edits invalidate the previous decision. Redirects remain fail-closed until this exact fingerprint receives allow.');
      await queryClient.invalidateQueries({ queryKey: ['links', runtime?.workspaceId] });
      await queryClient.invalidateQueries({ queryKey: ['link-history', runtime?.workspaceId, numericId] });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: async () => {
      if (!client || !runtime || !draft) throw new Error('Link editor unavailable');
      return client.remove(runtime.workspaceId, draft.id, draft.version, reason);
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['links', runtime?.workspaceId] });
      await navigate({ to: '/app/links' });
    },
  });

  const restoreMutation = useMutation({
    mutationFn: async (restoreVersion: number) => {
      if (!client || !runtime || !draft) throw new Error('Link editor unavailable');
      return client.restore(runtime.workspaceId, draft.id, draft.version, restoreVersion, reason);
    },
    onSuccess: async (restored) => {
      setDraft(restored);
      setRiskNotice('Restore created a new version and the restored reachable-target set now requires an exact-current risk decision.');
      await queryClient.invalidateQueries({ queryKey: ['link-history', runtime?.workspaceId, numericId] });
      await queryClient.invalidateQueries({ queryKey: ['links', runtime?.workspaceId] });
    },
  });

  function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!readOnly && draft && reason.trim()) saveMutation.mutate();
  }

  function addRoutingRule() {
    if (!draft) return;
    const id = `rule-${draft.routing.length + 1}`;
    setDraft({ ...draft, routing: [...draft.routing, { id, match_type: 'country', match_value: '', destination: '', enabled: true }] });
  }

  function updateRouting(index: number, patch: Partial<LinkRoutingRule>) {
    if (!draft) return;
    setDraft({ ...draft, routing: draft.routing.map((item, itemIndex) => itemIndex === index ? { ...item, ...patch } : item) });
  }

  function addVariant() {
    if (!draft) return;
    const id = `variant-${draft.ab.length + 1}`;
    const next: LinkABVariant = { id, destination: '', weight: draft.ab.length === 0 ? 50 : 0, enabled: true };
    setDraft({ ...draft, ab: [...draft.ab, next] });
  }

  function updateVariant(index: number, patch: Partial<LinkABVariant>) {
    if (!draft) return;
    setDraft({ ...draft, ab: draft.ab.map((item, itemIndex) => itemIndex === index ? { ...item, ...patch } : item) });
  }

  const apiError = saveMutation.error instanceof GoJetApiError ? saveMutation.error : null;

  return (
    <WorkspaceShell state={readOnly && runtime ? 'read-only-role' : 'notification-attention'} sectionLabel="Link detail">
      <section className="links-page" data-page="link-detail">
        <header className="links-page-header">
          <div><p className="links-eyebrow">LINKS</p><h1>{draft?.title || draft?.code || 'Link detail'}</h1><p>{draft ? `${draft.hostname}/${draft.code}` : 'Loading link…'}</p></div>
          <Link to="/app/links" className="links-secondary-link">Back to links</Link>
        </header>

        {!runtime ? <InlineMessage variant="warning">Production Workspace identity is unavailable until P12/P15 provides authoritative authentication context.</InlineMessage> : null}
        {linkQuery.isError ? <InlineMessage variant="danger">This link could not be loaded.</InlineMessage> : null}
        {apiError ? <InlineMessage variant={apiError.status === 409 ? 'warning' : 'danger'}>{apiError.message} <strong>{apiError.code}</strong></InlineMessage> : null}
        {riskNotice ? <InlineMessage variant="warning">{riskNotice}</InlineMessage> : null}

        {draft ? (
          <>
            <Card as="section" className="links-risk-card">
              <div><strong>Destination safety fingerprint</strong><code>{draft.risk_fingerprint}</code></div>
              <p>Redirectengine only emits a customer 3xx after an exact-current allow decision for this fingerprint. Domain trust never substitutes for destination trust.</p>
            </Card>
            <Tabs tabs={tabs.map((tab) => ({ ...tab }))} activeId={activeTab} onChange={(id) => setActiveTab(id as TabId)} ariaLabel="Link detail sections" />

            <form className="links-form" onSubmit={save}>
              <div id={`${activeTab}--panel`} role="tabpanel" aria-labelledby={`${activeTab}--tab`} className="links-tab-panel">
                {activeTab === 'overview' ? (
                  <Card as="section" className="links-form-section">
                    <h2>Overview</h2>
                    <TextField id="detail-title" label="Title" value={draft.title} onChange={(event) => setDraft({ ...draft, title: event.currentTarget.value })} />
                    <TextField id="detail-destination" label="Primary destination" type="url" value={draft.primary_destination} onChange={(event) => setDraft({ ...draft, primary_destination: event.currentTarget.value })} helpText="Any reachable-target change produces a new fingerprint and returns the link to fail-closed safety state." />
                    <TextField id="detail-hostname" label="Hostname" value={draft.hostname} onChange={(event) => setDraft({ ...draft, hostname: event.currentTarget.value })} />
                    <TextField id="detail-code" label="Code" value={draft.code} onChange={(event) => setDraft({ ...draft, code: event.currentTarget.value })} />
                    <SelectField id="detail-status" label="Lifecycle status" value={draft.status === 'paused' ? 'paused' : 'active'} onChange={(event) => setDraft({ ...draft, status: event.currentTarget.value as 'active' | 'paused' })} options={[{ value: 'active', label: 'Active' }, { value: 'paused', label: 'Paused' }]} />
                  </Card>
                ) : null}

                {activeTab === 'analytics' ? <EmptyState title="Analytics is owned by P07" reason="P05 does not fabricate click-series or reconciliation data. The link click counter remains available in Overview and list evidence." /> : null}

                {activeTab === 'routing' ? (
                  <Card as="section" className="links-form-section">
                    <h2>Smart routing</h2>
                    <p>Risk evaluation always happens before these rules. In P05, the first enabled matching rule wins; A/B is used only when no routing rule matches.</p>
                    {draft.routing.map((rule, index) => (
                      <div className="links-rule" key={rule.id}>
                        <TextField id={`route-id-${index}`} label="Rule ID" value={rule.id} onChange={(event) => updateRouting(index, { id: event.currentTarget.value })} />
                        <SelectField id={`route-type-${index}`} label="Match type" value={rule.match_type} onChange={(event) => updateRouting(index, { match_type: event.currentTarget.value })} options={[
                          { value: 'country', label: 'Country' }, { value: 'device', label: 'Device' }, { value: 'language', label: 'Language' }, { value: 'source_hostname', label: 'Source hostname' },
                        ]} />
                        <TextField id={`route-value-${index}`} label="Match value" value={rule.match_value} onChange={(event) => updateRouting(index, { match_value: event.currentTarget.value })} />
                        <TextField id={`route-destination-${index}`} label="Destination" type="url" value={rule.destination} onChange={(event) => updateRouting(index, { destination: event.currentTarget.value })} />
                        <Checkbox label="Enabled" checked={rule.enabled} onChange={(event) => updateRouting(index, { enabled: event.currentTarget.checked })} />
                        <Button variant="outline" onClick={() => setDraft({ ...draft, routing: draft.routing.filter((_, itemIndex) => itemIndex !== index) })}>Remove rule</Button>
                      </div>
                    ))}
                    <Button variant="outline" onClick={addRoutingRule}>Add routing rule</Button>
                  </Card>
                ) : null}

                {activeTab === 'ab' ? (
                  <Card as="section" className="links-form-section">
                    <h2>A/B Test</h2>
                    <p>Enabled weights must total exactly 100 and every enabled destination is included in the safety fingerprint.</p>
                    {draft.ab.map((variant, index) => (
                      <div className="links-rule" key={variant.id}>
                        <TextField id={`ab-id-${index}`} label="Variant ID" value={variant.id} onChange={(event) => updateVariant(index, { id: event.currentTarget.value })} />
                        <TextField id={`ab-destination-${index}`} label="Destination" type="url" value={variant.destination} onChange={(event) => updateVariant(index, { destination: event.currentTarget.value })} />
                        <TextField id={`ab-weight-${index}`} label="Weight" type="number" min="1" max="100" value={String(variant.weight)} onChange={(event) => updateVariant(index, { weight: Number(event.currentTarget.value) })} />
                        <Checkbox label="Enabled" checked={variant.enabled} onChange={(event) => updateVariant(index, { enabled: event.currentTarget.checked })} />
                        <Button variant="outline" onClick={() => setDraft({ ...draft, ab: draft.ab.filter((_, itemIndex) => itemIndex !== index) })}>Remove variant</Button>
                      </div>
                    ))}
                    <Button variant="outline" onClick={addVariant}>Add variant</Button>
                  </Card>
                ) : null}

                {activeTab === 'utm' ? (
                  <Card as="section" className="links-form-section">
                    <h2>UTM</h2>
                    <p>UTM mutation is applied only after risk allow and after routing/A-B target selection.</p>
                    <TextField id="utm-source" label="Source" value={draft.utm.source ?? ''} onChange={(event) => setDraft({ ...draft, utm: { ...draft.utm, source: event.currentTarget.value } })} />
                    <TextField id="utm-medium" label="Medium" value={draft.utm.medium ?? ''} onChange={(event) => setDraft({ ...draft, utm: { ...draft.utm, medium: event.currentTarget.value } })} />
                    <TextField id="utm-campaign" label="Campaign" value={draft.utm.campaign ?? ''} onChange={(event) => setDraft({ ...draft, utm: { ...draft.utm, campaign: event.currentTarget.value } })} />
                    <TextField id="utm-term" label="Term" value={draft.utm.term ?? ''} onChange={(event) => setDraft({ ...draft, utm: { ...draft.utm, term: event.currentTarget.value } })} />
                    <TextField id="utm-content" label="Content" value={draft.utm.content ?? ''} onChange={(event) => setDraft({ ...draft, utm: { ...draft.utm, content: event.currentTarget.value } })} />
                  </Card>
                ) : null}

                {activeTab === 'access' ? (
                  <Card as="section" className="links-form-section">
                    <h2>Access</h2>
                    <TextField id="detail-expires" label="Expiration" type="datetime-local" value={draft.expires_at ? draft.expires_at.slice(0, 16) : ''} onChange={(event) => setDraft({ ...draft, expires_at: event.currentTarget.value ? new Date(event.currentTarget.value).toISOString() : null })} />
                    <TextField id="detail-click-limit" label="Click limit" type="number" min="1" value={draft.click_limit ?? ''} onChange={(event) => setDraft({ ...draft, click_limit: event.currentTarget.value ? Number(event.currentTarget.value) : null })} />
                    <Checkbox label="One-time link" checked={draft.one_time} onChange={(event) => setDraft({ ...draft, one_time: event.currentTarget.checked })} />
                    <TextField id="detail-password" label="Password protection" disabled value="" helpText="Not enabled until server-side hashing and challenge verification are implemented. The browser never submits a trusted password_hash value." />
                  </Card>
                ) : null}

                {activeTab === 'qr' ? <EmptyState title="QR is owned by P08" reason="P05 does not fabricate QR artifacts. QR generation will also be denied whenever this source Link is pending, review or blocked." /> : null}

                {activeTab === 'settings' ? (
                  <Card as="section" className="links-form-section">
                    <h2>Settings</h2>
                    <SelectField id="detail-redirect-status" label="Redirect status" value={String(draft.redirect_status)} onChange={(event) => setDraft({ ...draft, redirect_status: Number(event.currentTarget.value) as LinkRecord['redirect_status'] })} options={[
                      { value: '301', label: '301' }, { value: '302', label: '302' }, { value: '307', label: '307' }, { value: '308', label: '308' },
                    ]} />
                    <InlineMessage variant="warning">Delete is version-checked and creates immutable history/audit evidence. It does not expose the destination after deletion.</InlineMessage>
                    <Button variant="destructive" disabled={readOnly || deleteMutation.isPending || !reason.trim()} onClick={() => deleteMutation.mutate()}>Delete link</Button>
                  </Card>
                ) : null}

                {activeTab === 'history' ? (
                  <Card as="section" className="links-form-section">
                    <h2>History</h2>
                    {historyQuery.isPending ? <p role="status">Loading history…</p> : null}
                    {historyQuery.data?.items.map((version) => (
                      <article className="links-history-row" key={version.version}>
                        <div><strong>Version {version.version}</strong><span>{version.actor_id}</span></div>
                        <p>{version.change_reason}</p>
                        <Button variant="outline" disabled={readOnly || restoreMutation.isPending || version.version === draft.version || !reason.trim()} onClick={() => restoreMutation.mutate(version.version)}>Restore as new version</Button>
                      </article>
                    ))}
                  </Card>
                ) : null}
              </div>

              {activeTab !== 'analytics' && activeTab !== 'qr' && activeTab !== 'history' && activeTab !== 'settings' ? (
                <div className="links-form-actions">
                  <TextField id="detail-reason" label="Change reason" required value={reason} onChange={(event) => setReason(event.currentTarget.value)} />
                  <Button type="submit" loading={saveMutation.isPending} disabled={readOnly || !reason.trim()}>Save changes</Button>
                </div>
              ) : null}
              {(activeTab === 'settings' || activeTab === 'history') ? <TextField id="detail-reason-secondary" label="Change reason" required value={reason} onChange={(event) => setReason(event.currentTarget.value)} /> : null}
            </form>
          </>
        ) : null}
      </section>
    </WorkspaceShell>
  );
}
