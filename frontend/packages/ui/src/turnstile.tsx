import { useCallback, useEffect, useRef, useState } from 'react';

type WidgetAPI = {
  render: (container: HTMLElement, options: Record<string, unknown>) => string;
  remove?: (id: string) => void;
  reset?: (id?: string) => void;
};
const api = () => (window as Window & { turnstile?: WidgetAPI }).turnstile;
async function loadWidget() {
  if (!api() && !document.querySelector('script[data-gojet-turnstile="true"]')) {
    const script = document.createElement('script');
    script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit';
    script.async = true;
    script.defer = true;
    script.dataset.gojetTurnstile = 'true';
    document.head.appendChild(script);
  }
  for (let attempt = 0; attempt < 100; attempt++) {
    const current = api();
    if (current) return current;
    await new Promise((resolve) => window.setTimeout(resolve, 100));
  }
  throw new Error('Verification unavailable');
}

// Test tokens are accepted only when the caller explicitly selects fixture mode.
// Production tokens stay in component memory and are consumed before submission.
export function useTurnstile({ siteKey, action, testMode = false, testToken = '' }: { siteKey: string; action: string; testMode?: boolean; testToken?: string }) {
  const host = useRef<HTMLDivElement>(null);
  const id = useRef('');
  const value = useRef('');
  const [token, setToken] = useState(testMode ? testToken : '');
  const [message, setMessage] = useState('');
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    value.current = '';
    if (testMode) { setToken(testToken); return; }
    setToken('');
    if (!siteKey) { setMessage('Verification is temporarily unavailable.'); return; }
    let cancelled = false;
    let widget = '';
    setMessage('Loading verification…');
    loadWidget().then((current) => {
      if (cancelled || !host.current) return;
      widget = current.render(host.current, {
        sitekey: siteKey, action,
        callback: (next: string) => { if (!cancelled) { value.current = next; setToken(next); setMessage(''); } },
        'expired-callback': () => { if (!cancelled) { value.current = ''; setToken(''); setMessage('Verification expired. Please retry.'); } },
        'error-callback': () => { if (!cancelled) { value.current = ''; setToken(''); setMessage('Verification failed. Please retry.'); } },
      });
      id.current = widget;
    }).catch(() => { if (!cancelled) setMessage('Verification could not load. Please retry.'); });
    return () => { cancelled = true; value.current = ''; id.current = ''; if (widget) api()?.remove?.(widget); };
  }, [siteKey, action, testMode, testToken, attempt]);
  const takeToken = useCallback(() => {
    if (testMode) return testToken;
    const current = value.current;
    value.current = '';
    setToken('');
    return current;
  }, [testMode, testToken]);
  const reset = useCallback(() => { if (!testMode && id.current) api()?.reset?.(id.current); }, [testMode]);
  const widget = testMode ? null : <div><div ref={host} aria-label="Security verification" />{message ? <p role="status">{message}</p> : null}{message && message !== 'Loading verification…' ? <button type="button" onClick={() => setAttempt((current) => current + 1)}>Retry verification</button> : null}</div>;
  return { token, takeToken, reset, widget };
}
