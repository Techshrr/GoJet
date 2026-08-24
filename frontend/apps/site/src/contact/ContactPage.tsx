import { useState, type FormEvent } from 'react';
import { Button, InlineMessage } from '@gojet/ui';
import { WebsiteShell } from '../shell/SiteShells';

type ContactState = 'input' | 'submitting' | 'success-persistent' | 'validation-error' | 'Turnstile-error' | 'rate-limited';
type ContactFields = { name: string; email: string; subject: string; message: string };
const emptyFields: ContactFields = { name: '', email: '', subject: '', message: '' };

export default function ContactPage() {
  const [fields, setFields] = useState<ContactFields>(emptyFields);
  const [state, setState] = useState<ContactState>('input');
  const [reference, setReference] = useState('');
  const testTurnstile = import.meta.env.VITE_GOJET_TEST_AUTH_ENABLED === '1' && import.meta.env.VITE_GOJET_TEST_SUPPORT_TURNSTILE_ENABLED === '1';
  const turnstileToken = testTurnstile ? String(import.meta.env.VITE_GOJET_TEST_SUPPORT_TURNSTILE_TOKEN ?? '').trim() : '';
  const update = (key: keyof ContactFields, value: string) => { setFields((current) => ({ ...current, [key]: value })); if (state !== 'submitting' && state !== 'success-persistent') setState('input'); };
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const clean = Object.fromEntries(Object.entries(fields).map(([key, value]) => [key, value.trim()])) as ContactFields;
    if (!clean.name || !clean.email || !clean.subject || !clean.message) { setState('validation-error'); return; }
    setState('submitting');
    try {
      const response = await fetch('/api/public/contact', {
        method: 'POST', credentials: 'same-origin',
        headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'Idempotency-Key': crypto.randomUUID() },
        body: JSON.stringify({ ...clean, turnstile_token: turnstileToken }),
      });
      const body = await response.json().catch(() => ({})) as { ticket_id?: string; error?: { code?: string } };
      if (response.status === 429 || body.error?.code === 'rate_limited') { setState('rate-limited'); return; }
      if (body.error?.code === 'turnstile_rejected') { setState('Turnstile-error'); return; }
      if (!response.ok) { setState('validation-error'); return; }
      setReference(String(body.ticket_id ?? 'received'));
      setState('success-persistent');
    } catch { setState('validation-error'); }
  };
  return <WebsiteShell><section className="contact-page" data-page="contact" data-state={state}><header><p className="contact-eyebrow">CONTACT</p><h1>Contact GoJet</h1><p>Send a support enquiry. Verification and rate limits are enforced by the server before durable creation.</p></header>
    {state === 'success-persistent' ? <InlineMessage variant="success">Message received. Reference: {reference}</InlineMessage> : null}
    {state === 'validation-error' ? <InlineMessage variant="danger">Check the required contact fields and try again.</InlineMessage> : null}
    {state === 'Turnstile-error' ? <InlineMessage variant="danger">Verification failed. Refresh the verification challenge and try again.</InlineMessage> : null}
    {state === 'rate-limited' ? <InlineMessage variant="warning">Too many contact submissions. Try again later.</InlineMessage> : null}
    <form className="contact-form" onSubmit={submit} aria-busy={state === 'submitting'}>
      <label>Name<input aria-label="Name" value={fields.name} onChange={(event) => update('name', event.currentTarget.value)} autoComplete="name"/></label>
      <label>Email<input aria-label="Email" type="email" value={fields.email} onChange={(event) => update('email', event.currentTarget.value)} autoComplete="email"/></label>
      <label>Subject<input aria-label="Subject" value={fields.subject} onChange={(event) => update('subject', event.currentTarget.value)}/></label>
      <label>Message<textarea aria-label="Message" rows={6} value={fields.message} onChange={(event) => update('message', event.currentTarget.value)}/></label>
      <p className="contact-verification">Verification is checked server-side. Raw verification material is never shown in this page.</p>
      <Button type="submit" disabled={state === 'submitting'}>{state === 'submitting' ? 'Sending…' : 'Send message'}</Button>
    </form>
  </section></WebsiteShell>;
}
