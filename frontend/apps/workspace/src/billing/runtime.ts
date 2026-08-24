import { readWorkspaceRuntime } from '../links/runtime';

export type BillingMoney = { currency: string; amount_minor: number };
export type BillingPlanEntitlement = { capability: string; limit_value: number; unit: string; source_version: number };
export type BillingPlan = {
  id: number; code: string; name: string; status: 'draft' | 'active' | 'archived'; money: BillingMoney;
  billing_period: 'one_time' | 'monthly' | 'yearly'; version: number; entitlements: BillingPlanEntitlement[];
  created_at: string; updated_at: string;
};
export type BillingSubscription = {
  id: string; workspace_id: string; plan_id: number; status: 'pending' | 'active' | 'grace' | 'overdue' | 'canceled' | 'expired';
  starts_at: string; current_term_ends_at?: string; grace_ends_at?: string; cancel_at?: string; version: number; created_at: string; updated_at: string;
};
export type BillingSummary = {
  state: 'active' | 'payment-pending' | 'payment-failed' | 'overdue' | 'canceled' | 'provider-partial';
  plan?: BillingPlan; subscription?: BillingSubscription; scheduled_target?: BillingSubscription; latest_order_status?: string;
};
export type BillingInvoice = {
  id: string; workspace_id: string; order_id: string; money: BillingMoney; status: string;
  issued_at: string; paid_at?: string; refunded_at?: string; created_at: string;
};
export type BillingPayment = {
  id: number; workspace_id: string; order_id: string; provider: string; provider_transaction_id: string;
  money: BillingMoney; status: 'pending' | 'paid' | 'failed' | 'refunded'; created_at: string; updated_at: string;
};

export type BillingRuntime = {
  workspaceId: string;
  actorId: string;
  role: 'owner' | 'admin' | 'member' | 'viewer';
  email: string;
  displayName: string;
};

export class BillingRequestError extends Error {
  constructor(public status: number, public code: string, message: string) { super(message); }
}

export function readBillingRuntime(): BillingRuntime | null {
  const workspace = readWorkspaceRuntime();
  if (!workspace) return null;
  const email = String(import.meta.env.VITE_GOJET_TEST_EMAIL ?? '').trim();
  if (workspace.testAuthority && !email) return null;
  return {
    workspaceId: workspace.workspaceId,
    actorId: workspace.actorId,
    role: workspace.role,
    email,
    displayName: String(import.meta.env.VITE_GOJET_TEST_DISPLAY_NAME ?? '').trim(),
  };
}

function authHeaders(runtime: BillingRuntime): Record<string, string> {
  return {
    'X-GoJet-Test-Actor': runtime.actorId,
    'X-GoJet-Test-Email': runtime.email,
    'X-GoJet-Test-Display-Name': runtime.displayName,
  };
}

async function decode<T>(response: Response): Promise<T> {
  const body = await response.json().catch(() => ({})) as { error?: { code?: string; message?: string } } & T;
  if (!response.ok) throw new BillingRequestError(response.status, body.error?.code ?? 'request_failed', body.error?.message ?? 'Request failed.');
  return body;
}

export async function publicPlans(): Promise<BillingPlan[]> {
  const response = await fetch('/api/public/plans', { credentials: 'same-origin', headers: { Accept: 'application/json' } });
  const body = await decode<{ items: BillingPlan[] }>(response);
  return body.items;
}

export async function billingSummary(runtime: BillingRuntime): Promise<BillingSummary> {
  const response = await fetch(`/api/workspaces/${encodeURIComponent(runtime.workspaceId)}/billing`, {
    credentials: 'same-origin', headers: { ...authHeaders(runtime), Accept: 'application/json' },
  });
  return (await decode<{ summary: BillingSummary }>(response)).summary;
}

export async function workspaceInvoices(runtime: BillingRuntime): Promise<BillingInvoice[]> {
  const response = await fetch(`/api/workspaces/${encodeURIComponent(runtime.workspaceId)}/invoices?limit=50`, {
    credentials: 'same-origin', headers: { ...authHeaders(runtime), Accept: 'application/json' },
  });
  return (await decode<{ items: BillingInvoice[] }>(response)).items;
}

export async function workspacePayments(runtime: BillingRuntime): Promise<BillingPayment[]> {
  const response = await fetch(`/api/workspaces/${encodeURIComponent(runtime.workspaceId)}/payments?limit=50`, {
    credentials: 'same-origin', headers: { ...authHeaders(runtime), Accept: 'application/json' },
  });
  return (await decode<{ items: BillingPayment[] }>(response)).items;
}

export async function createOrder(runtime: BillingRuntime, planId: number, kind: 'new' | 'upgrade' | 'renewal') {
  const response = await fetch(`/api/workspaces/${encodeURIComponent(runtime.workspaceId)}/orders`, {
    method: 'POST', credentials: 'same-origin',
    headers: { ...authHeaders(runtime), 'Content-Type': 'application/json', Accept: 'application/json', 'Idempotency-Key': crypto.randomUUID() },
    body: JSON.stringify({ plan_id: planId, kind }),
  });
  return decode<{ order: { id: string; status: string }; created: boolean }>(response);
}
