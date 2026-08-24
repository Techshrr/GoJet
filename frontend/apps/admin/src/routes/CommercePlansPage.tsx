import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Card, DataTable, EmptyState, InlineMessage, useShellViewport } from '@gojet/ui';
import { AdminShell } from '../shell/AdminShell';
import { CommerceNav } from '../commerce/CommerceNav';
import { adminGet, adminWrite, parseEntitlements, readCommerceRuntime, type CommercePlan, type CommerceRequestError } from '../commerce/runtime';

const defaultEntitlements = 'links:100:count\ncustom_domains:1:count';

export default function CommercePlansPage() {
  const runtime = useMemo(() => readCommerceRuntime(), []);
  const queryClient = useQueryClient();
  const viewport = useShellViewport();
  const [code, setCode] = useState(''); const [name, setName] = useState(''); const [currency, setCurrency] = useState('USD');
  const [amountMinor, setAmountMinor] = useState('1000'); const [billingPeriod, setBillingPeriod] = useState<'monthly' | 'yearly' | 'one_time'>('monthly');
  const [entitlements, setEntitlements] = useState(defaultEntitlements); const [validation, setValidation] = useState(''); const [conflict, setConflict] = useState('');
  const query = useQuery({ queryKey: ['p13-admin-plans'], enabled: runtime !== null, queryFn: () => adminGet<{ items: CommercePlan[] }>(runtime!, '/api/admin/plans'), retry: false });
  const createMutation = useMutation({
    mutationFn: async () => {
      setValidation(''); setConflict('');
      if (!code.trim() || !name.trim() || !/^[A-Z]{3}$/.test(currency.trim().toUpperCase()) || !Number.isSafeInteger(Number(amountMinor)) || Number(amountMinor) <= 0) throw new Error('Enter a valid code, name, ISO currency and positive minor-unit amount.');
      const parsed = parseEntitlements(entitlements);
      return adminWrite<{ plan: CommercePlan }>(runtime!, '/api/admin/plans', 'POST', { code: code.trim(), name: name.trim(), status: 'draft', currency: currency.trim().toUpperCase(), amount_minor: Number(amountMinor), billing_period: billingPeriod, entitlements: parsed });
    },
    onSuccess: async () => { setCode(''); setName(''); await queryClient.invalidateQueries({ queryKey: ['p13-admin-plans'] }); },
    onError: (error) => setValidation(error instanceof Error ? error.message : 'Plan validation failed.'),
  });
  const updateMutation = useMutation({
    mutationFn: async ({ plan, status }: { plan: CommercePlan; status: 'active' | 'archived' }) => adminWrite<{ plan: CommercePlan }>(runtime!, `/api/admin/plans/${plan.id}`, 'PUT', {
      name: plan.name, status, currency: plan.money.currency, amount_minor: plan.money.amount_minor, billing_period: plan.billing_period,
      entitlements: plan.entitlements.map((item) => ({ capability: item.capability, limit_value: item.limit_value, unit: item.unit })), expected_version: plan.version,
    }),
    onSuccess: async () => { setConflict(''); await queryClient.invalidateQueries({ queryKey: ['p13-admin-plans'] }); },
    onError: (error: CommerceRequestError) => { if (error?.status === 409) setConflict('Plan update conflicted with a newer version. Reloaded server state is required before retrying.'); else setValidation(error instanceof Error ? error.message : 'Plan update failed.'); },
  });
  const items = query.data?.items ?? [];
  const pageState = !runtime ? 'validation-error' : query.isPending ? 'loading' : query.isError ? 'validation-error' : conflict ? 'conflict' : validation ? 'validation-error' : items.length === 0 ? 'empty' : items.some((item) => item.status === 'archived') ? 'archived' : items.some((item) => item.status === 'active') ? 'active' : 'draft';

  return <AdminShell state={!runtime ? 'admin-auth-required' : query.isError ? 'partial-service-degradation' : 'normal'}>
    <section className="commerce-page" data-page="admin-commerce-plans" data-state={pageState}>
      <header><div><p className="commerce-eyebrow">COMMERCE</p><h1>Plans</h1><p>Plan code is immutable after creation. Versioned updates and terminal archive transitions are enforced by the server.</p></div></header>
      <CommerceNav />
      {!runtime ? <InlineMessage variant="danger">billing.manage authority is unavailable. Admin commerce fails closed.</InlineMessage> : null}
      {query.isPending && runtime ? <p role="status">Loading plans…</p> : null}
      {query.isError ? <InlineMessage variant="danger">Plans could not be loaded. No cached admin state is treated as current.</InlineMessage> : null}
      {validation ? <InlineMessage variant="danger">{validation}</InlineMessage> : null}
      {conflict ? <InlineMessage variant="warning">{conflict}</InlineMessage> : null}
      {runtime ? <Card as="section" className="commerce-form-card" aria-labelledby="new-plan-title"><h2 id="new-plan-title">Create draft plan</h2>
        <div className="commerce-form-grid">
          <label>Code<input value={code} onChange={(event) => setCode(event.currentTarget.value)} placeholder="business" /></label>
          <label>Name<input value={name} onChange={(event) => setName(event.currentTarget.value)} placeholder="Business" /></label>
          <label>Currency<input value={currency} maxLength={3} onChange={(event) => setCurrency(event.currentTarget.value.toUpperCase())} /></label>
          <label>Amount in minor units<input inputMode="numeric" value={amountMinor} onChange={(event) => setAmountMinor(event.currentTarget.value)} /></label>
          <label>Billing period<select value={billingPeriod} onChange={(event) => setBillingPeriod(event.currentTarget.value as typeof billingPeriod)}><option value="monthly">Monthly</option><option value="yearly">Yearly</option><option value="one_time">One time</option></select></label>
          <label className="commerce-form-wide">Entitlements<textarea value={entitlements} onChange={(event) => setEntitlements(event.currentTarget.value)} rows={3} aria-describedby="entitlement-help" /><span id="entitlement-help">One per line: capability:positive-limit:unit</span></label>
        </div><Button disabled={createMutation.isPending} onClick={() => createMutation.mutate()}>{createMutation.isPending ? 'Creating…' : 'Create draft'}</Button>
      </Card> : null}
      {!query.isPending && runtime && !query.isError && items.length === 0 ? <EmptyState title="No plans" reason="Create a draft plan before publishing billing options." /> : null}
      {items.length > 0 ? viewport !== 'mobile' ? <DataTable caption="Admin commerce plans"><thead><tr><th scope="col">Code</th><th scope="col">Name</th><th scope="col">Status</th><th scope="col">Price</th><th scope="col">Version</th><th scope="col">Action</th></tr></thead><tbody>{items.map((plan) => <tr key={plan.id}><td>{plan.code}</td><td>{plan.name}</td><td><span className="commerce-state" data-state={plan.status}>{plan.status}</span></td><td>{plan.money.currency} {plan.money.amount_minor} minor units / {plan.billing_period}</td><td>{plan.version}</td><td>{plan.status === 'draft' ? <Button variant="ghost" onClick={() => updateMutation.mutate({ plan, status: 'active' })}>Activate</Button> : null}{plan.status !== 'archived' ? <Button variant="ghost" onClick={() => updateMutation.mutate({ plan, status: 'archived' })}>Archive</Button> : <span>Terminal</span>}</td></tr>)}</tbody></DataTable> : <div className="commerce-card-list">{items.map((plan) => <Card as="article" key={plan.id}><strong>{plan.name}</strong><span>{plan.code} · {plan.status}</span><span>{plan.money.currency} {plan.money.amount_minor} minor units</span><span>Version {plan.version}</span></Card>)}</div> : null}
    </section>
  </AdminShell>;
}
