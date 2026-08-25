import type { FormEvent, ReactNode } from 'react';
import { useEffect, useRef, useState } from 'react';
import { Button } from '@gojet/ui';
import { WorkspaceShell } from '../shell/WorkspaceShell';

export type AccountState =
  | 'loading'
  | 'success'
  | 'read-only'
  | 'validation-error'
  | 'session-revoked'
  | 'provider-error'
  | 'destructive-confirm';

type UserSummary = {
  id: string;
  email: string;
  display_name: string;
  status: string;
  email_verified_at?: string | null;
};

type SessionSummary = {
  id: string;
  status: string;
  expires_at: string;
  last_seen_at: string;
  created_at: string;
  current: boolean;
};

type ConnectedAccount = {
  id: string;
  provider: string;
  provider_email?: string;
  provider_email_verified?: boolean;
  display_name?: string;
};

type MeResponse = {
  user: UserSummary;
  session: SessionSummary;
  csrf_token: string;
};

type ApiFailure = Error & { status?: number };

const providers = ['google', 'facebook', 'github', 'qq', 'wechat', 'rainbow'] as const;

async function requestJSON<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set('Accept', 'application/json');
  if (init.body !== undefined && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
  const response = await fetch(path, { ...init, credentials: 'include', headers });
  if (!response.ok) {
    const failure = new Error(`Account request failed with ${response.status}`) as ApiFailure;
    failure.status = response.status;
    throw failure;
  }
  return response.json() as Promise<T>;
}

function stateForFailure(error: unknown, validation = false): AccountState {
  const status = (error as ApiFailure)?.status;
  if (status === 401 || status === 410) return 'session-revoked';
  if (validation && (status === 400 || status === 409)) return 'validation-error';
  return 'provider-error';
}

async function currentAccount(): Promise<MeResponse> {
  return requestJSON<MeResponse>('/api/me');
}

function AccountFrame({ title, state, children }: { title: string; state: AccountState; children: ReactNode }) {
  return (
    <WorkspaceShell state="notification-attention" sectionLabel={`Settings / ${title}`}>
      <section className="p15-account" data-account-state={state} aria-busy={state === 'loading'}>
        <header className="p15-account__header">
          <span className="p15-account__eyebrow">Account settings</span>
          <h1>{title}</h1>
        </header>
        <nav className="p15-account__tabs" aria-label="Account settings">
          <a href="/app/settings/profile">Profile</a>
          <a href="/app/settings/security">Security</a>
          <a href="/app/settings/sessions">Sessions</a>
          <a href="/app/settings/connected-accounts">Connected accounts</a>
        </nav>
        {children}
      </section>
    </WorkspaceShell>
  );
}

function StateMessage({ state, message }: { state: AccountState; message?: string }) {
  if (state === 'loading' || state === 'success') return null;
  const copy = message ?? (
    state === 'session-revoked' ? 'Your session is no longer active. Sign in again to continue.' :
    state === 'validation-error' ? 'Check the highlighted account settings and try again.' :
    state === 'destructive-confirm' ? 'Confirm this security-sensitive account change.' :
    state === 'read-only' ? 'This account surface is read-only.' :
    'The account provider could not complete this request.'
  );
  return <div className="p15-account__message" role="alert" tabIndex={-1}>{copy}</div>;
}

export function ProfileSettingsPage() {
  const [state, setState] = useState<AccountState>('loading');
  const [user, setUser] = useState<UserSummary | null>(null);
  const [displayName, setDisplayName] = useState('');
  const alertRef = useRef<HTMLDivElement>(null);

  const load = async () => {
    setState('loading');
    try {
      const me = await currentAccount();
      setUser(me.user);
      setDisplayName(me.user.display_name);
      setState('success');
    } catch (error) {
      setState(stateForFailure(error));
    }
  };

  useEffect(() => { void load(); }, []);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    try {
      const me = await currentAccount();
      const result = await requestJSON<{ user: UserSummary }>('/api/me/profile', {
        method: 'PATCH',
        headers: { 'X-CSRF-Token': me.csrf_token },
        body: JSON.stringify({ display_name: displayName }),
      });
      setUser(result.user);
      setDisplayName(result.user.display_name);
      setState('success');
    } catch (error) {
      setState(stateForFailure(error, true));
      requestAnimationFrame(() => alertRef.current?.focus());
    }
  };

  return (
    <AccountFrame title="Profile" state={state}>
      {state === 'loading' && <p>Loading your account profile…</p>}
      {state === 'session-revoked' && <StateMessage state={state} />}
      {state === 'provider-error' && <StateMessage state={state} message="Your account profile could not be loaded." />}
      {(state === 'success' || state === 'validation-error') && user && (
        <form className="p15-account__form" onSubmit={submit}>
          <div ref={alertRef} tabIndex={-1}>{state === 'validation-error' && <StateMessage state={state} />}</div>
          <label>
            <span>Email</span>
            <input aria-label="Email" value={user.email} readOnly autoComplete="email" />
          </label>
          <label>
            <span>Display name</span>
            <input aria-label="Display name" value={displayName} onChange={(event) => setDisplayName(event.currentTarget.value)} autoComplete="name" />
          </label>
          <div className="p15-account__actions"><Button type="submit">Save profile</Button></div>
        </form>
      )}
    </AccountFrame>
  );
}

export function SecuritySettingsPage() {
  const [state, setState] = useState<AccountState>('loading');
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [message, setMessage] = useState('');
  const alertRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    void currentAccount().then(() => setState('success')).catch((error) => setState(stateForFailure(error)));
  }, []);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    try {
      const me = await currentAccount();
      await requestJSON<{ status: string }>('/api/me/password', {
        method: 'PATCH',
        headers: { 'X-CSRF-Token': me.csrf_token },
        body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
      });
      setCurrentPassword('');
      setNewPassword('');
      setMessage('Password changed. Other active sessions were revoked.');
      setState('success');
    } catch (error) {
      setMessage('The password change was rejected. Verify your current password and new password.');
      setState(stateForFailure(error, true));
      requestAnimationFrame(() => alertRef.current?.focus());
    }
  };

  return (
    <AccountFrame title="Security" state={state}>
      {state === 'loading' && <p>Loading account security…</p>}
      {state === 'session-revoked' && <StateMessage state={state} />}
      {state === 'provider-error' && <StateMessage state={state} message="Account security could not be loaded." />}
      {(state === 'success' || state === 'validation-error') && (
        <form className="p15-account__form" onSubmit={submit}>
          <div ref={alertRef} tabIndex={-1}>{state === 'validation-error' && <StateMessage state={state} message={message} />}</div>
          {state === 'success' && message && <p role="status">{message}</p>}
          <label>
            <span>Current password</span>
            <input aria-label="Current password" type="password" value={currentPassword} onChange={(event) => setCurrentPassword(event.currentTarget.value)} autoComplete="current-password" required />
          </label>
          <label>
            <span>New password</span>
            <input aria-label="New password" type="password" value={newPassword} onChange={(event) => setNewPassword(event.currentTarget.value)} autoComplete="new-password" required />
          </label>
          <div className="p15-account__actions"><Button type="submit">Change password</Button></div>
        </form>
      )}
    </AccountFrame>
  );
}

export function SessionsSettingsPage() {
  const [state, setState] = useState<AccountState>('loading');
  const [sessions, setSessions] = useState<SessionSummary[]>([]);
  const [target, setTarget] = useState<SessionSummary | null>(null);

  const load = async () => {
    setState('loading');
    try {
      await currentAccount();
      const response = await requestJSON<{ sessions: SessionSummary[] }>('/api/me/sessions');
      setSessions(response.sessions);
      setTarget(null);
      setState('success');
    } catch (error) {
      setState(stateForFailure(error));
    }
  };

  useEffect(() => { void load(); }, []);

  const requestRevoke = (item: SessionSummary) => {
    setTarget(item);
    setState('destructive-confirm');
  };

  const confirmRevoke = async () => {
    if (!target) return;
    try {
      const me = await currentAccount();
      await requestJSON<{ status: string }>(`/api/me/sessions/${encodeURIComponent(target.id)}`, {
        method: 'DELETE',
        headers: { 'X-CSRF-Token': me.csrf_token },
      });
      await load();
    } catch (error) {
      setState(stateForFailure(error));
    }
  };

  return (
    <AccountFrame title="Sessions" state={state}>
      {state === 'loading' && <p>Loading active sessions…</p>}
      {state === 'session-revoked' && <StateMessage state={state} />}
      {state === 'provider-error' && <StateMessage state={state} message="Active sessions could not be loaded." />}
      {state === 'destructive-confirm' && target && (
        <div className="p15-account__confirm" role="alertdialog" aria-labelledby="revoke-session-title">
          <h2 id="revoke-session-title">Revoke this session?</h2>
          <p>The selected session will be rejected by the server on its next request.</p>
          <div className="p15-account__actions">
            <Button onClick={() => void confirmRevoke()}>Confirm revoke</Button>
            <Button variant="ghost" onClick={() => { setTarget(null); setState('success'); }}>Cancel</Button>
          </div>
        </div>
      )}
      {(state === 'success' || state === 'destructive-confirm') && (
        <div className="p15-account__list" aria-label="Account sessions">
          {sessions.map((item) => (
            <article key={item.id} className="p15-account__row" data-session-current={item.current ? 'true' : 'false'}>
              <div><strong>{item.current ? 'Current session' : 'Signed-in session'}</strong><p>Status: {item.status}</p><p>Expires: {new Date(item.expires_at).toLocaleString()}</p></div>
              <Button variant="ghost" onClick={() => requestRevoke(item)}>Revoke session</Button>
            </article>
          ))}
        </div>
      )}
    </AccountFrame>
  );
}

export function ConnectedAccountsPage() {
  const [state, setState] = useState<AccountState>('loading');
  const [accounts, setAccounts] = useState<ConnectedAccount[]>([]);
  const [target, setTarget] = useState<ConnectedAccount | null>(null);
  const [message, setMessage] = useState('');

  const load = async () => {
    setState('loading');
    try {
      await currentAccount();
      const response = await requestJSON<{ accounts: ConnectedAccount[] }>('/api/me/connected-accounts');
      setAccounts(response.accounts);
      setTarget(null);
      setMessage('');
      setState('success');
    } catch (error) {
      setState(stateForFailure(error));
    }
  };

  useEffect(() => { void load(); }, []);

  const requestDisconnect = (item: ConnectedAccount) => {
    setTarget(item);
    setState('destructive-confirm');
  };

  const confirmDisconnect = async () => {
    if (!target) return;
    try {
      const me = await currentAccount();
      await requestJSON<{ status: string }>(`/api/me/connected-accounts/${encodeURIComponent(target.provider)}`, {
        method: 'DELETE',
        headers: { 'X-CSRF-Token': me.csrf_token },
      });
      await load();
    } catch (error) {
      setState(stateForFailure(error));
    }
  };

  const startProvider = async (provider: string) => {
    try {
      const me = await currentAccount();
      const result = await requestJSON<{ authorization_url: string }>(`/api/me/connected-accounts/${encodeURIComponent(provider)}/start`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': me.csrf_token },
        body: JSON.stringify({}),
      });
      setMessage(`Provider authorization is ready for ${provider}: ${new URL(result.authorization_url).origin}`);
      setState('success');
    } catch (error) {
      setMessage(`The ${provider} provider is unavailable or incomplete.`);
      setState(stateForFailure(error));
    }
  };

  return (
    <AccountFrame title="Connected accounts" state={state}>
      {state === 'loading' && <p>Loading connected accounts…</p>}
      {state === 'session-revoked' && <StateMessage state={state} />}
      {state === 'provider-error' && <StateMessage state={state} message={message || 'Connected accounts could not be loaded.'} />}
      {state === 'destructive-confirm' && target && (
        <div className="p15-account__confirm" role="alertdialog" aria-labelledby="disconnect-title">
          <h2 id="disconnect-title">Disconnect {target.provider}?</h2>
          <p>The external identity will no longer be bound to this account.</p>
          <div className="p15-account__actions">
            <Button onClick={() => void confirmDisconnect()}>Confirm disconnect</Button>
            <Button variant="ghost" onClick={() => { setTarget(null); setState('success'); }}>Cancel</Button>
          </div>
        </div>
      )}
      {(state === 'success' || state === 'destructive-confirm') && (
        <>
          {message && <p role="status">{message}</p>}
          <div className="p15-account__providers" aria-label="OAuth providers">
            {providers.map((provider) => <Button key={provider} variant="ghost" onClick={() => void startProvider(provider)}>Connect {provider}</Button>)}
          </div>
          <div className="p15-account__list" aria-label="Connected identities">
            {accounts.length === 0 && <p>No external identities are connected.</p>}
            {accounts.map((item) => (
              <article key={item.id} className="p15-account__row">
                <div><strong>{item.provider}</strong><p>{item.display_name || 'Connected identity'}</p></div>
                <Button variant="ghost" onClick={() => requestDisconnect(item)}>Disconnect {item.provider}</Button>
              </article>
            ))}
          </div>
        </>
      )}
    </AccountFrame>
  );
}
