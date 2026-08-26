import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { Button, Card, InlineMessage } from '@gojet/ui';
import { WebsiteShell } from '../shell/SiteShells';
import { usePrivatePublicPage } from './pageSecurity';

type AbuseState = 'input' | 'submitting' | 'success-persistent' | 'validation-error' | 'Turnstile-error' | 'rate-limited';
type ResourceType = 'short-link-risk' | 'custom-domain-risk';
type Category = 'phishing' | 'malware' | 'spam' | 'scam' | 'impersonation' | 'other';
type Fields = { resourceType: ResourceType; hostname: string; code: string; category: Category; details: string };
type Receipt = { reportId: string; correlationId: string };
type TurnstileAPI = {
  render: (container: HTMLElement, options: Record<string, unknown>) => string;
  remove?: (widgetId: string) => void;
  reset?: (widgetId?: string) => void;
};

declare global {
  interface Window { turnstile?: TurnstileAPI }
}

const initialFields: Fields = { resourceType: 'short-link-risk', hostname: '', code: '', category: 'phishing', details: '' };
const categories: Array<{ value: Category; label: string }> = [
  { value: 'phishing', label: 'Phishing' },
  { value: 'malware', label: 'Malware' },
  { value: 'spam', label: 'Spam' },
  { value: 'scam', label: 'Scam or fraud' },
  { value: 'impersonation', label: 'Impersonation' },
  { value: 'other', label: 'Other abuse' },
];

function newIdempotencyKey() {
  return `p16-public-abuse-${crypto.randomUUID()}`;
}

function validHostname(value: string) {
  const clean = value.trim().toLowerCase();
  return clean.length > 0 && clean.length <= 253 && !clean.includes('://') && !clean.includes('/') && !clean.includes('..') && !clean.startsWith('.') && !clean.endsWith('.');
}

function validCode(value: string) {
  const clean = value.trim();
  return clean.length > 0 && clean.length <= 128 && !/[\\/?#]/.test(clean);
}

function waitForTurnstile(attempts = 50): Promise<TurnstileAPI> {
  if (window.turnstile) return Promise.resolve(window.turnstile);
  if (attempts <= 0) return Promise.reject(new Error('Turnstile did not initialize.'));
  return new Promise((resolve) => window.setTimeout(resolve, 100)).then(() => waitForTurnstile(attempts - 1));
}

function loadTurnstile(): Promise<TurnstileAPI> {
  if (window.turnstile) return Promise.resolve(window.turnstile);
  const existing = document.querySelector<HTMLScriptElement>('script[data-gojet-turnstile="true"]');
  if (existing) return waitForTurnstile();
  const script = document.createElement('script');
  script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit';
  script.async = true;
  script.defer = true;
  script.dataset.gojetTurnstile = 'true';
  document.head.appendChild(script);
  return new Promise<void>((resolve, reject) => {
    script.addEventListener('load', () => resolve(), { once: true });
    script.addEventListener('error', () => reject(new Error('Turnstile script failed to load.')), { once: true });
  }).then(() => waitForTurnstile());
}

export default function AbuseReportPage() {
  usePrivatePublicPage('Report abuse · GoJet');
  const [fields, setFields] = useState<Fields>(initialFields);
  const [state, setState] = useState<AbuseState>('input');
  const [message, setMessage] = useState('');
  const [receipt, setReceipt] = useState<Receipt | null>(null);
  const [turnstileToken, setTurnstileToken] = useState('');
  const [idempotencyKey, setIdempotencyKey] = useState(newIdempotencyKey);
  const turnstileHost = useRef<HTMLDivElement>(null);
  const widgetId = useRef('');

  const testVerification = import.meta.env.VITE_GOJET_TEST_AUTH_ENABLED === '1' && import.meta.env.VITE_GOJET_TEST_TRUST_TURNSTILE_ENABLED === '1';
  const testToken = String(import.meta.env.VITE_GOJET_TEST_TRUST_TURNSTILE_TOKEN ?? '').trim();
  const siteKey = String(import.meta.env.VITE_GOJET_TURNSTILE_SITE_KEY ?? '').trim();
  const verificationConfigured = testVerification ? testToken !== '' : siteKey !== '';

  useEffect(() => {
    if (testVerification) {
      setTurnstileToken(testToken);
      return;
    }
    if (!siteKey || !turnstileHost.current) return;
    let cancelled = false;
    let localWidget = '';
    loadTurnstile().then((api) => {
      if (cancelled || !turnstileHost.current) return;
      localWidget = api.render(turnstileHost.current, {
        sitekey: siteKey,
        action: 'abuse-report',
        callback: (token: string) => {
          if (cancelled) return;
          setTurnstileToken(token);
          setState((current) => current === 'Turnstile-error' ? 'input' : current);
        },
        'expired-callback': () => { if (!cancelled) setTurnstileToken(''); },
        'error-callback': () => {
          if (cancelled) return;
          setTurnstileToken('');
          setState('Turnstile-error');
          setMessage('Verification could not be completed. Refresh the challenge and try again.');
        },
      });
      widgetId.current = localWidget;
    }).catch(() => {
      if (!cancelled) {
        setState('Turnstile-error');
        setMessage('Verification is temporarily unavailable.');
      }
    });
    return () => {
      cancelled = true;
      if (localWidget && window.turnstile?.remove) window.turnstile.remove(localWidget);
      widgetId.current = '';
    };
  }, [siteKey, testToken, testVerification]);

  const cleanFields = useMemo(() => ({
    resourceType: fields.resourceType,
    hostname: fields.hostname.trim().toLowerCase(),
    code: fields.resourceType === 'short-link-risk' ? fields.code.trim() : '',
    category: fields.category,
    details: fields.details.trim(),
  }), [fields]);

  const updateFields = (patch: Partial<Fields>) => {
    setFields((current) => {
      const next = { ...current, ...patch };
      if (patch.resourceType === 'custom-domain-risk') next.code = '';
      return next;
    });
    setReceipt(null);
    setMessage('');
    setState((current) => current === 'submitting' ? current : 'input');
    setIdempotencyKey(newIdempotencyKey());
  };

  const validate = () => {
    if (!validHostname(cleanFields.hostname)) return 'Enter a valid hostname without a scheme, path, or query string.';
    if (cleanFields.resourceType === 'short-link-risk' && !validCode(cleanFields.code)) return 'Enter the short-link code shown in the link.';
    if (cleanFields.details.length > 1000) return 'Details must be 1,000 characters or fewer.';
    return '';
  };

  const resetVerification = () => {
    setTurnstileToken(testVerification ? testToken : '');
    if (!testVerification && widgetId.current && window.turnstile?.reset) window.turnstile.reset(widgetId.current);
  };

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setReceipt(null);
    const validation = validate();
    if (validation) {
      setState('validation-error');
      setMessage(validation);
      return;
    }
    if (!verificationConfigured || !turnstileToken) {
      setState('Turnstile-error');
      setMessage(verificationConfigured ? 'Complete the verification challenge before submitting.' : 'Verification is not configured for this site.');
      return;
    }

    setState('submitting');
    setMessage('');
    try {
      const response = await fetch('/api/public/abuse-reports', {
        method: 'POST',
        credentials: 'same-origin',
        headers: {
          Accept: 'application/json',
          'Content-Type': 'application/json',
          'Idempotency-Key': idempotencyKey,
        },
        body: JSON.stringify({
          resource_type: cleanFields.resourceType,
          hostname: cleanFields.hostname,
          code: cleanFields.code,
          category: cleanFields.category,
          details: cleanFields.details,
          turnstile_token: turnstileToken,
        }),
      });
      const body = await response.json().catch(() => ({})) as {
        report_id?: string;
        correlation_id?: string;
        error?: { code?: string };
      };
      const code = body.error?.code ?? '';
      if (response.status === 429 || code === 'rate_limited') {
        setState('rate-limited');
        setMessage('Too many abuse reports were submitted from this network. Try again later.');
        resetVerification();
        return;
      }
      if (code === 'verification_failed') {
        setState('Turnstile-error');
        setMessage('Verification failed. Complete a fresh challenge and try again.');
        resetVerification();
        return;
      }
      if (!response.ok) {
        setState('validation-error');
        if (code === 'resource_unavailable') setMessage('The reported resource could not be found or is no longer available.');
        else if (code === 'idempotency_conflict') setMessage('This retry no longer matches the earlier submission. Review the form and submit again.');
        else if (code === 'intake_unavailable') setMessage('Abuse reporting is temporarily unavailable. Try again later.');
        else setMessage('Check the report fields and try again.');
        resetVerification();
        return;
      }
      const reportId = String(body.report_id ?? '').trim();
      const correlationId = String(body.correlation_id ?? '').trim();
      if (!reportId) throw new Error('Missing persistent report receipt.');
      setReceipt({ reportId, correlationId });
      setState('success-persistent');
      setMessage('Your report was received and assigned a persistent reference.');
    } catch {
      setState('validation-error');
      setMessage('The report could not be submitted. Your form is still here; try again when the service is available.');
      resetVerification();
    }
  };

  return <WebsiteShell>
    <section className="public-trust-page public-abuse-page" data-page="abuse-report" data-state={state}>
      <header className="public-trust-hero">
        <p className="public-trust-eyebrow">TRUST &amp; SAFETY</p>
        <h1>Report abuse</h1>
        <p>Report a GoJet short link or custom domain that you believe is being used for phishing, malware, spam, fraud, impersonation, or other abuse.</p>
      </header>

      {state === 'success-persistent' && receipt ? <InlineMessage variant="success">{message} Reference: {receipt.reportId}</InlineMessage> : null}
      {state === 'validation-error' ? <InlineMessage variant="danger">{message}</InlineMessage> : null}
      {state === 'Turnstile-error' ? <InlineMessage variant="danger">{message}</InlineMessage> : null}
      {state === 'rate-limited' ? <InlineMessage variant="warning">{message}</InlineMessage> : null}

      <Card as="section" className="public-trust-card" aria-labelledby="report-form-title">
        <h2 id="report-form-title">Abuse report</h2>
        <form className="public-abuse-form" onSubmit={submit} aria-busy={state === 'submitting'}>
          <label>Resource type
            <select value={fields.resourceType} onChange={(event) => updateFields({ resourceType: event.currentTarget.value as ResourceType })} disabled={state === 'submitting'}>
              <option value="short-link-risk">GoJet short link</option>
              <option value="custom-domain-risk">GoJet custom domain</option>
            </select>
          </label>
          <label>Hostname
            <input value={fields.hostname} onChange={(event) => updateFields({ hostname: event.currentTarget.value })} placeholder="go.example.com" autoComplete="off" disabled={state === 'submitting'} />
            <span>Enter only the hostname. Do not paste a full destination URL.</span>
          </label>
          {fields.resourceType === 'short-link-risk' ? <label>Short-link code
            <input value={fields.code} onChange={(event) => updateFields({ code: event.currentTarget.value })} placeholder="example-code" autoComplete="off" disabled={state === 'submitting'} />
          </label> : null}
          <label>Category
            <select value={fields.category} onChange={(event) => updateFields({ category: event.currentTarget.value as Category })} disabled={state === 'submitting'}>
              {categories.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
            </select>
          </label>
          <label>Details <span className="public-abuse-optional">Optional</span>
            <textarea rows={6} maxLength={1000} value={fields.details} onChange={(event) => updateFields({ details: event.currentTarget.value })} disabled={state === 'submitting'} aria-describedby="abuse-details-help" />
            <span id="abuse-details-help">Do not include passwords, access tokens, private keys, payment details, or unnecessary personal information. Submitted text is sanitized server-side.</span>
          </label>

          <div className="public-verification" aria-label="Abuse report verification">
            {testVerification ? <p data-test-verification="deterministic">Deterministic verification is enabled for the test environment.</p> : null}
            {!testVerification && siteKey ? <div ref={turnstileHost} /> : null}
            {!verificationConfigured ? <InlineMessage variant="danger">Verification is unavailable. Submission remains disabled by the server contract.</InlineMessage> : null}
          </div>

          <div className="public-trust-actions">
            <Button type="submit" disabled={state === 'submitting' || state === 'success-persistent'}>{state === 'submitting' ? 'Submitting…' : state === 'success-persistent' ? 'Report received' : 'Submit report'}</Button>
            {state === 'success-persistent' ? <Button type="button" variant="ghost" onClick={() => { setFields(initialFields); setReceipt(null); setMessage(''); setState('input'); setIdempotencyKey(newIdempotencyKey()); resetVerification(); }}>Report another resource</Button> : null}
          </div>
        </form>
      </Card>

      <p className="public-trust-safe-note">GoJet resolves the reported resource on the server. This form does not ask for, display, or confirm the destination URL or internal safety evidence.</p>
    </section>
  </WebsiteShell>;
}
