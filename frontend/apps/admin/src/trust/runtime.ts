export type DestinationRiskState = 'pending' | 'allow' | 'review' | 'block';
export type DestinationRiskRecord = {
  id: number;
  workspace_id: string;
  link_id: number;
  risk_fingerprint: string;
  policy_version: string;
  request_kind: string;
  scan_status: string;
  decision_state: DestinationRiskState;
  reason_category: string;
  attempts: number;
  max_attempts: number;
  target_count: number;
  provider_count: number;
  has_active_override: boolean;
  valid_until?: string;
  created_at: string;
  updated_at: string;
};

export type DomainRiskState = 'pending' | 'allow' | 'review' | 'block' | 'stale';
export type DomainRiskRecord = {
  evaluation_id: number;
  workspace_id: string;
  domain_id: number;
  hostname_ascii: string;
  policy_version: string;
  request_kind: string;
  state: DomainRiskState;
  reason_category: string;
  entitlement_status: string;
  ownership_status: string;
  ingress_dns_status: string;
  https_status: string;
  routing_status: string;
  provider_count: number;
  valid_until?: string;
  checked_at?: string;
  next_due_at?: string;
  created_at: string;
  updated_at: string;
};

export type AbuseStatus = 'open' | 'investigating' | 'resolved' | 'dismissed';
export type AbuseReportRecord = {
  id: number;
  public_id: string;
  workspace_id: string;
  resource_type: 'short-link-risk' | 'custom-domain-risk';
  resource_id: string;
  hostname: string;
  safe_code?: string;
  destination_fingerprint?: string;
  category: string;
  details: string;
  status: AbuseStatus;
  version: number;
  active_hold?: {
    id: number;
    state: string;
    reason_category: string;
    created_at: string;
  };
  created_at: string;
  updated_at: string;
};

export class TrustRequestError extends Error {
  constructor(public status: number, public code: string, message: string) {
    super(message);
  }
}

type ErrorEnvelope = { error?: string | { code?: string; message?: string } };

async function decode<T>(response: Response): Promise<T> {
  const body = await response.json().catch(() => ({})) as ErrorEnvelope & T;
  if (!response.ok) {
    const nested = typeof body.error === 'object' ? body.error : undefined;
    const code = typeof body.error === 'string' ? body.error : nested?.code ?? 'request_failed';
    throw new TrustRequestError(response.status, code, nested?.message ?? safeErrorMessage(response.status, code));
  }
  return body;
}

function safeErrorMessage(status: number, code: string): string {
  if (status === 401) return 'Administrator authentication is required.';
  if (status === 403) return 'Your administrator permission does not cover this action.';
  if (status === 404) return 'The requested trust record was not found.';
  if (status === 409) return code === 'state_conflict' ? 'The trust record changed or is no longer eligible for this action.' : 'The request conflicts with current authority.';
  if (status === 429) return 'This operation is rate limited. Wait for current authority to settle before retrying.';
  if (status === 503) return 'A required trust dependency is unavailable. No allow authority is inferred.';
  return 'The trust operation could not be completed.';
}

export async function trustGet<T>(path: string): Promise<T> {
  return decode<T>(await fetch(path, {
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
    cache: 'no-store',
  }));
}

export async function trustWrite<T>(path: string, csrfToken: string, body: unknown, options?: { idempotency?: boolean }): Promise<T> {
  const headers: Record<string, string> = {
    Accept: 'application/json',
    'Content-Type': 'application/json',
    'X-CSRF-Token': csrfToken,
    'X-Request-ID': crypto.randomUUID(),
  };
  if (options?.idempotency !== false) headers['Idempotency-Key'] = crypto.randomUUID();
  return decode<T>(await fetch(path, {
    method: 'POST',
    credentials: 'same-origin',
    cache: 'no-store',
    headers,
    body: JSON.stringify(body),
  }));
}

export function trustShellState(error: unknown): 'normal' | 'admin-auth-required' | 'permission-denied' | 'partial-service-degradation' {
  if (error instanceof TrustRequestError && error.status === 401) return 'admin-auth-required';
  if (error instanceof TrustRequestError && error.status === 403) return 'permission-denied';
  if (error) return 'partial-service-degradation';
  return 'normal';
}

export function trustErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : 'Trust & Safety data is unavailable.';
}

export function compactFingerprint(value: string): string {
  const clean = value.trim();
  if (clean.length <= 18) return clean;
  return `${clean.slice(0, 10)}…${clean.slice(-8)}`;
}

export function formatTimestamp(value?: string): string {
  if (!value) return 'Not set';
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? 'Unavailable' : parsed.toLocaleString();
}
