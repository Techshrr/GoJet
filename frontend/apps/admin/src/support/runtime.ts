export type AdminSupportRuntime = { actorId: string; email: string; displayName: string };

export class AdminSupportRequestError extends Error {
  constructor(public status: number, public code: string, message: string) { super(message); }
}

export type AdminTicketStatus = 'open' | 'awaiting_user' | 'awaiting_support' | 'closed';
export type AdminTicket = {
  id: string;
  workspace_id?: string;
  requester_user_id?: string;
  public_contact_id?: string;
  category: string;
  subject: string;
  status: AdminTicketStatus;
  created_at: string;
  updated_at: string;
  closed_at?: string;
  version: number;
  correlation_id: string;
};
export type AdminTicketMessage = {
  id: string;
  ticket_id: string;
  actor_type: 'requester' | 'support';
  actor_id: string;
  kind: 'requester_reply' | 'support_reply' | 'internal_note';
  body: string;
  created_at: string;
  correlation_id: string;
};
export type AdminMailStatus = 'queued' | 'sending' | 'sent' | 'retrying' | 'failed';
export type AdminMailQueueItem = {
  id: string;
  template_key: string;
  template_version: number;
  recipient_kind: string;
  resource_type: string;
  resource_id: string;
  status: AdminMailStatus;
  attempt_count: number;
  next_attempt_at?: string;
  last_error_code?: string;
  created_at: string;
  updated_at: string;
};
export type AdminMailTemplate = {
  key: string;
  locale: string;
  version: number;
  variable_allowlist: string[];
  enabled: boolean;
  updated_at: string;
};
export type AdminMailSettings = { enabled: boolean; version: number; updated_at: string };

export function readAdminSupportRuntime(): AdminSupportRuntime | null {
  if (import.meta.env.VITE_GOJET_TEST_AUTH_ENABLED !== '1') return null;
  const actorId = String(import.meta.env.VITE_GOJET_TEST_ACTOR_ID ?? '').trim();
  const email = String(import.meta.env.VITE_GOJET_TEST_EMAIL ?? '').trim();
  if (!actorId || !email) return null;
  return { actorId, email, displayName: String(import.meta.env.VITE_GOJET_TEST_DISPLAY_NAME ?? '').trim() };
}

function headers(runtime: AdminSupportRuntime, json = false, idempotent = false): Record<string, string> {
  return {
    'X-GoJet-Test-Actor': runtime.actorId,
    'X-GoJet-Test-Email': runtime.email,
    'X-GoJet-Test-Display-Name': runtime.displayName,
    Accept: 'application/json',
    ...(json ? { 'Content-Type': 'application/json' } : {}),
    ...(idempotent ? { 'Idempotency-Key': crypto.randomUUID() } : {}),
  };
}

async function decode<T>(response: Response): Promise<T> {
  const body = await response.json().catch(() => ({})) as { error?: { code?: string; message?: string } } & T;
  if (!response.ok) throw new AdminSupportRequestError(response.status, body.error?.code ?? 'request_failed', body.error?.message ?? 'Request failed.');
  return body;
}

export async function adminSupportGet<T>(runtime: AdminSupportRuntime, path: string): Promise<T> {
  return decode<T>(await fetch(path, { credentials: 'same-origin', headers: headers(runtime) }));
}

export async function adminSupportWrite<T>(runtime: AdminSupportRuntime, path: string, method: 'POST' | 'PATCH', body: unknown, idempotent = false): Promise<T> {
  return decode<T>(await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: { ...headers(runtime, true, idempotent), 'X-Request-Correlation-ID': crypto.randomUUID() },
    body: JSON.stringify(body),
  }));
}
