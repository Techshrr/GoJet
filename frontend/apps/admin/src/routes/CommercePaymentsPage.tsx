import { useMemo } from 'react';
import { Link } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { Card, DataTable, EmptyState, InlineMessage, useShellViewport } from '@gojet/ui';
import { AdminShell } from '../shell/AdminShell';
import { CommerceNav } from '../commerce/CommerceNav';
import { adminGet, readCommerceRuntime, type CommercePayment } from '../commerce/runtime';

export default function CommercePaymentsPage() {
  const runtime = useMemo(() => readCommerceRuntime(), []); const viewport = useShellViewport();
  const query = useQuery({ queryKey: ['p13-admin-payments'], enabled: runtime !== null, queryFn: () => adminGet<{ items: CommercePayment[] }>(runtime!, '/api/admin/payments?limit=100'), retry: false });
  const items = query.data?.items ?? [];
  const pageState = !runtime ? 'partial' : query.isPending ? 'loading' : query.isError ? 'partial' : items.length === 0 ? 'empty' : items.some((item) => item.status === 'refunded') ? 'refunded' : items.some((item) => item.status === 'failed') ? 'failed' : items.some((item) => item.status === 'paid') ? 'paid' : 'pending';
  return <AdminShell state={!runtime ? 'admin-auth-required' : query.isError ? 'partial-service-degradation' : 'normal'}>
    <section className="commerce-page" data-page="admin-commerce-payments" data-state={pageState}>
      <header><div><p className="commerce-eyebrow">COMMERCE</p><h1>Payments</h1><p>Provider settlement records are server authority. Raw callback bodies, credentials and signatures are never exposed here.</p></div></header>
      <CommerceNav />
      <InlineMessage variant="info">Invalid provider callbacks are rejected before durable payment mutation. They never become synthetic ledger rows or browser settlement authority.</InlineMessage>
      {!runtime ? <InlineMessage variant="danger">billing.manage authority is unavailable.</InlineMessage> : null}
      {query.isPending && runtime ? <p role="status">Loading payments…</p> : null}
      {query.isError ? <InlineMessage variant="warning">Payment records are partially unavailable. Missing records are not presented as an empty successful ledger.</InlineMessage> : null}
      {!query.isPending && runtime && !query.isError && items.length === 0 ? <EmptyState title="No payment records" reason="Provider transactions appear only after authenticated billing lifecycle events." /> : null}
      {items.length > 0 ? viewport !== 'mobile' ? <DataTable caption="Admin payment records"><thead><tr><th scope="col">Payment</th><th scope="col">Workspace</th><th scope="col">Provider</th><th scope="col">Status</th><th scope="col">Amount</th><th scope="col">Updated</th></tr></thead><tbody>{items.map((item) => <tr key={item.id}><td><Link to="/admin/commerce/payments/$paymentId" params={{ paymentId: String(item.id) }}>#{item.id}</Link></td><td>{item.workspace_id}</td><td>{item.provider}</td><td><span className="commerce-state" data-state={item.status}>{item.status}</span></td><td>{item.money.currency} {item.money.amount_minor} minor units</td><td>{new Date(item.updated_at).toLocaleString()}</td></tr>)}</tbody></DataTable> : <div className="commerce-card-list">{items.map((item) => <Card as="article" key={item.id}><Link to="/admin/commerce/payments/$paymentId" params={{ paymentId: String(item.id) }}>Payment #{item.id}</Link><span>{item.provider} · {item.status}</span><span>{item.money.currency} {item.money.amount_minor} minor units</span></Card>)}</div> : null}
    </section>
  </AdminShell>;
}
