import { useEffect, useMemo, useRef, useState } from 'react';
import { Link } from '@tanstack/react-router';
import { GoJetApiError, GoJetAuthClient } from '@gojet/api-client';
import type { AuthProvider, SocialRegistrationState } from '@gojet/api-client';
import { AuthShell } from '../shell/SiteShells';

type AuthPageKind = 'login' | 'register' | 'verify' | 'forgot' | 'reset' | 'oauth' | 'social';
type Notice = { tone: 'error' | 'success' | 'info'; text: string } | null;

const client = new GoJetAuthClient();
const providerLabels: Record<AuthProvider['provider'], string> = {
  google: 'Google', facebook: 'Facebook', github: 'GitHub', qq: 'QQ', wechat: 'WeChat', rainbow: 'Rainbow',
};

function safeError(error: unknown): GoJetApiError {
  if (error instanceof GoJetApiError) return error;
  return new GoJetApiError(500, 'internal_error', 'The authentication service could not complete the request.');
}

function authStateFor(error: GoJetApiError, fallback = 'invalid') {
  switch (error.code) {
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
    document.title = `${title} · GoJet`;
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
      <header className="p15-auth__header"><p className="p15-auth__eyebrow">GoJet account</p><h2>{title}</h2></header>
      {notice && <div ref={noticeRef} tabIndex={notice.tone === 'error' ? -1 : undefined} className={`p15-auth__notice p15-auth__notice--${notice.tone}`} role={notice.tone === 'error' ? 'alert' : 'status'} aria-live="polite">{notice.text}</div>}
      {children}
    </section>
  </AuthShell>;
}

function Providers({ intent, providers }: { intent: 'login' | 'register'; providers: AuthProvider[] }) {
  if (providers.length === 0) return <p className="p15-auth__muted">Social sign-in is not configured.</p>;
  return <div className="p15-auth__providers" aria-label="Social authentication providers">
    {providers.map((provider) => <a
      key={provider.provider}
      href={provider.enabled ? `/api/public/auth/${provider.provider}/start?intent=${intent}` : undefined}
      aria-disabled={!provider.enabled}
      className={!provider.enabled ? 'is-disabled' : undefined}
      onClick={(event) => { if (!provider.enabled) event.preventDefault(); }}
    >{providerLabels[provider.provider]}{provider.enabled ? '' : ' unavailable'}</a>)}
  </div>;
}

function LoginPage() {
  const [state, setState] = useState('input');
  const [notice, setNotice] = useState<Notice>(null);
  const [providers, setProviders] = useState<AuthProvider[]>([]);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [emailCode, setEmailCode] = useState('');
  const [codeSent, setCodeSent] = useState(false);

  useEffect(() => { client.providers().then((result) => setProviders(result.providers)).catch(() => setProviders([])); }, []);

  async function submitPassword(event: React.FormEvent) {
    event.preventDefault(); setState('submitting'); setNotice(null);
    try {
      await client.login(email, password);
      setState('success'); setNotice({ tone: 'success', text: 'Signed in successfully. Your session is stored only in the secure server cookie.' });
    } catch (caught) {
      const error = safeError(caught); setState(authStateFor(error)); setNotice({ tone: 'error', text: error.message });
    }
  }
  async function sendCode() {
    setState('submitting'); setNotice(null);
    try { await client.requestLoginCode(email); setCodeSent(true); setState('input'); setNotice({ tone: 'info', text: 'A one-time sign-in code was sent if this account can use email-code sign in.' }); }
    catch (caught) { const error = safeError(caught); setState(authStateFor(error)); setNotice({ tone: 'error', text: error.message }); }
  }
  async function signInWithCode() {
    setState('submitting'); setNotice(null);
    try { await client.loginWithCode(emailCode); setState('success'); setNotice({ tone: 'success', text: 'Signed in with the one-time email code.' }); }
    catch (caught) { const error = safeError(caught); setState(authStateFor(error)); setNotice({ tone: 'error', text: error.message }); }
  }

  return <AuthFrame title="Sign in" state={state} notice={notice}>
    <form className="p15-auth__form" onSubmit={submitPassword}>
      <label htmlFor="login-email">Email</label><input id="login-email" name="email" type="email" autoComplete="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
      <label htmlFor="login-password">Password</label><input id="login-password" name="password" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
      <button type="submit" disabled={state === 'submitting'}>Sign in</button>
    </form>
    <div className="p15-auth__divider"><span>or</span></div>
    <div className="p15-auth__code-login">
      <button type="button" className="p15-auth__secondary" onClick={sendCode} disabled={!email || state === 'submitting'}>Send email sign-in code</button>
      {codeSent && <><label htmlFor="login-code">Email sign-in code</label><input id="login-code" autoComplete="one-time-code" value={emailCode} onChange={(e) => setEmailCode(e.target.value)} /><button type="button" className="p15-auth__secondary" onClick={signInWithCode} disabled={!emailCode || state === 'submitting'}>Sign in with email code</button></>}
    </div>
    <Providers intent="login" providers={providers} />
    <p className="p15-auth__links"><Link to="/forgot-password">Forgot password?</Link><span>New to GoJet? <Link to="/register">Create account</Link></span></p>
  </AuthFrame>;
}

function RegisterPage() {
  const [state, setState] = useState('input');
  const [notice, setNotice] = useState<Notice>(null);
  const [providers, setProviders] = useState<AuthProvider[]>([]);
  const [email, setEmail] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [password, setPassword] = useState('');
  const [code, setCode] = useState('');
  useEffect(() => { client.providers().then((result) => setProviders(result.providers)).catch(() => setProviders([])); }, []);

  async function register(event: React.FormEvent) {
    event.preventDefault(); setState('submitting'); setNotice(null);
    try { await client.register(email, displayName, password); setState('code-sent'); setNotice({ tone: 'success', text: 'Account created. Check your email for the verification code.' }); }
    catch (caught) { const error = safeError(caught); setState(authStateFor(error)); setNotice({ tone: 'error', text: error.message }); }
  }
  async function verify() {
    setState('submitting'); setNotice(null);
    try { await client.verifyRegistrationCode(code); setState('success'); setNotice({ tone: 'success', text: 'Email verified. You can now sign in.' }); }
    catch (caught) { const error = safeError(caught); setState(authStateFor(error, 'code-expired')); setNotice({ tone: 'error', text: error.message }); }
  }

  return <AuthFrame title="Create account" state={state} notice={notice}>
    <form className="p15-auth__form" onSubmit={register}>
      <label htmlFor="register-email">Email</label><input id="register-email" type="email" autoComplete="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
      <label htmlFor="register-name">Display name</label><input id="register-name" autoComplete="name" required value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
      <label htmlFor="register-password">Password</label><input id="register-password" type="password" autoComplete="new-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
      <button type="submit" disabled={state === 'submitting'}>Create account</button>
    </form>
    {(state === 'code-sent' || code !== '') && <div className="p15-auth__verification"><label htmlFor="register-code">Verification code</label><input id="register-code" autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value)} /><button type="button" onClick={verify} disabled={!code || state === 'submitting'}>Verify email</button></div>}
    <Providers intent="register" providers={providers} />
    <p className="p15-auth__links">Already registered? <Link to="/login">Sign in</Link></p>
  </AuthFrame>;
}

function VerifyPage() {
  const initialCode = useMemo(() => new URLSearchParams(window.location.search).get('code') ?? '', []);
  const [code, setCode] = useState(initialCode);
  const [email, setEmail] = useState('');
  const [state, setState] = useState(initialCode ? 'verifying' : 'invalid-token');
  const [notice, setNotice] = useState<Notice>(initialCode ? null : { tone: 'error', text: 'Enter the verification code from your email.' });
  async function verify(event: React.FormEvent) { event.preventDefault(); setState('verifying'); setNotice(null); try { await client.verifyEmail(code); setCode(''); setState('success'); setNotice({ tone: 'success', text: 'Email verified successfully.' }); } catch (caught) { const error = safeError(caught); setState(authStateFor(error, 'invalid-token')); setNotice({ tone: 'error', text: error.message }); } }
  async function resend() { setNotice(null); try { await client.resendVerification(email); setState('verifying'); setNotice({ tone: 'info', text: 'A new verification email was requested.' }); } catch (caught) { const error = safeError(caught); setState(error.code === 'rate_limited' ? 'resend-limited' : authStateFor(error)); setNotice({ tone: 'error', text: error.message }); } }
  return <AuthFrame title="Verify email" state={state} notice={notice}><form className="p15-auth__form" onSubmit={verify}><label htmlFor="verify-code">Verification code</label><input id="verify-code" autoComplete="one-time-code" required value={code} onChange={(e) => setCode(e.target.value)} /><button type="submit">Verify email</button></form><div className="p15-auth__resend"><label htmlFor="verify-email">Email for resend</label><input id="verify-email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} /><button type="button" className="p15-auth__secondary" onClick={resend} disabled={!email}>Resend verification</button></div></AuthFrame>;
}

function ForgotPage() {
  const [email, setEmail] = useState(''); const [state, setState] = useState('input'); const [notice, setNotice] = useState<Notice>(null);
  async function submit(event: React.FormEvent) { event.preventDefault(); setState('submitting'); setNotice(null); try { await client.forgotPassword(email); setState('submitted-neutral'); setNotice({ tone: 'success', text: 'If the account can be recovered, a password reset message will be sent.' }); } catch (caught) { const error = safeError(caught); setState(authStateFor(error)); setNotice({ tone: 'error', text: error.message }); } }
  return <AuthFrame title="Reset your password" state={state} notice={notice}><form className="p15-auth__form" onSubmit={submit}><label htmlFor="forgot-email">Email</label><input id="forgot-email" type="email" autoComplete="email" required value={email} onChange={(e) => setEmail(e.target.value)} /><button type="submit">Send reset instructions</button></form><p className="p15-auth__links"><Link to="/login">Back to sign in</Link></p></AuthFrame>;
}

function ResetPage() {
  const token = useMemo(() => new URLSearchParams(window.location.search).get('token') ?? '', []);
  const [password, setPassword] = useState(''); const [state, setState] = useState(token ? 'input' : 'invalid-token'); const [notice, setNotice] = useState<Notice>(token ? null : { tone: 'error', text: 'This password reset link is not valid.' });
  async function submit(event: React.FormEvent) { event.preventDefault(); if (!token) return; setState('submitting'); setNotice(null); try { await client.resetPassword(token, password); setState('success'); setNotice({ tone: 'success', text: 'Password reset successfully. You can sign in with the new password.' }); } catch (caught) { const error = safeError(caught); setState(authStateFor(error, 'invalid-token')); setNotice({ tone: 'error', text: error.message }); } }
  return <AuthFrame title="Choose a new password" state={state} notice={notice}><form className="p15-auth__form" onSubmit={submit}><label htmlFor="reset-password">New password</label><input id="reset-password" type="password" autoComplete="new-password" required disabled={!token} value={password} onChange={(e) => setPassword(e.target.value)} /><button type="submit" disabled={!token || state === 'submitting'}>Reset password</button></form></AuthFrame>;
}

function OAuthCallbackPage() {
  const [state, setState] = useState('processing'); const [notice, setNotice] = useState<Notice>({ tone: 'info', text: 'Finishing sign in with your identity provider…' }); const [registrationHref, setRegistrationHref] = useState('');
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      const parts = window.location.pathname.split('/').filter(Boolean); const provider = parts[1] ?? '';
      const query = new URLSearchParams(window.location.search); const providerState = query.get('state') ?? ''; const providerCode = query.get('code') ?? '';
      try {
        const callback = await client.oauthCallback(provider, providerState, providerCode);
        if (cancelled) return;
        const exchange = await client.exchangeHandoff(callback.handoff_code);
        if (cancelled) return;
        if (exchange.status === 'authenticated') { setState('login-success'); setNotice({ tone: 'success', text: 'Provider sign in completed.' }); }
        else { const href = `/social-registration?code=${encodeURIComponent(exchange.registration_code)}`; setRegistrationHref(href); setState('registration-required'); setNotice({ tone: 'info', text: 'One more step is required to finish registration.' }); }
      } catch (caught) { if (cancelled) return; const error = safeError(caught); setState(authStateFor(error, 'provider-error')); setNotice({ tone: 'error', text: error.code === 'state_error' ? 'The provider response could not be validated. Start sign in again.' : error.message }); }
    })();
    return () => { cancelled = true; };
  }, []);
  return <AuthFrame title="Provider sign in" state={state} notice={notice}><p className="p15-auth__muted">Provider authorization codes, access tokens and callback parameters are never shown on this page or stored in browser storage.</p>{state === 'registration-required' && registrationHref && <a href={registrationHref}>Continue registration</a>}</AuthFrame>;
}

function SocialPage() {
  const code = useMemo(() => new URLSearchParams(window.location.search).get('code') ?? '', []);
  const [registration, setRegistration] = useState<SocialRegistrationState | null>(null); const [email, setEmail] = useState(''); const [verificationCode, setVerificationCode] = useState(''); const [state, setState] = useState('loading-handoff'); const [notice, setNotice] = useState<Notice>(null);
  useEffect(() => { let cancelled = false; if (!code) { setState('expired-handoff'); setNotice({ tone: 'error', text: 'This social registration handoff is not valid.' }); return; } client.socialRegistration(code).then((result) => { if (cancelled) return; setRegistration(result); setEmail(result.email); setState(result.requires_email_verification && !result.email ? 'missing-provider-email' : 'form'); }).catch((caught) => { if (cancelled) return; const error = safeError(caught); setState(authStateFor(error, 'expired-handoff')); setNotice({ tone: 'error', text: error.message }); }); return () => { cancelled = true; }; }, [code]);
  async function submit(event: React.FormEvent) { event.preventDefault(); setNotice(null); try { const result = await client.completeSocialRegistration(code, email, verificationCode); if (result.status === 'verification_required') { setState('email-code'); setNotice({ tone: 'info', text: 'Enter the verification code sent to your email.' }); } else { setState('success'); setNotice({ tone: 'success', text: 'Account registration completed.' }); } } catch (caught) { const error = safeError(caught); setState(authStateFor(error, 'conflict')); setNotice({ tone: 'error', text: error.message }); } }
  return <AuthFrame title="Finish social registration" state={state} notice={notice}>{registration && <form className="p15-auth__form" onSubmit={submit}><p className="p15-auth__provider-context">Provider: <strong>{providerLabels[registration.provider]}</strong></p><label htmlFor="social-email">Email</label><input id="social-email" type="email" required value={email} onChange={(e) => setEmail(e.target.value)} /><label htmlFor="social-code">Email verification code</label><input id="social-code" autoComplete="one-time-code" value={verificationCode} onChange={(e) => setVerificationCode(e.target.value)} /><button type="submit">Finish registration</button></form>}</AuthFrame>;
}

export function AuthRoutePage({ kind }: { kind: AuthPageKind }) {
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
