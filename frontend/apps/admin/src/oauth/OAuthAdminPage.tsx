import type { FormEvent } from 'react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { Button, InlineMessage } from '@gojet/ui';
import { AdminShell } from '../shell/AdminShell';
import {
  AdminOAuthAPIError,
  listOAuthProviders,
  testOAuthProvider,
  updateOAuthProvider,
  type OAuthProviderConfig,
  type OAuthProviderInput,
} from './api';

type AdminOAuthState = 'loading' | 'empty' | 'configured' | 'incomplete' | 'provider-error' | 'secret-masked' | 'test-result';
type ShellState = 'normal' | 'admin-auth-required' | 'permission-denied' | 'partial-service-degradation';

const frozenProviders = ['google', 'facebook', 'github', 'qq', 'wechat', 'rainbow'] as const;

type FormState = {
  enabled: boolean;
  clientID: string;
  clientSecret: string;
  authorizationURL: string;
  tokenURL: string;
  userInfoURL: string;
  redirectURI: string;
  scopes: string;
};

const emptyForm: FormState = {
  enabled: true,
  clientID: '',
  clientSecret: '',
  authorizationURL: '',
  tokenURL: '',
  userInfoURL: '',
  redirectURI: '',
  scopes: 'openid email',
};

function formFor(config: OAuthProviderConfig | undefined): FormState {
  if (!config) return { ...emptyForm };
  return {
    enabled: config.configured ? config.enabled : true,
    clientID: config.client_id,
    clientSecret: '',
    authorizationURL: config.authorization_url,
    tokenURL: config.token_url,
    userInfoURL: config.userinfo_url,
    redirectURI: config.redirect_uri,
    scopes: config.scopes.join(' '),
  };
}

function stateForConfig(config: OAuthProviderConfig | undefined): AdminOAuthState {
  return config?.configured ? 'configured' : 'incomplete';
}

export default function OAuthAdminPage() {
  const [providers, setProviders] = useState<OAuthProviderConfig[]>([]);
  const [selected, setSelected] = useState<string>('google');
  const [form, setForm] = useState<FormState>({ ...emptyForm });
  const [state, setState] = useState<AdminOAuthState>('loading');
  const [shellState, setShellState] = useState<ShellState>('normal');
  const [message, setMessage] = useState('');
  const alertRef = useRef<HTMLDivElement>(null);

  const selectedConfig = useMemo(() => providers.find((item) => item.provider === selected), [providers, selected]);

  const load = async () => {
    setState('loading');
    try {
      const response = await listOAuthProviders();
      const actual = response.providers.map((item) => item.provider);
      if (actual.length !== frozenProviders.length || actual.some((provider, index) => provider !== frozenProviders[index])) {
        throw new Error('Provider registry mismatch');
      }
      setProviders(response.providers);
      const current = response.providers.find((item) => item.provider === selected) ?? response.providers.at(0);
      if (!current) throw new Error('Provider registry is empty');
      setSelected(current.provider);
      setForm(formFor(current));
      setShellState('normal');
      setState(response.providers.every((item) => !item.configured) ? 'empty' : stateForConfig(current));
      setMessage('');
    } catch (error) {
      const status = error instanceof AdminOAuthAPIError ? error.status : 500;
      setShellState(status === 401 ? 'admin-auth-required' : status === 403 ? 'permission-denied' : 'partial-service-degradation');
      setState('provider-error');
      setMessage(status === 403 ? 'settings.manage permission is required for OAuth provider configuration.' : 'OAuth provider configuration is unavailable.');
    }
  };

  useEffect(() => { void load(); }, []);
  useEffect(() => {
    if (state === 'provider-error') alertRef.current?.focus();
  }, [state, message]);

  const chooseProvider = (provider: string) => {
    const config = providers.find((item) => item.provider === provider);
    setSelected(provider);
    setForm(formFor(config));
    setMessage('');
    setState(stateForConfig(config));
  };

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const input: OAuthProviderInput = {
      enabled: form.enabled,
      client_id: form.clientID,
      client_secret: form.clientSecret,
      authorization_url: form.authorizationURL,
      token_url: form.tokenURL,
      userinfo_url: form.userInfoURL,
      redirect_uri: form.redirectURI,
      scopes: form.scopes.split(/\s+/).map((value) => value.trim()).filter(Boolean),
    };
    try {
      const updated = await updateOAuthProvider(selected, input);
      const next = providers.map((item) => item.provider === selected ? updated : item);
      setProviders(next);
      setForm(formFor(updated));
      setShellState('normal');
      setState('configured');
      setMessage('Provider configuration saved. Client secret remains masked.');
    } catch (error) {
      const status = error instanceof AdminOAuthAPIError ? error.status : 500;
      setShellState(status === 403 ? 'permission-denied' : 'normal');
      setState('provider-error');
      setMessage(status === 403 ? 'settings.manage permission is required.' : 'Provider configuration was rejected by server validation.');
    }
  };

  const testProvider = async () => {
    try {
      const result = await testOAuthProvider(selected);
      setState('test-result');
      setMessage(result.status === 'configuration_ready' ? 'Server-side provider configuration test passed.' : 'Provider configuration test returned an unexpected result.');
    } catch (error) {
      const status = error instanceof AdminOAuthAPIError ? error.status : 500;
      setShellState(status === 403 ? 'permission-denied' : 'normal');
      setState('provider-error');
      setMessage(status === 409 ? 'Provider configuration is incomplete or disabled.' : 'Provider configuration test failed.');
    }
  };

  const secretMasked = selectedConfig?.secret_configured === true;

  return (
    <AdminShell state={shellState}>
      <section className="p15-admin-oauth" data-page="admin-oauth" data-admin-oauth-state={state} aria-busy={state === 'loading'}>
        <header className="p15-admin-oauth__header">
          <span>Platform / OAuth</span>
          <h1>OAuth provider configuration</h1>
          <p>P15 consumes <code>settings.manage</code>; P17 remains the owner of administrator role and permission lifecycle.</p>
        </header>
        {state === 'loading' && <p role="status">Loading provider registry…</p>}
        <div ref={alertRef} tabIndex={-1}>
          {state === 'provider-error' && <InlineMessage variant="danger">{message}</InlineMessage>}
          {state === 'empty' && <InlineMessage variant="info">No OAuth provider is configured yet. The frozen six-provider registry is available below.</InlineMessage>}
          {state === 'incomplete' && <InlineMessage variant="warning">This provider is incomplete and cannot be enabled for customer authentication.</InlineMessage>}
          {state === 'secret-masked' && <InlineMessage variant="success">Client secret is configured. The stored value is never returned to the browser.</InlineMessage>}
          {state === 'test-result' && <InlineMessage variant="success">{message}</InlineMessage>}
          {state === 'configured' && message && <InlineMessage variant="success">{message}</InlineMessage>}
        </div>
        {providers.length > 0 && (
          <div className="p15-admin-oauth__layout">
            <nav className="p15-admin-oauth__providers" aria-label="OAuth providers">
              {providers.map((provider) => (
                <button key={provider.provider} type="button" aria-current={selected === provider.provider ? 'page' : undefined} onClick={() => chooseProvider(provider.provider)}>
                  <strong>{provider.provider}</strong>
                  <span>{provider.configured ? 'Configured' : 'Incomplete'}</span>
                </button>
              ))}
            </nav>
            <form className="p15-admin-oauth__form" onSubmit={submit}>
              <div className="p15-admin-oauth__form-heading">
                <div><span>Provider</span><h2>{selected}</h2></div>
                <label className="p15-admin-oauth__toggle"><input type="checkbox" checked={form.enabled} onChange={(event) => setForm({ ...form, enabled: event.currentTarget.checked })} /> Enabled</label>
              </div>
              <label><span>Client ID</span><input aria-label="Client ID" value={form.clientID} onChange={(event) => setForm({ ...form, clientID: event.currentTarget.value })} autoComplete="off" /></label>
              <label><span>Client secret</span><input aria-label="Client secret" type="password" value={form.clientSecret} onChange={(event) => setForm({ ...form, clientSecret: event.currentTarget.value })} autoComplete="off" spellCheck={false} data-private-secret="true" /></label>
              <p className="p15-admin-oauth__secret-status">Secret status: {secretMasked ? 'Configured · masked' : 'Not configured'}</p>
              <label><span>Authorization URL</span><input aria-label="Authorization URL" value={form.authorizationURL} onChange={(event) => setForm({ ...form, authorizationURL: event.currentTarget.value })} /></label>
              <label><span>Token URL</span><input aria-label="Token URL" value={form.tokenURL} onChange={(event) => setForm({ ...form, tokenURL: event.currentTarget.value })} /></label>
              <label><span>User info URL</span><input aria-label="User info URL" value={form.userInfoURL} onChange={(event) => setForm({ ...form, userInfoURL: event.currentTarget.value })} /></label>
              <label><span>Redirect URI</span><input aria-label="Redirect URI" value={form.redirectURI} onChange={(event) => setForm({ ...form, redirectURI: event.currentTarget.value })} /></label>
              <label><span>Scopes</span><input aria-label="Scopes" value={form.scopes} onChange={(event) => setForm({ ...form, scopes: event.currentTarget.value })} /></label>
              <div className="p15-admin-oauth__actions">
                <Button type="submit">Save provider</Button>
                <Button type="button" variant="ghost" disabled={!selectedConfig?.configured || !selectedConfig?.enabled} onClick={() => void testProvider()}>Test provider</Button>
                <Button type="button" variant="ghost" disabled={!secretMasked} onClick={() => { setState('secret-masked'); setMessage(''); }}>Review secret status</Button>
              </div>
            </form>
          </div>
        )}
      </section>
    </AdminShell>
  );
}
