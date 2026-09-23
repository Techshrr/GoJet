import { useEffect } from 'react';
import { GoJetAuthClient } from '@gojet/api-client';

type Identity = {
  initialize(options: { client_id: string; nonce: string; auto_select: boolean; use_fedcm_for_prompt: boolean; callback: (value: { credential: string }) => void }): void;
  prompt(): void;
  cancel(): void;
};
type GoogleWindow = Window & { google?: { accounts?: { id?: Identity } } };
let loading: Promise<Identity> | undefined;
type Challenge = { enabled: boolean; client_id: string; state: string; nonce: string };
let pendingChallenge: Promise<Challenge> | undefined;
function identity(): Promise<Identity> {
  const current = (window as GoogleWindow).google?.accounts?.id;
  if (current) return Promise.resolve(current);
  if (!loading) loading = new Promise<Identity>((resolve, reject) => {
    const script = document.createElement('script');
    script.src = 'https://accounts.google.com/gsi/client';
    script.async = true;
    script.onload = () => {
      const id = (window as GoogleWindow).google?.accounts?.id;
      if (id) resolve(id); else reject(new Error('Identity unavailable'));
    };
    script.onerror = () => { script.remove(); loading = undefined; reject(new Error('Identity unavailable')); };
    document.head.appendChild(script);
  });
  return loading;
}

// No token or account data is placed in storage. A timestamp only limits prompts.
export function GoogleOneTap() {
  useEffect(() => {
    const path = window.location.pathname;
    if (/^\/(oauth|social-registration|verify|forgot|reset)/.test(path)) return;
    const key = 'gojet-one-tap-last-prompt';
    try { if (Date.now() - Number(localStorage.getItem(key) || 0) < 86400000) return; } catch { return; }
    let cancelled = false;
    let id: Identity | undefined;
    async function post<T>(url: string, body: object): Promise<T> {
      const response = await fetch(url, { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
      if (!response.ok) throw new Error('Sign in unavailable');
      return response.json() as Promise<T>;
    }
    void (async () => {
      pendingChallenge ??= post<Challenge>('/api/public/auth/google/one-tap/start', {}).catch((error: unknown) => { pendingChallenge = undefined; throw error; });
      const challenge = await pendingChallenge;
      if (cancelled || !challenge.enabled) return;
      id = await identity();
      if (cancelled) return;
      id.initialize({ client_id: challenge.client_id, nonce: challenge.nonce, auto_select: false, use_fedcm_for_prompt: true,
        callback: ({ credential }) => {
          if (cancelled) return;
          void (async () => {
            const result = await post<{ handoff_code: string }>('/api/public/auth/google/one-tap/complete', { state: challenge.state, credential });
            if (cancelled || !result.handoff_code) return;
            const exchange = await new GoJetAuthClient().exchangeHandoff(result.handoff_code);
            if (cancelled) return;
            window.location.assign(exchange.status === 'authenticated' ? '/app' : `/social-registration?code=${encodeURIComponent(exchange.registration_code)}`);
          })().catch(() => { /* The ordinary sign-in button remains available. */ });
        },
      });
      try { localStorage.setItem(key, String(Date.now())); } catch { return; }
      id.prompt();
    })().catch(() => { /* Disabled/unavailable identity must not break the page. */ });
    return () => { cancelled = true; id?.cancel(); };
  }, []);
  return null;
}
