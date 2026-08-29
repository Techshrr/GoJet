import type { FormEvent } from 'react';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Button, InlineMessage } from '@gojet/ui';
import { WorkspaceShell } from '../shell/WorkspaceShell';

type APIError = { error?: { code?: string } };
type APIKey = {
  id: string;
  name: string;
  prefix: string;
  scopes: string[];
  status: string;
  rate_limit_per_minute: number;
  expires_at?: string;
};
type APIKeySecret = { key: APIKey; secret: string };
type Webhook = {
  id: string;
  name: string;
  endpoint_url: string;
  events: string[];
  secret_prefix: string;
  status: string;
};
type WebhookSecret = { webhook: Webhook; secret: string };
type Delivery = {
  id: string;
  event_id: string;
  event_type: string;
  status: string;
  attempts: number;
  last_error_code?: string;
};

const workspaceID = import.meta.env.VITE_P17_WORKSPACE_ID || 'ws-p17-browser';

function errorCode(status: number, body: APIError | undefined) {
  return body?.error?.code || `http_${status}`;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-Request-ID': `p17-browser-${Date.now()}`, ...(init?.headers || {}) },
  });
  const body = await response.json().catch(() => undefined) as T & APIError | undefined;
  if (!response.ok) throw new Error(errorCode(response.status, body));
  return body as T;
}

function SecretOnce({ label, value, onDismiss }: { label: string; value: string; onDismiss: () => void }) {
  return (
    <section className="developer-secret" role="alert" aria-live="assertive" data-secret-once="true">
      <strong>{label}</strong>
      <p>This secret is shown once. Store it now; GoJet will not show the full value again.</p>
      <code>{value}</code>
      <Button type="button" variant="ghost" onClick={onDismiss}>I stored it</Button>
    </section>
  );
}

function ErrorMessage({ error }: { error: string }) {
  if (!error) return null;
  const forbidden = error === 'forbidden' || error === 'authentication_required';
  return <InlineMessage variant="danger">{forbidden ? 'You do not have permission to manage this developer surface.' : `Request failed: ${error}`}</InlineMessage>;
}

function effectiveKeyStatus(key: APIKey) {
  if (key.status === 'active' && key.expires_at && new Date(key.expires_at).getTime() <= Date.now()) return 'expired';
  return key.status;
}

export function APIKeysPage() {
  const [keys, setKeys] = useState<APIKey[]>([]);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(true);
  const [secret, setSecret] = useState('');
  const [name, setName] = useState('Browser key');
  const [scopes, setScopes] = useState('links.read,links.write');
  const [rate, setRate] = useState(120);

  const load = useCallback(async () => {
    setBusy(true); setError('');
    try {
      const result = await request<{ keys: APIKey[] }>(`/api/workspaces/${workspaceID}/api-keys`);
      setKeys(result.keys || []);
    } catch (err) { setError(err instanceof Error ? err.message : 'server_error'); }
    finally { setBusy(false); }
  }, []);

  useEffect(() => { void load(); }, [load]);

  async function create(event: FormEvent) {
    event.preventDefault(); setError(''); setSecret('');
    try {
      const result = await request<APIKeySecret>(`/api/workspaces/${workspaceID}/api-keys`, {
        method: 'POST',
        body: JSON.stringify({ name, scopes: scopes.split(',').map((value) => value.trim()).filter(Boolean), rate_limit_per_minute: rate }),
      });
      setSecret(result.secret); await load();
    } catch (err) { setError(err instanceof Error ? err.message : 'server_error'); }
  }

  async function lifecycle(id: string, action: 'rotate' | 'revoke') {
    setError(''); setSecret('');
    try {
      const result = await request<APIKeySecret | { key: APIKey }>(`/api/workspaces/${workspaceID}/api-keys/${id}/${action}`, { method: 'POST', body: '{}' });
      if ('secret' in result) setSecret(result.secret);
      await load();
    } catch (err) { setError(err instanceof Error ? err.message : 'server_error'); }
  }

  const pageState = useMemo(() => {
    if (error) return error === 'forbidden' || error === 'authentication_required' ? 'forbidden' : 'error';
    if (busy) return 'loading';
    if (secret) return 'secret-once';
    if (keys.length === 0) return 'empty';
    if (keys.some((key) => effectiveKeyStatus(key) === 'expired')) return 'expired';
    if (keys.some((key) => effectiveKeyStatus(key) === 'revoked')) return 'revoked';
    return 'create';
  }, [busy, error, keys, secret]);

  return (
    <WorkspaceShell state="normal" sectionLabel="API keys" workspaceLabel="Developer workspace">
      <section className="developer-page" data-page="api-keys" data-state={pageState}>
        <header><p className="developer-kicker">Developer</p><h1>API keys</h1><p>Create scoped credentials with explicit rate authority. Full secrets are never listed again.</p></header>
        <ErrorMessage error={error} />
        {secret && <SecretOnce label="New API key secret" value={secret} onDismiss={() => setSecret('')} />}
        <form className="developer-form" onSubmit={create} aria-label="Create API key">
          <label>Name<input required value={name} onChange={(event) => setName(event.target.value)} /></label>
          <label>Scopes<input required value={scopes} onChange={(event) => setScopes(event.target.value)} aria-describedby="scope-help" /></label>
          <small id="scope-help">Comma-separated exact scopes; wildcard scopes are not accepted.</small>
          <label>Requests per minute<input type="number" min="1" max="10000" value={rate} onChange={(event) => setRate(Number(event.target.value))} /></label>
          <Button type="submit">Create key</Button>
        </form>
        <div className="developer-table-wrap">
          <table><caption>Workspace API keys</caption><thead><tr><th>Name</th><th>Prefix</th><th>Scopes</th><th>Status</th><th>Rate</th><th>Actions</th></tr></thead>
            <tbody>{keys.map((key) => <tr key={key.id} data-key-id={key.id}><td>{key.name}</td><td><code>{key.prefix}</code></td><td>{key.scopes.join(', ')}</td><td>{effectiveKeyStatus(key)}</td><td>{key.rate_limit_per_minute}/min</td><td><Button type="button" variant="ghost" disabled={key.status === 'revoked'} onClick={() => void lifecycle(key.id, 'rotate')}>Rotate</Button> <Button type="button" variant="ghost" disabled={key.status === 'revoked'} onClick={() => void lifecycle(key.id, 'revoke')}>Revoke</Button></td></tr>)}</tbody>
          </table>
          {!busy && !error && keys.length === 0 && <p data-empty="true">No API keys yet.</p>}
        </div>
      </section>
    </WorkspaceShell>
  );
}

export function WebhooksPage() {
  const [hooks, setHooks] = useState<Webhook[]>([]);
  const [deliveries, setDeliveries] = useState<Delivery[]>([]);
  const [selected, setSelected] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(true);
  const [secret, setSecret] = useState('');
  const [name, setName] = useState('Browser webhook');
  const [endpoint, setEndpoint] = useState('https://hooks.example.com/gojet');
  const [events, setEvents] = useState('link.updated');

  const load = useCallback(async () => {
    setBusy(true); setError('');
    try {
      const result = await request<{ webhooks: Webhook[] }>(`/api/workspaces/${workspaceID}/webhooks`);
      setHooks(result.webhooks || []);
      setSelected((current) => current || result.webhooks?.[0]?.id || '');
    } catch (err) { setError(err instanceof Error ? err.message : 'server_error'); }
    finally { setBusy(false); }
  }, []);

  useEffect(() => { void load(); }, [load]);
  useEffect(() => {
    if (!selected) { setDeliveries([]); return; }
    void request<{ deliveries: Delivery[] }>(`/api/workspaces/${workspaceID}/webhooks/${selected}/deliveries`)
      .then((result) => setDeliveries(result.deliveries || []))
      .catch((err) => setError(err instanceof Error ? err.message : 'server_error'));
  }, [selected]);

  async function create(event: FormEvent) {
    event.preventDefault(); setError(''); setSecret('');
    try {
      const result = await request<WebhookSecret>(`/api/workspaces/${workspaceID}/webhooks`, {
        method: 'POST', body: JSON.stringify({ name, endpoint_url: endpoint, events: events.split(',').map((value) => value.trim()).filter(Boolean) }),
      });
      setSecret(result.secret); setSelected(result.webhook.id); await load();
    } catch (err) { setError(err instanceof Error ? err.message : 'server_error'); }
  }

  async function action(id: string, verb: 'rotate-secret' | 'enable' | 'disable') {
    setError(''); setSecret('');
    try {
      const result = await request<WebhookSecret | { webhook: Webhook }>(`/api/workspaces/${workspaceID}/webhooks/${id}/${verb}`, { method: 'POST', body: '{}' });
      if ('secret' in result) setSecret(result.secret);
      await load();
    } catch (err) { setError(err instanceof Error ? err.message : 'server_error'); }
  }

  async function retry(deliveryID: string) {
    if (!selected) return;
    setError('');
    try {
      await request(`/api/workspaces/${workspaceID}/webhooks/${selected}/deliveries/${deliveryID}/retry`, { method: 'POST', body: '{}' });
      const result = await request<{ deliveries: Delivery[] }>(`/api/workspaces/${workspaceID}/webhooks/${selected}/deliveries`);
      setDeliveries(result.deliveries || []);
    } catch (err) { setError(err instanceof Error ? err.message : 'server_error'); }
  }

  const selectedHook = hooks.find((hook) => hook.id === selected);
  const pageState = useMemo(() => {
    if (error) return error === 'forbidden' || error === 'authentication_required' ? 'forbidden' : 'error';
    if (busy) return 'loading';
    if (secret) return 'secret-rotate';
    if (hooks.length === 0) return 'empty';
    if (selectedHook?.status === 'disabled') return 'disabled';
    if (deliveries.some((delivery) => delivery.status === 'retrying')) return 'retrying';
    if (deliveries.length > 0) return 'delivery';
    return 'create';
  }, [busy, deliveries, error, hooks.length, secret, selectedHook?.status]);

  return (
    <WorkspaceShell state="normal" sectionLabel="Webhooks" workspaceLabel="Developer workspace">
      <section className="developer-page" data-page="webhooks" data-state={pageState}>
        <header><p className="developer-kicker">Developer</p><h1>Webhooks</h1><p>Configure signed Workspace-owned outbound deliveries. Payment callbacks remain a separate authority.</p></header>
        <ErrorMessage error={error} />
        {secret && <SecretOnce label="Webhook signing secret" value={secret} onDismiss={() => setSecret('')} />}
        <form className="developer-form" onSubmit={create} aria-label="Create webhook">
          <label>Name<input required value={name} onChange={(event) => setName(event.target.value)} /></label>
          <label>HTTPS endpoint<input required type="url" value={endpoint} onChange={(event) => setEndpoint(event.target.value)} /></label>
          <label>Events<input required value={events} onChange={(event) => setEvents(event.target.value)} /></label>
          <Button type="submit">Create webhook</Button>
        </form>
        <div className="developer-table-wrap"><table><caption>Workspace webhooks</caption><thead><tr><th>Name</th><th>Endpoint</th><th>Events</th><th>Status</th><th>Actions</th></tr></thead><tbody>
          {hooks.map((hook) => <tr key={hook.id} data-webhook-id={hook.id} aria-selected={selected === hook.id}><td><button type="button" className="developer-link-button" onClick={() => setSelected(hook.id)}>{hook.name}</button></td><td>{hook.endpoint_url}</td><td>{hook.events.join(', ')}</td><td>{hook.status}</td><td><Button type="button" variant="ghost" onClick={() => void action(hook.id, 'rotate-secret')}>Rotate secret</Button> {hook.status === 'active' ? <Button type="button" variant="ghost" onClick={() => void action(hook.id, 'disable')}>Disable</Button> : <Button type="button" variant="ghost" onClick={() => void action(hook.id, 'enable')}>Enable</Button>}</td></tr>)}
        </tbody></table>{!busy && !error && hooks.length === 0 && <p data-empty="true">No webhooks yet.</p>}</div>
        <section className="developer-deliveries" aria-labelledby="delivery-title"><h2 id="delivery-title">Recent deliveries</h2>{!selected && <p>Select a webhook to inspect delivery metadata.</p>}<ul>{deliveries.map((delivery) => <li key={delivery.id} data-delivery-status={delivery.status}><code>{delivery.event_type}</code><span>{delivery.status} · {delivery.attempts} attempts</span>{delivery.last_error_code && <span>{delivery.last_error_code}</span>}{delivery.status === 'failed' && <Button type="button" variant="ghost" onClick={() => void retry(delivery.id)}>Retry</Button>}</li>)}</ul></section>
      </section>
    </WorkspaceShell>
  );
}
