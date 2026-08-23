import { GoJetApiError } from './links';
import type { ApiTransport } from './links';

export type TextVisibility = 'private' | 'public';
export type TextShareRecord = {
  id: number;
  workspace_id: string;
  public_slug: string;
  title: string;
  content: string;
  visibility: TextVisibility;
  password_required: boolean;
  expires_at?: string | null;
  one_time: boolean;
  consumed_at?: string | null;
  version: number;
  created_by: string;
  created_at: string;
  updated_at: string;
  deleted_at?: string | null;
};
export type TextListResponse = { items: TextShareRecord[]; total: number; quota: { used: number; limit: number; reached: boolean } };
export type TextCreateInput = {
  title: string;
  content: string;
  visibility: TextVisibility;
  password?: string;
  expires_at?: string;
  one_time?: boolean;
  change_reason: string;
};
export type TextUpdateInput = {
  expected_version: number;
  title?: string;
  content?: string;
  visibility?: TextVisibility;
  password?: string;
  clear_password?: boolean;
  expires_at?: string;
  clear_expires_at?: boolean;
  one_time?: boolean;
  change_reason: string;
};

type ApiErrorEnvelope = { error?: { code?: string; message?: string } };
function normalizeBaseUrl(value: string | undefined): string { return value?.replace(/\/$/, '') ?? ''; }
function basePath(workspaceId: string): string { return `/api/workspaces/${encodeURIComponent(workspaceId)}/text-shares`; }

export class GoJetTextClient {
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
    if (typeof init.body === 'string') headers.set('Content-Type', 'application/json');
    const response = await this.doFetch(`${this.baseUrl}${path}`, { ...init, headers });
    if (!response.ok) {
      let envelope: ApiErrorEnvelope = {};
      try { envelope = await response.json() as ApiErrorEnvelope; } catch { /* non-JSON */ }
      throw new GoJetApiError(response.status, envelope.error?.code ?? 'request_failed', envelope.error?.message ?? `Request failed with HTTP ${response.status}`);
    }
    if (response.status === 204) return undefined as T;
    return await response.json() as T;
  }
  list(workspaceId: string, limit = 100, offset = 0): Promise<TextListResponse> {
    const query = new URLSearchParams({ limit: String(limit), offset: String(offset) });
    return this.request(`${basePath(workspaceId)}?${query}`);
  }
  get(workspaceId: string, shareId: number): Promise<TextShareRecord> {
    return this.request(`${basePath(workspaceId)}/${shareId}`);
  }
  create(workspaceId: string, input: TextCreateInput): Promise<TextShareRecord> {
    return this.request(basePath(workspaceId), { method: 'POST', body: JSON.stringify(input) });
  }
  update(workspaceId: string, shareId: number, input: TextUpdateInput): Promise<TextShareRecord> {
    return this.request(`${basePath(workspaceId)}/${shareId}`, { method: 'PATCH', body: JSON.stringify(input) });
  }
  remove(workspaceId: string, shareId: number, expectedVersion: number, changeReason: string): Promise<void> {
    return this.request(`${basePath(workspaceId)}/${shareId}`, { method: 'DELETE', body: JSON.stringify({ expected_version: expectedVersion, change_reason: changeReason }) });
  }
}
