import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Card, DataTable, EmptyState, InlineMessage, useShellViewport } from '@gojet/ui';
import { AdminShell } from '../shell/AdminShell';
import { CommerceNav } from '../commerce/CommerceNav';
import { adminGet, adminWrite, readCommerceRuntime, type CommerceFX } from '../commerce/runtime';

export default function CommerceFXPage() {
  const runtime = useMemo(() => readCommerceRuntime(), []); const queryClient = useQueryClient(); const viewport = useShellViewport();
  const [base, setBase] = useState('USD'); const [quote, setQuote] = useState('EUR'); const [rate, setRate] = useState(''); const [source, setSource] = useState('manual'); const [reason, setReason] = useState(''); const [confirming, setConfirming] = useState(false); const [validation, setValidation] = useState('');
  const query = useQuery({ queryKey: ['p13-admin-fx'], enabled: runtime !== null, queryFn: () => adminGet<{ items: CommerceFX[] }>(runtime!, '/api/admin/fx'), retry: false });
  const mutation = useMutation({
    mutationFn: async () => {
      setValidation('');
      if (!/^[A-Z]{3}$/.test(base) || !/^[A-Z]{3}$/.test(quote) || base === quote || !/^\d+(?:\.\d+)?$/.test(rate) || !reason.trim()) throw new Error('Override requires distinct ISO currencies, a positive decimal-string rate and a reason.');
      return adminWrite<{ fx: CommerceFX }>(runtime!, `/api/admin/fx/${base}/${quote}`, 'PUT', { rate, source: source.trim() || 'manual', as_of: new Date().toISOString(), status: 'override', override_reason: reason.trim() });
    },
    onSuccess: async () => { setConfirming(false); setReason(''); setRate(''); await queryClient.invalidateQueries({ queryKey: ['p13-admin-fx'] }); },
    onError: (error) => { setConfirming(false); setValidation(error instanceof Error ? error.message : 'FX override failed.'); },
  });
  const providerErrorMutation = useMutation({
    mutationFn: (item: CommerceFX) => adminWrite<{ fx: CommerceFX }>(runtime!, `/api/admin/fx/${item.base_currency}/${item.quote_currency}/provider-error`, 'POST', { source: item.source, as_of: new Date().toISOString() }),
    onSuccess: async () => queryClient.invalidateQueries({ queryKey: ['p13-admin-fx'] }),
  });
  const items = query.data?.items ?? [];
  const pageState = !runtime || query.isError ? 'validation-error' : query.isPending ? 'loading' : confirming ? 'override-confirm' : validation ? 'validation-error' : items.some((item) => item.status === 'provider-error') ? 'provider-error' : items.some((item) => item.status === 'stale') ? 'stale' : items.length === 0 ? 'current' : 'current';
  return <AdminShell state={!runtime ? 'admin-auth-required' : query.isError ? 'partial-service-degradation' : 'normal'}><section className="commerce-page" data-page="admin-commerce-fx" data-state={pageState}>
    <header><div><p className="commerce-eyebrow">COMMERCE</p><h1>FX authority</h1><p>Rates retain source, as-of time and explicit current/stale/provider-error/override state. Provider errors retain the last positive rate.</p></div></header><CommerceNav />
    {!runtime ? <InlineMessage variant="danger">billing.manage authority is unavailable.</InlineMessage> : null}{query.isPending && runtime ? <p role="status">Loading FX rates…</p> : null}{query.isError ? <InlineMessage variant="danger">FX authority is unavailable. Stale data is not presented as current.</InlineMessage> : null}{validation ? <InlineMessage variant="danger">{validation}</InlineMessage> : null}
    {runtime ? <Card as="section" className="commerce-form-card" aria-labelledby="fx-override-title"><h2 id="fx-override-title">Audited manual override</h2><div className="commerce-form-grid"><label>Base<input value={base} maxLength={3} onChange={(event) => setBase(event.currentTarget.value.toUpperCase())} /></label><label>Quote<input value={quote} maxLength={3} onChange={(event) => setQuote(event.currentTarget.value.toUpperCase())} /></label><label>Decimal-string rate<input inputMode="decimal" value={rate} onChange={(event) => setRate(event.currentTarget.value)} /></label><label>Source<input value={source} onChange={(event) => setSource(event.currentTarget.value)} /></label><label className="commerce-form-wide">Override reason<textarea value={reason} onChange={(event) => setReason(event.currentTarget.value)} rows={2} /></label></div>{confirming ? <div className="commerce-confirm" role="alert"><strong>Confirm override</strong><p>This writes an audited override and replaces current presentation authority for this currency pair.</p><div><Button onClick={() => mutation.mutate()} disabled={mutation.isPending}>{mutation.isPending ? 'Applying…' : 'Confirm override'}</Button><Button variant="ghost" onClick={() => setConfirming(false)}>Cancel</Button></div></div> : <Button onClick={() => { setValidation(''); if (!rate || !reason.trim()) setValidation('Rate and override reason are required before confirmation.'); else setConfirming(true); }}>Review override</Button>}</Card> : null}
    {!query.isPending && runtime && !query.isError && items.length === 0 ? <EmptyState title="No FX rates" reason="Rates appear after provider ingestion or an audited override." /> : null}
    {items.length > 0 ? viewport !== 'mobile' ? <DataTable caption="Admin FX rates"><thead><tr><th scope="col">Pair</th><th scope="col">Rate</th><th scope="col">Status</th><th scope="col">Source</th><th scope="col">As of</th><th scope="col">Action</th></tr></thead><tbody>{items.map((item) => <tr key={`${item.base_currency}-${item.quote_currency}`}><td>{item.base_currency}/{item.quote_currency}</td><td>{item.rate}</td><td><span className="commerce-state" data-state={item.status}>{item.status}</span></td><td>{item.source}</td><td>{new Date(item.as_of).toLocaleString()}</td><td><Button variant="ghost" onClick={() => providerErrorMutation.mutate(item)} disabled={providerErrorMutation.isPending}>Record provider error</Button></td></tr>)}</tbody></DataTable> : <div className="commerce-card-list">{items.map((item) => <Card as="article" key={`${item.base_currency}-${item.quote_currency}`}><strong>{item.base_currency}/{item.quote_currency}</strong><span>{item.rate} · {item.status}</span><span>{item.source} · {new Date(item.as_of).toLocaleString()}</span></Card>)}</div> : null}
  </section></AdminShell>;
}
