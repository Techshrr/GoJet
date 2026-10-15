import { useEffect, useState } from 'react';
import { Button } from '@gojet/ui';
import { adminRequest, ErrorNotice, type AdminSession } from './api';

type Setting = { value: { enabled: string }; version: number };
export function GoogleOneTapSettings({ session }: { session: AdminSession | null }) {
  const [enabled, setEnabled] = useState(false);
  const [version, setVersion] = useState(0);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');
  const [reason, setReason] = useState('');
  useEffect(() => {
    if (!session) return;
    let active = true;
    setBusy(true);
    void adminRequest<{ setting: Setting }>('/api/admin/settings/google_one_tap').then(({ setting }) => {
      if (active) { setEnabled(setting.value.enabled === 'true'); setVersion(setting.version); setError(''); }
    }).catch((err: unknown) => {
      if (active) { const code = err instanceof Error ? err.message : 'internal_error'; setError(code === 'not_found' ? '' : code); }
    }).finally(() => { if (active) setBusy(false); });
    return () => { active = false; };
  }, [session]);
  async function save() {
    setBusy(true); setError(''); setMessage('');
    try {
      const current = await adminRequest<AdminSession>('/api/admin/auth/session');
      const { setting } = await adminRequest<{ setting: Setting }>('/api/admin/settings/google_one_tap', {
        method: 'PUT', body: JSON.stringify({ value: { enabled: String(enabled) }, expected_version: version, reason }),
      }, current.csrf_token);
      setVersion(setting.version); setEnabled(setting.value.enabled === 'true'); setMessage('Google One Tap preference saved.');
    } catch (err) { setError(err instanceof Error ? err.message : 'internal_error'); }
    finally { setBusy(false); }
  }
  return <section aria-label="Google One Tap settings">
    <h2>Google One Tap</h2>
    <p>Show a Google sign-in prompt to signed-out visitors. Google login must also be configured and enabled.</p>
    <ErrorNotice error={error} />
    <label><input type="checkbox" checked={enabled} disabled={busy || !session} onChange={(e) => setEnabled(e.target.checked)} /> Enable Google One Tap</label>
    <label>Reason for change<input value={reason} onChange={(e) => setReason(e.target.value)} /></label>
    <Button type="button" disabled={busy || !session || !reason.trim()} onClick={() => void save()}>Save One Tap preference</Button>
    {message && <p role="status">{message}</p>}
  </section>;
}
