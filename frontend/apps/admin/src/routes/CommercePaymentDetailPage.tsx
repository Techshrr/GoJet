import { useMemo } from 'react';
import { useParams } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { Card, InlineMessage } from '@gojet/ui';
import { AdminShell } from '../shell/AdminShell';
import { CommerceNav } from '../commerce/CommerceNav';
import { adminGet, readCommerceRuntime, type CommercePayment } from '../commerce/runtime';

export default function CommercePaymentDetailPage() {
  const runtime = useMemo(() => readCommerceRuntime(), []); const { paymentId } = useParams({ from: '/admin/commerce/payments/$paymentId' });
  const query = useQuery({ queryKey: ['p13-admin-payment', paymentId], enabled: runtime !== null, queryFn: () => adminGet<{ payment: CommercePayment }>(runtime!, `/api/admin/payments/${encodeURIComponent(paymentId)}`), retry: false });
  const item = query.data?.payment; const state = !runtime || query.isError ? 'partial' : query.isPending ? 'loading' : item?.status ?? 'partial';
  const reference = item?.provider_transaction_id ? `••••${item.provider_transaction_id.slice(-4)}` : 'Unavailable';
  return <AdminShell state={!runtime ? 'admin-auth-required' : query.isError ? 'partial-service-degradation' : 'normal'}><section className="commerce-page" data-page="admin-commerce-payment-detail" data-state={state}>
    <header><div><p className="commerce-eyebrow">COMMERCE / PAYMENTS</p><h1>Payment detail</h1><p>Only allowlisted operational fields are shown. Provider callback evidence and credentials remain server-side.</p></div></header><CommerceNav />
    {query.isPending && runtime ? <p role="status">Loading payment…</p> : null}{query.isError ? <InlineMessage variant="danger">Payment detail is unavailable.</InlineMessage> : null}
    {item ? <Card as="section"><dl className="commerce-kv"><div><dt>Payment</dt><dd>#{item.id}</dd></div><div><dt>Status</dt><dd><strong>{item.status}</strong></dd></div><div><dt>Workspace</dt><dd>{item.workspace_id}</dd></div><div><dt>Order</dt><dd>{item.order_id}</dd></div><div><dt>Provider</dt><dd>{item.provider}</dd></div><div><dt>Provider reference</dt><dd>{reference}</dd></div><div><dt>Amount</dt><dd>{item.money.currency} {item.money.amount_minor} minor units</dd></div><div><dt>Updated</dt><dd>{new Date(item.updated_at).toLocaleString()}</dd></div></dl></Card> : null}
  </section></AdminShell>;
}
