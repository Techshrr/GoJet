import { GoJetApiError } from './links';
import type { ApiTransport } from './links';

export type FileScanState = 'quarantined' | 'scanning' | 'safe' | 'blocked' | 'scan_error';
export type FileDependencyState = 'healthy' | 'unavailable' | 'permission_error' | 'stale' | 'indeterminate';

export type FileRecord = {
  id: number;
  workspace_id: string;
  public_slug: string;
  original_name: string;
  size_bytes: number;
  content_sha256: string;
  declared_mime: string;
  detected_mime: string;
  scan_state: FileScanState;
  scan_generation: number;
  published: boolean;
  published_at?: string | null;
  password_required: boolean;
  expires_at?: string | null;
  retention_until?: string | null;
  download_limit?: number | null;
  download_count: number;
  created_by: string;
  created_at: string;
  updated_at: string;
  deleted_at?: string | null;
};

export type FileListResponse = { items: FileRecord[]; total: number };
export type FileUploadInput = { file: File; change_reason: string };
export type FilePolicyInput = {
  password?: string;
  clear_password?: boolean;
  expires_at?: string;
  clear_expires_at?: boolean;
  retention_until?: string;
  clear_retention_until?: boolean;
  download_limit?: number;
  clear_download_limit?: boolean;
  change_reason: string;
};
export type FileArtifact = { blob: Blob; filename: string };
export type FileDependencyHealthReport = {
  ready: boolean;
  status: FileDependencyState;
  storage: { state: FileDependencyState; writable: boolean };
  clamav: {
    state: FileDependencyState;
    engine_version?: string;
    signature_version?: string;
    signature_date?: string;
    checked_at: string;
  };
};

type ApiErrorEnvelope = { error?: { code?: string; message?: string } };

function normalizeBaseUrl(value: string | undefined): string { return value?.replace(/\/$/, '') ?? ''; }
function filePath(workspaceId: string, suffix = ''): string {
  return `/api/workspaces/${encodeURIComponent(workspaceId)}/files${suffix}`;
}
function safeFilename(value: string | null, fallback: string): string {
  if (!value) return fallback;
  const match = value.match(/filename="?([^";]+)"?/i);
  return match?.[1]?.trim() || fallback;
}

export class GoJetFilesClient {
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
    if (typeof init.body === 'string' && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
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

  list(workspaceId: string, limit = 100, offset = 0): Promise<FileListResponse> {
    const query = new URLSearchParams({ limit: String(limit), offset: String(offset) });
    return this.request(`${filePath(workspaceId)}?${query}`);
  }

  get(workspaceId: string, fileId: number): Promise<FileRecord> {
    return this.request(filePath(workspaceId, `/${fileId}`));
  }

  upload(workspaceId: string, input: FileUploadInput): Promise<FileRecord> {
    const body = new FormData();
    body.append('change_reason', input.change_reason);
    body.append('file', input.file, input.file.name);
    return this.request(filePath(workspaceId), { method: 'POST', body });
  }

  updatePolicy(workspaceId: string, fileId: number, input: FilePolicyInput): Promise<FileRecord> {
    return this.request(filePath(workspaceId, `/${fileId}`), { method: 'PATCH', body: JSON.stringify(input) });
  }

  publish(workspaceId: string, fileId: number, changeReason: string): Promise<FileRecord> {
    return this.request(filePath(workspaceId, `/${fileId}/publish`), {
      method: 'POST', body: JSON.stringify({ change_reason: changeReason }),
    });
  }

  rescan(workspaceId: string, fileId: number, changeReason: string): Promise<FileRecord> {
    return this.request(filePath(workspaceId, `/${fileId}/rescan`), {
      method: 'POST', body: JSON.stringify({ change_reason: changeReason }),
    });
  }

  remove(workspaceId: string, fileId: number, changeReason: string): Promise<void> {
    return this.request(filePath(workspaceId, `/${fileId}`), {
      method: 'DELETE', body: JSON.stringify({ change_reason: changeReason }),
    });
  }

  async download(workspaceId: string, fileId: number): Promise<FileArtifact> {
    const response = await this.response(filePath(workspaceId, `/${fileId}/download`), {}, 'application/octet-stream');
    return {
      blob: await response.blob(),
      filename: safeFilename(response.headers.get('Content-Disposition'), `gojet-file-${fileId}`),
    };
  }

  health(): Promise<FileDependencyHealthReport> {
    return this.request('/api/admin/platform/storage');
  }
}
