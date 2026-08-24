import { GoJetApiError } from './links';
import type { ApiTransport } from './links';

export type SupportTicketStatus = 'open' | 'awaiting_user' | 'awaiting_support' | 'closed';
export type SupportMessageKind = 'requester_reply' | 'support_reply' | 'internal_note';
export type SupportActorType = 'requester' | 'support';

export type SupportTicket = {
  id: string;
  workspace_id?: string;
  requester_user_id?: string;
  public_contact_id?: string;
  category: string;
  subject: string;
  status: SupportTicketStatus;
  created_at: string;
  updated_at: string;
  closed_at?: string;
  version: number;
  correlation_id: string;
};

export type SupportMessage = {
  id: string;
  ticket_id: string;
  actor_type: SupportActorType;
  actor_id: string;
  kind: Exclude<SupportMessageKind, 'internal_note'>;
  body: string;
  created_at: string;
  correlation_id: string;
};

export type SupportTicketListResponse = { items: SupportTicket[] };
export type SupportTicketDetailResponse = { ticket: SupportTicket; messages: SupportMessage[] };
export type SupportTicketCreateInput = {
  workspace_id: string;
  category: string;
  subject: string;
  message: string;
  turnstile_token: string;
};
export type SupportTicketCreateResponse = { ticket: SupportTicket; created: boolean };
export type SupportTicketReplyResponse = { ticket: SupportTicket; message_id: string; created: boolean };
export type SupportTicketCloseResponse = { ticket: SupportTicket; changed: boolean };

type ApiErrorEnvelope = { error?: { code?: string; message?: string } };

function normalizeBaseUrl(value: string | undefined): string { return value?.replace(/\/$/, '') ?? ''; }
function normalizeItems<T>(value: T[] | null | undefined): T[] {
  if (value == null) return [];
  if (!Array.isArray(value)) throw new TypeError('Invalid support collection response.');
  return value;
}

export class GoJetSupportClient {
  private readonly baseUrl: string;
  private readonly headers: (() => HeadersInit) | undefined;
  private readonly doFetch: typeof globalThis.fetch;

  constructor(transport: ApiTransport = {}) {
    this.baseUrl = normalizeBaseUrl(transport.baseUrl);
    this.headers = transport.headers;
    this.doFetch = transport.fetch ?? globalThis.fetch.bind(globalThis);
  }

  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const headers = new Headers(this.headers?.());
    headers.set('Accept', 'application/json');
    if (init.body !== undefined && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
    const response = await this.doFetch(`${this.baseUrl}${path}`, { credentials: 'same-origin', ...init, headers });
    if (!response.ok) {
      let envelope: ApiErrorEnvelope = {};
      try { envelope = await response.json() as ApiErrorEnvelope; } catch { /* non-JSON error */ }
      throw new GoJetApiError(response.status, envelope.error?.code ?? 'request_failed', envelope.error?.message ?? `Request failed with HTTP ${response.status}`);
    }
    if (response.status === 204) return undefined as T;
    return await response.json() as T;
  }

  async list(workspaceId: string): Promise<SupportTicketListResponse> {
    const value = await this.request<{ items: SupportTicket[] | null }>(`/api/support/tickets?workspace_id=${encodeURIComponent(workspaceId)}`);
    return { items: normalizeItems(value.items) };
  }

  async get(ticketId: string): Promise<SupportTicketDetailResponse> {
    const value = await this.request<{ ticket: SupportTicket; messages: SupportMessage[] | null }>(`/api/support/tickets/${encodeURIComponent(ticketId)}`);
    return { ticket: value.ticket, messages: normalizeItems(value.messages) };
  }

  create(input: SupportTicketCreateInput): Promise<SupportTicketCreateResponse> {
    return this.request('/api/support/tickets', {
      method: 'POST',
      headers: { 'Idempotency-Key': crypto.randomUUID() },
      body: JSON.stringify(input),
    });
  }

  reply(ticketId: string, message: string): Promise<SupportTicketReplyResponse> {
    return this.request(`/api/support/tickets/${encodeURIComponent(ticketId)}/replies`, {
      method: 'POST',
      headers: { 'Idempotency-Key': crypto.randomUUID() },
      body: JSON.stringify({ message }),
    });
  }

  close(ticketId: string): Promise<SupportTicketCloseResponse> {
    return this.request(`/api/support/tickets/${encodeURIComponent(ticketId)}/close`, { method: 'POST', body: JSON.stringify({}) });
  }
}
