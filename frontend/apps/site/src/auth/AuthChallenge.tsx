import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { GoJetApiError, GoJetAuthClient } from '@gojet/api-client';

type Policy = { turnstile_required?: boolean; turnstile_site_key?: string; turnstile_available?: boolean };
const protectedPaths = new Set(['/api/auth/login', '/api/auth/register', '/api/auth/forgotpassword', '/api/auth/resetpassword', '/api/public/login-email-code', '/api/public/email-code', '/api/public/register-email-code', '/api/auth/verifyemail', '/api/mail/verification', '/api/public/auth/social-registration/complete']);
const defaultClient = new GoJetAuthClient();
const Context = createContext<{ client: GoJetAuthClient; challenge: ReactNode }>({ client: defaultClient, challenge: null });
export const useAuthClient = () => useContext(Context).client;
export const AuthChallenge = () => useContext(Context).challenge;

async function loadWidget() {
  if (!window.turnstile && !document.querySelector('script[data-gojet-turnstile="true"]')) {
    const script = document.createElement('script');
    script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit';
    script.async = true;
    script.defer = true;
    script.dataset.gojetTurnstile = 'true';
    document.head.appendChild(script);
  }
  for (let attempt = 0; attempt < 100; attempt++) {
    if (window.turnstile) return window.turnstile;
    await new Promise((resolve) => window.setTimeout(resolve, 100));
  }
  throw new Error('Verification unavailable');
}

export function AuthChallengeProvider({ children }: { children: ReactNode }) {
  const [policy, setPolicy] = useState<Policy | null>(null);
  const [message, setMessage] = useState('');
  const host = useRef<HTMLDivElement>(null);
  const token = useRef('');
  const widget = useRef('');
  const currentPolicy = useRef<Policy | null>(null);
  const client = useMemo(() => new GoJetAuthClient({ fetch: async (input, init) => {
    const path = new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url, window.location.origin).pathname;
    const protectedMutation = init?.method === 'POST' && protectedPaths.has(path);
    const p = currentPolicy.current;
    if (protectedMutation && (!p || (p.turnstile_required && (!p.turnstile_available || !token.current)))) {
      throw new GoJetApiError(400, 'turnstile_rejected', 'Complete verification before submitting.');
    }
    const headers = new Headers(init?.headers);
    if (protectedMutation && p?.turnstile_required) {
      headers.set('X-Turnstile-Token', token.current);
      token.current = ''; // A concurrent submit cannot reuse this challenge.
    }
    try { return await fetch(input, { ...init, headers }); }
    finally {
      if (protectedMutation && p?.turnstile_required && widget.current) window.turnstile?.reset?.(widget.current);
    }
  } }), []);

  useEffect(() => {
    let cancelled = false;
    fetch('/api/public/auth/providers', { credentials: 'same-origin', headers: { Accept: 'application/json' } })
      .then(async (response) => { if (!response.ok) throw new Error(); return await response.json() as Policy; })
      .then((value) => { if (!cancelled) { currentPolicy.current = value; setPolicy(value); } })
      .catch(() => { if (!cancelled) setMessage('Verification configuration is unavailable. Reload and try again.'); });
    return () => { cancelled = true; token.current = ''; currentPolicy.current = null; };
  }, []);

  useEffect(() => {
    if (!policy?.turnstile_required) return;
    if (!policy.turnstile_available || !policy.turnstile_site_key) { setMessage('Verification is temporarily unavailable.'); return; }
    let cancelled = false;
    let id = '';
    loadWidget().then((api) => {
      if (cancelled || !host.current) return;
      id = api.render(host.current, {
        sitekey: policy.turnstile_site_key, action: 'authentication',
        callback: (value: string) => { if (!cancelled) { token.current = value; setMessage(''); } },
        'expired-callback': () => { if (!cancelled) { token.current = ''; setMessage('Verification expired. Complete the challenge again.'); } },
        'error-callback': () => { if (!cancelled) { token.current = ''; setMessage('Verification failed. Please retry.'); } },
      });
      widget.current = id;
    }).catch(() => { if (!cancelled) setMessage('Verification could not load. Please retry.'); });
    return () => { cancelled = true; token.current = ''; widget.current = ''; if (id) window.turnstile?.remove?.(id); };
  }, [policy]);

  const challenge = <>{policy?.turnstile_required ? <div ref={host} aria-label="Security verification" /> : null}{message ? <p role="alert">{message}</p> : null}</>;
  return <Context.Provider value={{ client, challenge }}>{children}</Context.Provider>;
}
