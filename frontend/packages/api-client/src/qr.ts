import { GoJetApiError } from './links';
import type { ApiTransport } from './links';

export type QRState = 'ready' | 'source-link-review' | 'source-link-block';
export type QRRiskState = 'allow' | 'review' | 'block' | 'missing' | 'malformed' | 'stale';

export type QRSource = {
  link_id: number;
  hostname?: string;
  code?: string;
  public_url?: string;
  risk_state: QRRiskState;
  reason: string;
};

export type QRRecord = {
  id: number;
  workspace_id: string;
  source_link_id: number;
  label: string;
  state: QRState;
  source: QRSource;
  created_at: string;
  updated_at: string;
};

export type QRListResponse = {
  items: QRRecord[];
  total: number;
  quota: {
    used: number;
    limit: number;
    reached: boolean;
  };
};

export type QRCreateInput = {
  source_link_id: number;
  label: string;
  change_reason: string;
};

export type QRArtifact = {
  blob: Blob;
  format: 'png' | 'svg';
  sha256: string | null;
  filename: string;
};

type ApiErrorEnvelope = { error?: { code?: string; message?: string } };

function normalizeBaseUrl(value: string | undefined): string {
  return value?.replace(/\/$/, '') ?? '';
}

function safeFilename(value: string | null, fallback: string): string {
  if (!value) return fallback;
  const match = value.match(/filename="?([^";]+)"?/i);
  return match?.[1]?.trim() || fallback;
}

export class GoJetQRClient {
  private readonly baseUrl: string;
  private readonly headers: (() => HeadersInit) | undefined;
  private readonly doFetch: typeof globalThis.fetch;

  constructor(transport: ApiTransport = {}) {
    this.baseUrl = normalizeBaseUrl(transport.baseUrl);
    this.headers = transport.headers;
    this.doFetch = transport.fetch ?? globalThis.fetch.bind(globalThis);
  }

  private async response(path: string, init: RequestInit = {}, accept = 'application/json'): Promise<Response> {
    const headers = new Headers(this.headers?.());
    if (init.body !== undefined && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
    headers.set('Accept', accept);
    const response = await this.doFetch(`${this.baseUrl}${path}`, { ...init, headers });
    if (!response.ok) {
      let envelope: ApiErrorEnvelope = {};
      try { envelope = await response.json() as ApiErrorEnvelope; } catch { /* non-JSON error */ }
      throw new GoJetApiError(
        response.status,
        envelope.error?.code ?? 'request_failed',
        envelope.error?.message ?? `Request failed with HTTP ${response.status}`,
      );
    }
    return response;
  }

  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const response = await this.response(path, init);
    if (response.status === 204) return undefined as T;
    return await response.json() as T;
  }

  list(workspaceId: string, limit = 100, offset = 0): Promise<QRListResponse> {
    const query = new URLSearchParams({ limit: String(limit), offset: String(offset) });
    return this.request(`/api/workspaces/${encodeURIComponent(workspaceId)}/qr-codes?${query}`);
  }

  get(workspaceId: string, qrId: number): Promise<QRRecord> {
    return this.request(`/api/workspaces/${encodeURIComponent(workspaceId)}/qr-codes/${qrId}`);
  }

  create(workspaceId: string, input: QRCreateInput): Promise<QRRecord> {
    return this.request(`/api/workspaces/${encodeURIComponent(workspaceId)}/qr-codes`, {
      method: 'POST',
      body: JSON.stringify(input),
    });
  }

  remove(workspaceId: string, qrId: number, changeReason: string): Promise<void> {
    return this.request(`/api/workspaces/${encodeURIComponent(workspaceId)}/qr-codes/${qrId}`, {
      method: 'DELETE',
      body: JSON.stringify({ change_reason: changeReason }),
    });
  }

  async artifact(workspaceId: string, qrId: number, format: 'png' | 'svg', download = false): Promise<QRArtifact> {
    const action = download ? 'download' : 'preview';
    const response = await this.response(
      `/api/workspaces/${encodeURIComponent(workspaceId)}/qr-codes/${qrId}/${action}?format=${format}`,
      {},
      format === 'png' ? 'image/png' : 'image/svg+xml',
    );
    return {
      blob: await response.blob(),
      format,
      sha256: response.headers.get('X-GoJet-Artifact-SHA256'),
      filename: safeFilename(response.headers.get('Content-Disposition'), `gojet-qr-${qrId}.${format}`),
    };
  }
}
