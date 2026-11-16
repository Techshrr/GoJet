import { useEffect, useMemo, useRef, useState } from 'react';
import { Link } from '@tanstack/react-router';
import { GoJetApiError } from '@gojet/api-client';
import { FeedbackIcon } from '@gojet/ui';
import type { AuthProvider, SocialRegistrationState } from '@gojet/api-client';
import { AuthShell } from '../shell/SiteShells';
import { AuthChallenge, AuthChallengeProvider, useAuthClient } from './AuthChallenge';

type AuthPageKind = 'login' | 'register' | 'verify' | 'forgot' | 'reset' | 'oauth' | 'social';
type Notice = { tone: 'error' | 'success' | 'info'; text: string } | null;

const providerLabels: Record<AuthProvider['provider'], string> = {
  google: 'Google', facebook: 'Facebook', github: 'GitHub', qq: 'QQ', wechat: 'WeChat', rainbow: 'Rainbow', x: 'X', linkedin: 'LinkedIn',
};

function safeError(error: unknown): GoJetApiError {
  if (error instanceof GoJetApiError) return error;
  return new GoJetApiError(500, 'internal_error', 'The authentication service could not complete the request.');
}

function authStateFor(error: GoJetApiError, fallback = 'invalid') {
  switch (error.code) {
    case 'turnstile_rejected': return 'Turnstile-required';
    case 'account_locked': return 'account-locked';
    case 'verification_required': return 'verification-required';
    case 'rate_limited': return 'rate-limited';
    case 'expired_token': return 'expired-token';
    case 'reused_token': return 'reused-token';
    case 'state_error': return 'state-error';
    case 'provider_error': return 'provider-error';
    case 'conflict': return 'conflict';
    case 'email_required': return 'missing-provider-email';
    default: return fallback;
  }
}

function useAuthDocument(title: string) {
  useEffect(() => {
    const priorTitle = document.title;
    document.title = `${title} · Linketic`;
    let robots = document.querySelector<HTMLMetaElement>('meta[name="robots"]');
    const created = !robots;
    if (!robots) {
      robots = document.createElement('meta');
      robots.name = 'robots';
      document.head.appendChild(robots);
    }
    const priorRobots = robots.content;
    robots.content = 'noindex,nofollow';
    return () => {
      document.title = priorTitle;
      if (created) robots?.remove(); else if (robots) robots.content = priorRobots;
    };
  }, [title]);
}

function AuthFrame({ title, state, notice, children }: { title: string; state: string; notice: Notice; children: React.ReactNode }) {
  useAuthDocument(title);
  const noticeRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (notice?.tone === 'error') requestAnimationFrame(() => noticeRef.current?.focus());
  }, [notice]);
  return <AuthShell state={state === 'submitting' ? 'submitting' : state === 'rate-limited' ? 'rate-limited' : state === 'provider-error' ? 'provider-error' : 'normal'}>
    <section className="p15-auth" data-auth-page={title.toLowerCase().replaceAll(' ', '-')} data-auth-state={state}>
      <header className="p15-auth__header"><h2>{title}</h2></header>
      {notice && <div ref={noticeRef} tabIndex={notice.tone === 'error' ? -1 : undefined} className={`p15-auth__notice p15-auth__notice--${notice.tone}`} data-notice-tone={notice.tone} role={notice.tone === 'error' ? 'alert' : 'status'} aria-live="polite"><FeedbackIcon variant={notice.tone === 'error' ? 'danger' : notice.tone} /><span>{notice.text}</span></div>}
      <AuthChallenge />
      {children}
    </section>
  </AuthShell>;
}

function Providers({ intent, providers }: { intent: 'login' | 'register'; providers: AuthProvider[] }) {
  const available = providers.filter((provider) => provider.enabled);
  if (available.length === 0) return null;
  return <div className="p15-auth__providers" role="group" aria-label="Social authentication providers">
    {available.map((provider) => <a key={provider.provider} href={`/api/public/auth/${provider.provider}/start?intent=${intent}`}>{providerLabels[provider.provider]}</a>)}
  </div>;
}

async function enterWorkspace() {
  const response = await fetch('/api/me', { credentials: 'same-origin', cache: 'no-store' });
  if (!response.ok) throw new Error('Your session could not be saved. Please sign in again.');
  window.location.replace('/app');
}

function CodeField({ id, value, onChange, label = 'Six-digit code' }: { id: string; value: string; onChange: (value: string) => void; label?: string }) {
  return <><label htmlFor={id}>{label}</label><input className="p15-auth__otp" id={id} type="text" inputMode="numeric" pattern="[0-9]{6}" maxLength={6} autoComplete="one-time-code" placeholder="000000" required value={value} onChange={(event) => onChange(event.target.value.replace(/\D/g, '').slice(0, 6))} /><small>Enter the 6 numbers from your email. You can paste the entire code.</small></>;
}

function Resend({ send, initial = 0 }: { send: () => Promise<unknown>; initial?: number }) {
  const [seconds, setSeconds] = useState(initial);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  useEffect(() => { if (!seconds) return; const timer = window.setTimeout(() => setSeconds(seconds - 1), 1000); return () => clearTimeout(timer); }, [seconds]);
  async function submit() {
    setBusy(true); setMessage('');
    try { await send(); setSeconds(60); setMessage('New code requested. Check your inbox and spam folder. Use the most recent email.'); }
    catch (error) { setMessage(safeError(error).message); }
    finally { setBusy(false); }
  }
  return <div className="p15-auth__resend"><span>Didn’t receive the email?</span><button type="button" className="p15-auth__text-button" disabled={busy || seconds > 0} onClick={() => void submit()}>{busy ? 'Sending…' : seconds ? `Resend in ${seconds}s` : 'Resend code'}</button>{message && <p role="status">{message}</p>}</div>;
}

function VerifyForm({ email, initial = 0, onError }: { email: string; initial?: number; onError: (error: unknown) => void }) {
  const client = useAuthClient(); const [code, setCode] = useState(''); const [busy, setBusy] = useState(false);
  async function submit(event: React.FormEvent) {
    event.preventDefault(); setBusy(true);
    try { await client.verifyEmail(code, email); await enterWorkspace(); }
    catch (error) { onError(error); setBusy(false); }
  }
  return <><p className="p15-auth__description">Verify <strong>{email}</strong> to open your account.</p><form className="p15-auth__form" onSubmit={submit}><CodeField id="verify-code" value={code} onChange={setCode} label="Verification code" /><button disabled={busy || code.length !== 6}>{busy ? 'Verifying…' : 'Verify and continue'}</button></form><Resend initial={initial} send={() => client.resendVerification(email)} /></>;
}

function LoginPage() {
  const client = useAuthClient();
  const [state, setState] = useState('input'); const [notice, setNotice] = useState<Notice>(null);
  const [providers, setProviders] = useState<AuthProvider[]>([]);
  const [email, setEmail] = useState(''); const [password, setPassword] = useState('');
  const [mode, setMode] = useState<'password' | 'code' | 'verify'>('password');
  const [code, setCode] = useState(''); const [sent, setSent] = useState(false);
  useEffect(() => { client.providers().then((result) => setProviders(result.providers)).catch(() => setProviders([])); }, []);
  function fail(caught: unknown) { const error = safeError(caught); setState(authStateFor(error)); setNotice({ tone: 'error', text: error.message }); if (error.code === 'verification_required') setMode('verify'); }
  async function submit(event: React.FormEvent) {
    event.preventDefault(); setState('submitting'); setNotice(null);
    try {
      if (mode === 'code' && !sent) { await client.requestLoginCode(email); setSent(true); setState('input'); setNotice({ tone: 'info', text: `Enter the six-digit code sent to ${email}.` }); return; }
      if (mode === 'code') await client.loginWithCode(code, email); else await client.login(email, password);
      await enterWorkspace();
    } catch (error) { fail(error); }
  }
  return <AuthFrame title={mode === 'verify' ? 'Verify your email' : 'Welcome back'} state={state} notice={notice}>
    {mode === 'verify' ? <VerifyForm email={email} onError={fail} /> : <>
      <p className="p15-auth__description">Sign in to manage your links and workspace.</p>
      <Providers intent="login" providers={providers} />
      <div className="p15-auth__tabs" aria-label="Sign-in method"><button type="button" aria-pressed={mode === 'password'} onClick={() => { setMode('password'); setNotice(null); }}>Password</button><button type="button" aria-pressed={mode === 'code'} onClick={() => { setMode('code'); setNotice(null); }}>Email code</button></div>
      <form className="p15-auth__form" onSubmit={submit}>
        <label htmlFor="login-email">Email</label><input id="login-email" type="email" autoComplete="email" required readOnly={sent && mode === 'code'} value={email} onChange={(event) => setEmail(event.target.value)} />
        {mode === 'password' ? <><div className="p15-auth__label-row"><label htmlFor="login-password">Password</label><Link to="/forgot-password">Forgot password?</Link></div><input id="login-password" type="password" autoComplete="current-password" required value={password} onChange={(event) => setPassword(event.target.value)} /></> : sent && <CodeField id="login-code" label="Email sign-in code" value={code} onChange={setCode} />}
        <button type="submit" disabled={state === 'submitting'}>{state === 'submitting' ? 'Please wait…' : mode === 'code' && !sent ? 'Send sign-in code' : 'Sign in'}</button>
      </form>
      {mode === 'code' && sent && <><Resend initial={60} send={() => client.requestLoginCode(email)} /><button className="p15-auth__text-button" type="button" onClick={() => { setSent(false); setCode(''); }}>Use another email</button></>}
    </>}
    <p className="p15-auth__links">New to Linketic? <Link to="/register">Create an account</Link></p>
    {mode !== 'verify' && <Link className="p15-auth__help-link" to="/verify-email">Verify email or resend a code</Link>}
  </AuthFrame>;
}

function RegisterPage() {
  const client = useAuthClient(); const [state, setState] = useState('input'); const [notice, setNotice] = useState<Notice>(null);
  const [providers, setProviders] = useState<AuthProvider[]>([]); const [email, setEmail] = useState(''); const [displayName, setDisplayName] = useState(''); const [password, setPassword] = useState(''); const [registered, setRegistered] = useState(false);
  useEffect(() => { client.providers().then((result) => setProviders(result.providers)).catch(() => setProviders([])); }, []);
  function fail(caught: unknown) { const error = safeError(caught); setState(authStateFor(error)); setNotice({ tone: 'error', text: error.message }); }
  async function register(event: React.FormEvent) {
    event.preventDefault(); setState('submitting'); setNotice(null);
    try { await client.register(email, displayName, password); setRegistered(true); setPassword(''); setState('code-sent'); }
    catch (error) { fail(error); }
  }
  return <AuthFrame title={registered ? 'Check your email' : 'Create your account'} state={state} notice={notice}>
    {registered ? <VerifyForm email={email} initial={60} onError={fail} /> : <><p className="p15-auth__description">One place for your links, QR codes and files.</p><Providers intent="register" providers={providers} /><form className="p15-auth__form" onSubmit={register}>
      <label htmlFor="register-name">Your name</label><input id="register-name" autoComplete="name" required value={displayName} onChange={(event) => setDisplayName(event.target.value)} />
      <label htmlFor="register-email">Email</label><input id="register-email" type="email" autoComplete="email" required value={email} onChange={(event) => setEmail(event.target.value)} />
      <label htmlFor="register-password">Password</label><input id="register-password" type="password" minLength={12} autoComplete="new-password" required value={password} onChange={(event) => setPassword(event.target.value)} /><small>Use at least 12 characters.</small>
      <button type="submit" disabled={state === 'submitting'}>{state === 'submitting' ? 'Creating account…' : 'Create account'}</button>
    </form><p className="p15-auth__legal">By creating an account, you agree to our <a href="/legal/terms">Terms</a> and <a href="/legal/privacy">Privacy Policy</a>.</p></>}
    <p className="p15-auth__links">Already have an account? <Link to="/login">Sign in</Link></p>
  </AuthFrame>;
}

function VerifyPage() {
  const [email, setEmail] = useState(''); const [ready, setReady] = useState(false); const [notice, setNotice] = useState<Notice>(null);
  return <AuthFrame title="Verify your email" state="input" notice={notice}>{ready ? <VerifyForm email={email} onError={(error) => setNotice({ tone: 'error', text: safeError(error).message })} /> : <form className="p15-auth__form" onSubmit={(event) => { event.preventDefault(); setReady(true); }}><p>Use the email address you registered with.</p><label htmlFor="verify-email">Email</label><input id="verify-email" type="email" autoComplete="email" required value={email} onChange={(event) => setEmail(event.target.value)} /><button>Continue</button></form>}<p className="p15-auth__links"><Link to="/login">Back to sign in</Link></p></AuthFrame>;
}

function ForgotPage() {
  const client = useAuthClient(); const [email, setEmail] = useState(''); const [code, setCode] = useState(''); const [password, setPassword] = useState('');
  const [sent, setSent] = useState(false); const [state, setState] = useState('input'); const [notice, setNotice] = useState<Notice>(null);
  async function submit(event: React.FormEvent) {
    event.preventDefault(); setState('submitting'); setNotice(null);
    try {
      if (!sent) { await client.forgotPassword(email); setSent(true); setState('input'); setNotice({ tone: 'info', text: 'If this account can be recovered, a six-digit code will arrive by email. Enter it here with your new password.' }); }
      else { await client.resetPassword(code, password, email); await enterWorkspace(); }
    } catch (error) { setState('input'); setNotice({ tone: 'error', text: safeError(error).message }); }
  }
  return <AuthFrame title="Reset your password" state={state} notice={notice}>
    <p className="p15-auth__description">Get a code by email, then set your new password here.</p>
    <form className="p15-auth__form" onSubmit={submit}>
      <label htmlFor="forgot-email">Email</label><input id="forgot-email" type="email" autoComplete="email" required readOnly={sent} value={email} onChange={(event) => setEmail(event.target.value)} />
      {sent && <><CodeField id="reset-code" label="Reset code" value={code} onChange={setCode} /><label htmlFor="reset-password">New password</label><input id="reset-password" type="password" autoComplete="new-password" minLength={12} required value={password} onChange={(event) => setPassword(event.target.value)} /><small>Use at least 12 characters. Other signed-in sessions will be closed.</small></>}
      <button disabled={state === 'submitting'}>{state === 'submitting' ? 'Please wait…' : sent ? 'Reset password and continue' : 'Send reset code'}</button>
    </form>
    {sent && <><Resend initial={60} send={() => client.forgotPassword(email)} /><button className="p15-auth__text-button" type="button" onClick={() => { setSent(false); setCode(''); setPassword(''); setNotice(null); }}>Use another email</button></>}
    <p className="p15-auth__links"><Link to="/login">Back to sign in</Link></p>
  </AuthFrame>;
}
function ResetPage() { return <ForgotPage />; }

function OAuthCallbackPage() {
  const client = useAuthClient();
  const [state, setState] = useState('processing');
  const [notice, setNotice] = useState<Notice>({ tone: 'info', text: 'Finishing sign in with your identity provider…' });
  const [registrationHref, setRegistrationHref] = useState('');
  const input = useMemo(() => {
    const query = new URLSearchParams(window.location.search);
    return { provider: window.location.pathname.split('/').filter(Boolean)[1] ?? '', state: query.get('state') ?? '', code: query.get('code') ?? '' };
  }, []);
  const operation = useRef<Promise<{ destination: string; registration: boolean }> | null>(null);
  useEffect(() => {
    let cancelled = false;
    // Keep callback credentials in memory only; remove them from browser history.
    window.history.replaceState(window.history.state, '', window.location.pathname);
    // StrictMode can rerun effects. Complete the one-time exchange once, even
    // when the first effect's UI subscription has already been cleaned up.
    operation.current ??= (async () => {
      const callback = await client.oauthCallback(input.provider, input.state, input.code);
      if (callback.status === 'binding_required') {
        await client.completeConnectedAccount(input.provider, input.state, input.code);
        return { destination: '/app/settings/connected-accounts', registration: false };
      }
      const exchange = await client.exchangeHandoff(callback.handoff_code);
      return exchange.status === 'authenticated'
        ? { destination: '/app', registration: false }
        : { destination: `/social-registration?code=${encodeURIComponent(exchange.registration_code)}`, registration: true };
    })();
    void operation.current.then((result) => {
      if (cancelled) return;
      if (result.registration) {
        setRegistrationHref(result.destination);
        setState('registration-required');
        setNotice({ tone: 'info', text: 'One more step is required to finish registration.' });
      } else {
        window.location.replace(result.destination);
      }
    }).catch((caught: unknown) => {
      if (cancelled) return;
      const error = safeError(caught);
      setState(authStateFor(error, 'provider-error'));
      setNotice({ tone: 'error', text: error.code === 'state_error' ? 'The provider response could not be validated. Start sign in again.' : error.message });
    });
    return () => { cancelled = true; };
  }, [input]);
  return <AuthFrame title="Provider sign in" state={state} notice={notice}>{state === 'registration-required' && registrationHref && <a href={registrationHref}>Continue registration</a>}</AuthFrame>;
}

function SocialPage() {
  const client = useAuthClient();
  const code = useMemo(() => new URLSearchParams(window.location.search).get('code') ?? '', []);
  const [registration, setRegistration] = useState<SocialRegistrationState | null>(null); const [email, setEmail] = useState(''); const [verificationCode, setVerificationCode] = useState(''); const [state, setState] = useState('loading-handoff'); const [notice, setNotice] = useState<Notice>(null);
  useEffect(() => { let cancelled = false; if (!code) { setState('expired-handoff'); setNotice({ tone: 'error', text: 'This social registration handoff is not valid.' }); return; } client.socialRegistration(code).then((result) => { if (cancelled) return; setRegistration(result); setEmail(result.email); setState(result.requires_email_verification && !result.email ? 'missing-provider-email' : 'form'); }).catch((caught) => { if (cancelled) return; const error = safeError(caught); setState(authStateFor(error, 'expired-handoff')); setNotice({ tone: 'error', text: error.message }); }); return () => { cancelled = true; }; }, [code]);
  async function submit(event: React.FormEvent) { event.preventDefault(); setNotice(null); try { const result = await client.completeSocialRegistration(code, email, verificationCode); if (result.status === 'verification_required') { setState('email-code'); setNotice({ tone: 'info', text: 'Enter the verification code sent to your email.' }); } else { await enterWorkspace(); } } catch (caught) { const error = safeError(caught); setState(authStateFor(error, 'conflict')); setNotice({ tone: 'error', text: error.message }); } }
  return <AuthFrame title="Finish social registration" state={state} notice={notice}>{registration && <form className="p15-auth__form" onSubmit={submit}><p className="p15-auth__provider-context">Provider: <strong>{providerLabels[registration.provider]}</strong></p><label htmlFor="social-email">Email</label><input id="social-email" type="email" autoComplete="email" required value={email} onChange={(e) => setEmail(e.target.value)} /><label htmlFor="social-code">Email verification code</label><input id="social-code" inputMode="numeric" pattern="[0-9]{6}" maxLength={6} autoComplete="one-time-code" value={verificationCode} onChange={(e) => setVerificationCode(e.target.value)} /><button type="submit">Finish registration</button></form>}</AuthFrame>;
}

function AuthPageContent({ kind }: { kind: AuthPageKind }) {
  switch (kind) {
    case 'login': return <LoginPage />;
    case 'register': return <RegisterPage />;
    case 'verify': return <VerifyPage />;
    case 'forgot': return <ForgotPage />;
    case 'reset': return <ResetPage />;
    case 'oauth': return <OAuthCallbackPage />;
    case 'social': return <SocialPage />;
  }
}

export function AuthRoutePage({ kind }: { kind: AuthPageKind }) {
  if (kind === 'oauth') return <AuthPageContent kind={kind} />;
  return <AuthChallengeProvider key={kind}><AuthPageContent kind={kind} /></AuthChallengeProvider>;
}
