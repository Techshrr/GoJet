import { GoJetApiError } from './links';
import type { ApiTransport } from './links';

export type BioStatus = 'draft' | 'published' | 'paused';
export type BioChildRiskStatus = 'review' | 'allowed' | 'blocked';
export type BioChildRecord = {
  id: number;
  bio_page_id: number;
  position: number;
  label: string;
  destination_url: string;
  destination_fingerprint: string;
  risk_status: BioChildRiskStatus;
  risk_checked_at?: string | null;
};
export type BioPageRecord = {
  id: number;
  workspace_id: string;
  slug: string;
  title: string;
  bio: string;
  status: BioStatus;
  version: number;
  published_at?: string | null;
  created_by: string;
  created_at: string;
  updated_at: string;
  deleted_at?: string | null;
  links: BioChildRecord[];
};
export type BioListResponse = { items: BioPageRecord[]; total: number; quota: { used: number; limit: number; reached: boolean } };
export type BioChildInput = { id?: number; position: number; label: string; destination_url: string };
export type BioCreateInput = { title: string; bio: string; links: BioChildInput[]; change_reason: string };
export type BioUpdateInput = { expected_version: number; title?: string; bio?: string; links?: BioChildInput[]; change_reason: string };

type ApiErrorEnvelope = { error?: { code?: string; message?: string } };
function normalizeBaseUrl(value: string | undefined): string { return value?.replace(/\/$/, '') ?? ''; }
function basePath(workspaceId: string): string { return `/api/workspaces/${encodeURIComponent(workspaceId)}/bio-pages`; }

export class GoJetBioClient {
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
  list(workspaceId: string, limit = 100, offset = 0): Promise<BioListResponse> {
    const query = new URLSearchParams({ limit: String(limit), offset: String(offset) });
    return this.request(`${basePath(workspaceId)}?${query}`);
  }
  get(workspaceId: string, pageId: number): Promise<BioPageRecord> {
    return this.request(`${basePath(workspaceId)}/${pageId}`);
  }
  create(workspaceId: string, input: BioCreateInput): Promise<BioPageRecord> {
    return this.request(basePath(workspaceId), { method: 'POST', body: JSON.stringify(input) });
  }
  update(workspaceId: string, pageId: number, input: BioUpdateInput): Promise<BioPageRecord> {
    return this.request(`${basePath(workspaceId)}/${pageId}`, { method: 'PATCH', body: JSON.stringify(input) });
  }
  publish(workspaceId: string, pageId: number, expectedVersion: number, changeReason: string): Promise<BioPageRecord> {
    return this.request(`${basePath(workspaceId)}/${pageId}/publish`, { method: 'POST', body: JSON.stringify({ expected_version: expectedVersion, change_reason: changeReason }) });
  }
  pause(workspaceId: string, pageId: number, expectedVersion: number, changeReason: string): Promise<BioPageRecord> {
    return this.request(`${basePath(workspaceId)}/${pageId}/pause`, { method: 'POST', body: JSON.stringify({ expected_version: expectedVersion, change_reason: changeReason }) });
  }
  remove(workspaceId: string, pageId: number, expectedVersion: number, changeReason: string): Promise<void> {
    return this.request(`${basePath(workspaceId)}/${pageId}`, { method: 'DELETE', body: JSON.stringify({ expected_version: expectedVersion, change_reason: changeReason }) });
  }
}
