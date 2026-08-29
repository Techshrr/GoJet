import type { ReactNode } from 'react';
import { useCallback, useEffect, useState } from 'react';
import { InlineMessage } from '@gojet/ui';
import { AdminShell } from '../shell/AdminShell';

export type JsonObject = Record<string, any>;
export type AdminSession = {
  administrator: { id: string; email: string; display_name: string; status: string; mfa_enabled: boolean };
  session: { id: string; status: string; expires_at: string };
  permissions: string[];
  csrf_token: string;
};

let mutationSequence = 0;

export async function adminRequest<T>(path: string, init: RequestInit = {}, csrf = ''): Promise<T> {
  const method = (init.method || 'GET').toUpperCase();
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    'X-Correlation-ID': `p17-browser-${Date.now()}`,
    ...(init.headers as Record<string, string> | undefined),
  };
  if (!['GET', 'HEAD', 'OPTIONS'].includes(method)) {
    mutationSequence += 1;
    headers['Idempotency-Key'] = `p17-browser-${Date.now()}-${mutationSequence}`;
    if (csrf) headers['X-CSRF-Token'] = csrf;
  }
  const response = await fetch(path, { ...init, method, headers, credentials: 'same-origin' });
  const body = await response.json().catch(() => undefined) as ({ error?: { code?: string } } & T) | undefined;
  if (!response.ok) throw new Error(body?.error?.code || `http_${response.status}`);
  return body as T;
}

export function ErrorNotice({ error }: { error: string }) {
  if (!error) return null;
  const denied = error === 'forbidden' || error === 'unauthorized';
  return <InlineMessage variant="danger">{denied ? 'Your administrator permission does not authorize this operation.' : `Request failed: ${error}`}</InlineMessage>;
}

export function JsonPreview({ value }: { value: unknown }) {
  return <pre className="p17-json" aria-label="Record detail">{JSON.stringify(value, null, 2)}</pre>;
}

export function ProtectedLayout({ children, state = 'normal' }: { children: ReactNode; state?: 'normal' | 'permission-denied' | 'partial-service-degradation' }) {
  return <AdminShell state={state}>{children}</AdminShell>;
}

export function useAdminSession() {
  const [session, setSession] = useState<AdminSession | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(true);
  const reload = useCallback(async () => {
    setBusy(true); setError('');
    try { setSession(await adminRequest<AdminSession>('/api/admin/auth/session')); }
    catch (err) { setSession(null); setError(err instanceof Error ? err.message : 'internal_error'); }
    finally { setBusy(false); }
  }, []);
  useEffect(() => { void reload(); }, [reload]);
  return { session, error, busy, reload };
}
