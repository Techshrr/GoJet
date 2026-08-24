export type CommerceMoney = { currency: string; amount_minor: number };
export type CommerceEntitlement = { capability: string; limit_value: number; unit: string; source_version: number };
export type CommercePlan = {
  id: number; code: string; name: string; status: 'draft' | 'active' | 'archived'; money: CommerceMoney;
  billing_period: 'one_time' | 'monthly' | 'yearly'; version: number; entitlements: CommerceEntitlement[];
  created_at: string; updated_at: string;
};
export type CommercePayment = {
  id: number; workspace_id: string; order_id: string; provider: string; provider_transaction_id: string;
  money: CommerceMoney; status: 'pending' | 'paid' | 'failed' | 'refunded'; created_at: string; updated_at: string;
};
export type CommerceFX = {
  base_currency: string; quote_currency: string; rate: string; source: string; as_of: string;
  status: 'current' | 'stale' | 'provider-error' | 'override'; override_reason?: string; updated_at: string;
};
export type CommerceRuntime = { actorId: string; email: string; displayName: string };

export class CommerceRequestError extends Error {
  constructor(public status: number, public code: string, message: string) { super(message); }
}

export function readCommerceRuntime(): CommerceRuntime | null {
  if (import.meta.env.VITE_GOJET_TEST_AUTH_ENABLED !== '1') return null;
  const actorId = String(import.meta.env.VITE_GOJET_TEST_ACTOR_ID ?? '').trim();
  const email = String(import.meta.env.VITE_GOJET_TEST_EMAIL ?? '').trim();
  if (!actorId || !email) return null;
  return { actorId, email, displayName: String(import.meta.env.VITE_GOJET_TEST_DISPLAY_NAME ?? '').trim() };
}

function headers(runtime: CommerceRuntime, json = false): Record<string, string> {
  return {
    'X-GoJet-Test-Actor': runtime.actorId,
    'X-GoJet-Test-Email': runtime.email,
    'X-GoJet-Test-Display-Name': runtime.displayName,
    Accept: 'application/json',
    ...(json ? { 'Content-Type': 'application/json' } : {}),
  };
}

async function decode<T>(response: Response): Promise<T> {
  const body = await response.json().catch(() => ({})) as { error?: { code?: string; message?: string } } & T;
  if (!response.ok) throw new CommerceRequestError(response.status, body.error?.code ?? 'request_failed', body.error?.message ?? 'Request failed.');
  return body;
}

export async function adminGet<T>(runtime: CommerceRuntime, path: string): Promise<T> {
  return decode<T>(await fetch(path, { credentials: 'same-origin', headers: headers(runtime) }));
}

export async function adminWrite<T>(runtime: CommerceRuntime, path: string, method: 'POST' | 'PUT', body: unknown): Promise<T> {
  return decode<T>(await fetch(path, {
    method, credentials: 'same-origin', headers: { ...headers(runtime, true), 'X-Request-Correlation-ID': crypto.randomUUID() }, body: JSON.stringify(body),
  }));
}

export function parseEntitlements(value: string): Array<{ capability: string; limit_value: number; unit: string }> {
  const lines = value.split(/\r?\n/).map((line) => line.trim()).filter(Boolean);
  if (lines.length === 0) throw new Error('At least one entitlement is required.');
  return lines.map((line) => {
    const [capability = '', rawLimit = '', unit = 'count'] = line.split(':').map((part) => part.trim());
    const limit = Number(rawLimit);
    if (!/^[a-z0-9][a-z0-9_.:-]{0,95}$/.test(capability) || !Number.isSafeInteger(limit) || limit <= 0 || !/^[a-z0-9][a-z0-9_./-]{0,31}$/.test(unit)) {
      throw new Error(`Invalid entitlement line: ${line}`);
    }
    return { capability, limit_value: limit, unit };
  });
}
