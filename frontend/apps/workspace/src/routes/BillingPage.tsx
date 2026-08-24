import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Card, DataTable, EmptyState, InlineMessage, useShellViewport } from '@gojet/ui';
import { WorkspaceShell } from '../shell/WorkspaceShell';
import {
  billingSummary, createOrder, publicPlans, readBillingRuntime, workspaceInvoices, workspacePayments,
  type BillingPlan, type BillingSummary,
} from '../billing/runtime';

function money(plan: BillingPlan) { return `${plan.money.currency} ${plan.money.amount_minor} minor units / ${plan.billing_period.replace('_', ' ')}`; }
function summaryMessage(summary: BillingSummary) {
  switch (summary.state) {
    case 'payment-pending': return { variant: 'info' as const, text: 'Payment is pending. Entitlements do not change until server-side settlement is confirmed.' };
    case 'payment-failed': return { variant: 'danger' as const, text: 'The latest payment failed. No optimistic browser state can activate billing entitlement.' };
    case 'overdue': return { variant: 'warning' as const, text: 'Billing is overdue. Current access remains governed by the recorded subscription and entitlement boundaries.' };
    case 'canceled': return { variant: 'warning' as const, text: 'No active billing subscription is available for this Workspace.' };
    case 'provider-partial': return { variant: 'warning' as const, text: 'The provider flow is still processing. Treat payment status as incomplete until authoritative settlement arrives.' };
    default: return { variant: 'success' as const, text: 'Billing is active. Effective capabilities remain server-resolved from durable entitlement grants.' };
  }
}

export default function BillingPage() {
  const runtime = useMemo(() => readBillingRuntime(), []);
  const queryClient = useQueryClient();
  const viewport = useShellViewport();
  const [selectedPlan, setSelectedPlan] = useState('');
  const summaryQuery = useQuery({
    queryKey: ['p13-billing-summary', runtime?.workspaceId],
    enabled: Boolean(runtime && (runtime.role === 'owner' || runtime.role === 'admin')),
    queryFn: () => billingSummary(runtime!), retry: false,
  });
  const plansQuery = useQuery({ queryKey: ['p13-public-plans'], queryFn: publicPlans, retry: false });
  const invoiceQuery = useQuery({
    queryKey: ['p13-invoices', runtime?.workspaceId], enabled: runtime?.role === 'owner', queryFn: () => workspaceInvoices(runtime!), retry: false,
  });
  const paymentQuery = useQuery({
    queryKey: ['p13-payments', runtime?.workspaceId], enabled: runtime?.role === 'owner', queryFn: () => workspacePayments(runtime!), retry: false,
  });
  const orderMutation = useMutation({
    mutationFn: async () => {
      if (!runtime || runtime.role !== 'owner' || !selectedPlan) throw new Error('Owner and plan are required.');
      const kind = summaryQuery.data?.subscription ? 'upgrade' : 'new';
      return createOrder(runtime, Number(selectedPlan), kind);
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['p13-billing-summary', runtime?.workspaceId] });
      await queryClient.invalidateQueries({ queryKey: ['p13-invoices', runtime?.workspaceId] });
    },
  });

  const authorizedSummary = runtime && (runtime.role === 'owner' || runtime.role === 'admin');
  const loading = authorizedSummary && (summaryQuery.isPending || plansQuery.isPending);
  const pageState = !runtime || !authorizedSummary || summaryQuery.isError || plansQuery.isError ? 'error' : loading ? 'loading' : summaryQuery.data?.state ?? 'canceled';
  const shellState = !runtime ? 'api-offline' : runtime.role === 'member' || runtime.role === 'viewer' ? 'read-only-role' : 'notification-attention';
  const notice = summaryQuery.data ? summaryMessage(summaryQuery.data) : null;
  const plans = plansQuery.data ?? [];
  const invoices = invoiceQuery.data ?? [];
  const payments = paymentQuery.data ?? [];

  return (
    <WorkspaceShell state={shellState} sectionLabel="Billing">
      <section className="billing-page" data-page="workspace-billing" data-state={pageState}>
        <header className="billing-page-header">
          <div><p className="billing-eyebrow">BILLING</p><h1>Plan and payments</h1><p>Payment presentation is never settlement authority. This page reflects server-side billing state and durable entitlement boundaries.</p></div>
        </header>
        {!runtime ? <InlineMessage variant="danger">Billing authentication context is unavailable. No local billing state is substituted.</InlineMessage> : null}
        {runtime && !authorizedSummary ? <InlineMessage variant="info">Billing summary and financial records are restricted. Effective capability checks remain available through the owning product surfaces.</InlineMessage> : null}
        {loading ? <p role="status">Loading authoritative billing state…</p> : null}
        {summaryQuery.isError ? <InlineMessage variant="danger">Billing status could not be loaded. Stale payment state is not shown as current.</InlineMessage> : null}
        {plansQuery.isError ? <InlineMessage variant="danger">Plan data is unavailable. Plan changes are disabled.</InlineMessage> : null}
        {notice ? <InlineMessage variant={notice.variant}>{notice.text}</InlineMessage> : null}

        {summaryQuery.data ? <Card as="section" className="billing-summary" aria-labelledby="billing-summary-title">
          <div><p className="billing-eyebrow">CURRENT STATE</p><h2 id="billing-summary-title">{summaryQuery.data.plan?.name ?? 'No active plan'}</h2></div>
          <dl className="billing-kv">
            <div><dt>Billing state</dt><dd><strong>{summaryQuery.data.state}</strong></dd></div>
            <div><dt>Subscription</dt><dd>{summaryQuery.data.subscription?.status ?? 'none'}</dd></div>
            <div><dt>Latest order</dt><dd>{summaryQuery.data.latest_order_status || 'none'}</dd></div>
            <div><dt>Term ends</dt><dd>{summaryQuery.data.subscription?.current_term_ends_at ? new Date(summaryQuery.data.subscription.current_term_ends_at).toLocaleString() : 'Not scheduled'}</dd></div>
            <div><dt>Grace ends</dt><dd>{summaryQuery.data.subscription?.grace_ends_at ? new Date(summaryQuery.data.subscription.grace_ends_at).toLocaleString() : 'Not in grace'}</dd></div>
            <div><dt>Scheduled target</dt><dd>{summaryQuery.data.scheduled_target ? `Plan #${summaryQuery.data.scheduled_target.plan_id} at ${new Date(summaryQuery.data.scheduled_target.starts_at).toLocaleString()}` : 'None'}</dd></div>
          </dl>
        </Card> : null}

        {runtime?.role === 'owner' ? <Card as="section" className="billing-plan-action" aria-labelledby="billing-plan-action-title">
          <div><p className="billing-eyebrow">PLAN CHANGE</p><h2 id="billing-plan-action-title">Create a payable order</h2></div>
          <p>The order becomes pending only. Entitlement activates only after an authenticated provider settlement callback.</p>
          <div className="billing-action-row">
            <label>Plan<select value={selectedPlan} onChange={(event) => setSelectedPlan(event.currentTarget.value)}><option value="">Select a plan</option>{plans.map((plan) => <option key={plan.id} value={plan.id}>{plan.name} — {money(plan)}</option>)}</select></label>
            <Button disabled={!selectedPlan || orderMutation.isPending} onClick={() => orderMutation.mutate()}>{orderMutation.isPending ? 'Creating order…' : 'Create order'}</Button>
          </div>
          {orderMutation.isSuccess ? <InlineMessage variant="info">Order {orderMutation.data.order.id} is {orderMutation.data.order.status}. Payment settlement is still pending server authority.</InlineMessage> : null}
          {orderMutation.isError ? <InlineMessage variant="danger">The payable order could not be created. No billing state was assumed locally.</InlineMessage> : null}
        </Card> : runtime?.role === 'admin' ? <InlineMessage variant="info">Workspace admins may inspect billing status, but plan changes and financial ledgers require the Workspace owner.</InlineMessage> : null}

        {runtime?.role === 'owner' ? <section className="billing-ledger" aria-labelledby="billing-ledger-title">
          <div><p className="billing-eyebrow">FINANCIAL LEDGER</p><h2 id="billing-ledger-title">Invoices and payments</h2></div>
          {(invoiceQuery.isError || paymentQuery.isError) ? <InlineMessage variant="warning">Part of the financial ledger is unavailable. Missing data is not presented as zero activity.</InlineMessage> : null}
          {invoices.length === 0 && payments.length === 0 && !invoiceQuery.isPending && !paymentQuery.isPending ? <EmptyState title="No financial records yet" reason="Invoices and provider transactions appear only after real billing lifecycle events." /> : null}
          {invoices.length > 0 ? <Card as="section"><h3>Invoices</h3>{viewport !== 'mobile' ? <DataTable caption="Workspace invoices"><thead><tr><th scope="col">Invoice</th><th scope="col">Status</th><th scope="col">Amount</th><th scope="col">Issued</th></tr></thead><tbody>{invoices.map((item) => <tr key={item.id}><td>{item.id}</td><td><span className="billing-state" data-state={item.status}>{item.status}</span></td><td>{item.money.currency} {item.money.amount_minor} minor units</td><td>{new Date(item.issued_at).toLocaleString()}</td></tr>)}</tbody></DataTable> : <div className="billing-card-list">{invoices.map((item) => <article key={item.id}><strong>{item.id}</strong><span>{item.status}</span><span>{item.money.currency} {item.money.amount_minor} minor units</span></article>)}</div>}</Card> : null}
          {payments.length > 0 ? <Card as="section"><h3>Payments</h3>{viewport !== 'mobile' ? <DataTable caption="Workspace payments"><thead><tr><th scope="col">Provider</th><th scope="col">Status</th><th scope="col">Amount</th><th scope="col">Updated</th></tr></thead><tbody>{payments.map((item) => <tr key={item.id}><td>{item.provider}</td><td><span className="billing-state" data-state={item.status}>{item.status}</span></td><td>{item.money.currency} {item.money.amount_minor} minor units</td><td>{new Date(item.updated_at).toLocaleString()}</td></tr>)}</tbody></DataTable> : <div className="billing-card-list">{payments.map((item) => <article key={item.id}><strong>{item.provider}</strong><span>{item.status}</span><span>{item.money.currency} {item.money.amount_minor} minor units</span></article>)}</div>}</Card> : null}
        </section> : null}
      </section>
    </WorkspaceShell>
  );
}
