import { useEffect, useState } from 'react';
import { useParams } from '@tanstack/react-router';
import { Button } from '@gojet/ui';
import { adminRequest, ErrorNotice, ProtectedLayout, useAdminSession } from '../p17/api';

type Template = { key: string; locale: string; version: number; subject_template: string; text_template: string; html_template: string; variable_allowlist: string[]; enabled: boolean };
type Preview = { Subject: string; Text: string; HTML: string };

export default function MailTemplatesPage() {
  const params = useParams({ strict: false }) as { key?: string };
  return <Templates key={params.key || 'list'} templateKey={params.key} />;
}

function Templates({ templateKey }: { templateKey: string | undefined }) {
  const auth = useAdminSession();
  const [items, setItems] = useState<Template[]>([]);
  const [selected, setSelected] = useState<Template | null>(null);
  const [draft, setDraft] = useState<Template | null>(null);
  const [preview, setPreview] = useState<Preview | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);
  useEffect(() => {
    let active = true;
    if (!auth.session) return () => { active = false; };
    adminRequest<{ items: Template[] }>('/api/admin/mail/templates').then(result => {
      if (!active) return;
      const latest = new Map<string, Template>();
      for (const item of result.items) {
        const key = `${item.key}:${item.locale}`;
        if (!latest.has(key) || latest.get(key)!.version < item.version) latest.set(key, item);
      }
      const rows = [...latest.values()].filter(item => !templateKey || item.key === templateKey);
      setItems(rows);
      if (templateKey) {
        const locale = new URLSearchParams(window.location.search).get('locale');
        const item = rows.find(row => row.locale === locale) || rows[0];
        if (!item) { setError('not_found'); return; }
        setSelected(item); setDraft({ ...item });
      }
    }).catch(err => { if (active) setError(err instanceof Error ? err.message : 'server_error'); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [auth.session, templateKey]);
  function change(field: 'subject_template' | 'text_template' | 'html_template', value: string) {
    setDraft(current => current ? { ...current, [field]: value } : null); setPreview(null); setSaved(false);
  }
  async function submit(save: boolean) {
    if (!draft || !selected || busy) return;
    setBusy(true); setError(''); setSaved(false); setPreview(null);
    try {
      const session = await adminRequest<{ csrf_token: string }>('/api/admin/auth/session');
      const path = `/api/admin/mail/templates/${encodeURIComponent(draft.key)}`;
      const body = JSON.stringify({ locale: draft.locale, expected_version: selected.version, subject_template: draft.subject_template, text_template: draft.text_template, html_template: draft.html_template });
      if (save) {
        const result = await adminRequest<{ template: Template }>(path, { method: 'PATCH', body }, session.csrf_token);
        setSelected(result.template); setDraft(result.template);
        setItems(rows => rows.map(row => row.locale === result.template.locale ? result.template : row)); setSaved(true);
      } else {
        const result = await adminRequest<{ preview: Preview }>(`${path}/preview`, { method: 'POST', body }, session.csrf_token);
        setPreview(result.preview);
      }
    } catch (err) { setError(err instanceof Error ? err.message : 'server_error'); }
    finally { setBusy(false); }
  }
  const failure = auth.error || error;
  const state = failure ? (failure === 'forbidden' ? 'permission-denied' : failure === 'conflict' ? 'conflict' : 'error') : auth.busy || loading ? 'loading' : saved ? 'saved' : preview ? 'preview' : draft ? 'edit' : items.length ? 'ready' : 'empty';
  return <ProtectedLayout state={state === 'permission-denied' ? 'permission-denied' : 'normal'}><section className="p17-admin-page" data-page="admin-mail-templates" data-state={state}>
    <header><h1>Mail templates</h1><a href="/admin/mail">Mail delivery</a></header>
    <ErrorNotice error={failure} />
    {state === 'loading' ? <p role="status">Loading templates…</p> : null}
    {state === 'empty' ? <p>No templates available.</p> : null}
    {state === 'conflict' ? <p role="alert">This template changed. <a href={window.location.href}>Reload the latest version</a> before saving again.</p> : null}
    {saved ? <p role="status">Template saved. Queued messages keep their original version.</p> : null}
    {!templateKey ? <ul className="p17-list">{items.map(item => <li key={`${item.key}:${item.locale}`}><a href={`/admin/platform/mail-templates/${encodeURIComponent(item.key)}?locale=${encodeURIComponent(item.locale)}`}>{item.key} · {item.locale}</a><span>Version {item.version}</span></li>)}</ul> : null}
    {draft && !auth.error ? <form onSubmit={event => { event.preventDefault(); void submit(true); }}>
      <label>Locale<select value={draft.locale} disabled={busy} onChange={event => { const item = items.find(row => row.locale === event.target.value)!; setSelected(item); setDraft({ ...item }); setPreview(null); setError(''); setSaved(false); }}>{items.map(item => <option key={item.locale} value={item.locale}>{item.locale}</option>)}</select></label>
      <p>Available variables: {draft.variable_allowlist.join(', ')}</p>
      <label>Subject<input required maxLength={500} value={draft.subject_template} disabled={busy} onChange={event => change('subject_template', event.target.value)} /></label>
      <label>Plain text<textarea rows={8} maxLength={24000} value={draft.text_template} disabled={busy} onChange={event => change('text_template', event.target.value)} /></label>
      <label>HTML<textarea rows={8} maxLength={24000} value={draft.html_template} disabled={busy} onChange={event => change('html_template', event.target.value)} /></label>
      <div className="p17-actions"><Button type="button" disabled={busy || state === 'conflict'} onClick={() => void submit(false)}>Preview with sample values</Button><Button type="submit" disabled={busy || state === 'conflict'}>Save template</Button></div>
      {preview ? <section aria-label="Template preview"><h2>Sample preview</h2><p>{preview.Subject}</p><pre style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{preview.Text}</pre><h3>Rendered HTML source</h3><pre style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{preview.HTML}</pre></section> : null}
    </form> : null}
  </section></ProtectedLayout>;
}
