import { useEffect, useState, type FormEvent } from 'react';
import { Button } from '@gojet/ui';
import { adminRequest, ErrorNotice, type AdminSession } from './api';

type Setting = { value: Record<string, string>; version: number };
export function GeneralSettings({ session }: { session: AdminSession | null }) {
  const [setting, setSetting] = useState<Setting | null>(null);
  const [values, setValues] = useState({ site_name: '', public_base_url: '', support_url: '' });
  const [reason, setReason] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(true);
  const [saved, setSaved] = useState(false);
  useEffect(() => {
    if (!session) return;
    let active = true;
    void adminRequest<{ setting: Setting }>('/api/admin/settings/general').then(({ setting }) => {
      if (active) { setSetting(setting); setValues({ site_name: setting.value.site_name || '', public_base_url: setting.value.public_base_url || '', support_url: setting.value.support_url || '' }); }
    }).catch((err) => { if (active) setError(err instanceof Error ? err.message : 'internal_error'); })
      .finally(() => { if (active) setBusy(false); });
    return () => { active = false; };
  }, [session]);
  async function save(event: FormEvent) {
    event.preventDefault();
    if (!setting || !session || busy) return;
    setBusy(true); setError(''); setSaved(false);
    try {
      const current = await adminRequest<AdminSession>('/api/admin/auth/session');
      const result = await adminRequest<{ setting: Setting }>('/api/admin/settings/general', {
        method: 'PUT', body: JSON.stringify({ value: values, expected_version: setting.version, reason }),
      }, current.csrf_token);
      setSetting(result.setting); setSaved(true); setReason('');
    } catch (err) { setError(err instanceof Error ? err.message : 'internal_error'); }
    finally { setBusy(false); }
  }
  return <section aria-label="General site settings"><ErrorNotice error={error} />
    {busy && !setting && <p role="status">Loading settings…</p>}
    {setting?.version === 0 && <p>General settings have not been configured yet.</p>}
    {setting && <form className="admin-support-form" onSubmit={(event) => void save(event)}>
      <label>Site name<input required maxLength={120} value={values.site_name} onChange={(event) => setValues({ ...values, site_name: event.target.value })} /></label>
      <label>Public HTTPS address<input required type="url" value={values.public_base_url} onChange={(event) => setValues({ ...values, public_base_url: event.target.value })} /></label>
      <label>Support HTTPS address<input required type="url" value={values.support_url} onChange={(event) => setValues({ ...values, support_url: event.target.value })} /></label>
      <label>Reason for general settings change<input required value={reason} onChange={(event) => setReason(event.target.value)} /></label>
      <Button type="submit" disabled={busy || !reason.trim()}>{busy ? 'Saving…' : 'Save general settings'}</Button>
    </form>}
    {saved && <p role="status">General settings saved.</p>}
  </section>;
}
