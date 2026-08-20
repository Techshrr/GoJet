export type LinkRoutingRule = {
  id: string;
  match_type: string;
  match_value: string;
  destination: string;
  enabled: boolean;
};

export type LinkABVariant = {
  id: string;
  destination: string;
  weight: number;
  enabled: boolean;
};

export type LinkUTM = {
  source?: string;
  medium?: string;
  campaign?: string;
  term?: string;
  content?: string;
};

export type LinkAccess = {
  password_hash?: string;
};

export type LinkRecord = {
  id: number;
  workspace_id: string;
  hostname: string;
  domain_kind: 'official' | 'custom';
  code: string;
  title: string;
  primary_destination: string;
  redirect_status: 301 | 302 | 307 | 308;
  status: 'active' | 'paused' | 'deleted';
  version: number;
  risk_fingerprint: string;
  routing: LinkRoutingRule[];
  ab: LinkABVariant[];
  utm: LinkUTM;
  access: LinkAccess;
  expires_at?: string | null;
  click_limit?: number | null;
  click_count: number;
  one_time: boolean;
  created_at: string;
  updated_at: string;
  deleted_at?: string | null;
};

export type LinkListFilters = {
  q?: string;
  hostname?: string;
  status?: 'active' | 'paused' | 'deleted';
  updated_from?: string;
  updated_to?: string;
  limit?: number;
  offset?: number;
};

export type LinkListResponse = {
  items: LinkRecord[];
  total: number;
  filters: {
    implemented: string[];
    deferred_to_owners: Record<string, string>;
  };
};

export type LinkCreateInput = {
  hostname: string;
  domain_kind: 'official' | 'custom';
  code: string;
  title: string;
  primary_destination: string;
  redirect_status: 301 | 302 | 307 | 308;
  routing: LinkRoutingRule[];
  ab: LinkABVariant[];
  utm: LinkUTM;
  access: LinkAccess;
  expires_at: string | null;
  click_limit: number | null;
  one_time: boolean;
  change_reason: string;
};

export type LinkUpdateInput = LinkCreateInput & {
  expected_version: number;
  status: 'active' | 'paused';
};

export type LinkVersionRecord = {
  version: number;
  actor_id: string;
  change_reason: string;
  snapshot: unknown;
  risk_fingerprint: string;
  created_at: string;
};

export type BulkLinkAction = 'pause' | 'activate' | 'delete';

export type BulkLinkResult = {
  id: number;
  status: 'success' | 'conflict' | 'failed';
  version?: number;
  error?: string;
};

export type BulkLinkResponse = {
  action: BulkLinkAction;
  results: BulkLinkResult[];
  unsupported_until_owner_nodes: string[];
};

export type ApiTransport = {
  baseUrl?: string;
  headers?: () => HeadersInit;
  fetch?: typeof globalThis.fetch;
};

type ApiErrorEnvelope = {
  error?: {
    code?: string;
    message?: string;
  };
};

export class GoJetApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'GoJetApiError';
    this.status = status;
    this.code = code;
  }
}

function normalizeBaseUrl(value: string | undefined): string {
  return value?.replace(/\/$/, '') ?? '';
}

function buildQuery(filters: LinkListFilters): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(filters)) {
    if (value === undefined || value === null || value === '') continue;
    query.set(key, String(value));
  }
  const serialized = query.toString();
  return serialized ? `?${serialized}` : '';
}

export class GoJetLinksClient {
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
    if (init.body !== undefined && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
    headers.set('Accept', 'application/json');
    const response = await this.doFetch(`${this.baseUrl}${path}`, { ...init, headers });
    if (!response.ok) {
      let envelope: ApiErrorEnvelope = {};
      try { envelope = await response.json() as ApiErrorEnvelope; } catch { /* non-JSON error */ }
      const code = envelope.error?.code ?? 'request_failed';
      const message = envelope.error?.message ?? `Request failed with HTTP ${response.status}`;
      throw new GoJetApiError(response.status, code, message);
    }
    if (response.status === 204) return undefined as T;
    return await response.json() as T;
  }

  list(workspaceId: string, filters: LinkListFilters = {}): Promise<LinkListResponse> {
    return this.request(`/api/workspaces/${encodeURIComponent(workspaceId)}/links${buildQuery(filters)}`);
  }

  get(workspaceId: string, linkId: number): Promise<LinkRecord> {
    return this.request(`/api/workspaces/${encodeURIComponent(workspaceId)}/links/${linkId}`);
  }

  create(workspaceId: string, input: LinkCreateInput): Promise<LinkRecord> {
    return this.request(`/api/workspaces/${encodeURIComponent(workspaceId)}/links`, {
      method: 'POST', body: JSON.stringify(input),
    });
  }

  update(workspaceId: string, linkId: number, input: LinkUpdateInput): Promise<LinkRecord> {
    return this.request(`/api/workspaces/${encodeURIComponent(workspaceId)}/links/${linkId}`, {
      method: 'PATCH', body: JSON.stringify(input),
    });
  }

  remove(workspaceId: string, linkId: number, expectedVersion: number, changeReason: string): Promise<void> {
    return this.request(`/api/workspaces/${encodeURIComponent(workspaceId)}/links/${linkId}`, {
      method: 'DELETE', body: JSON.stringify({ expected_version: expectedVersion, change_reason: changeReason }),
    });
  }

  history(workspaceId: string, linkId: number): Promise<{ items: LinkVersionRecord[] }> {
    return this.request(`/api/workspaces/${encodeURIComponent(workspaceId)}/links/${linkId}/history`);
  }

  restore(workspaceId: string, linkId: number, expectedVersion: number, restoreVersion: number, changeReason: string): Promise<LinkRecord> {
    return this.request(`/api/workspaces/${encodeURIComponent(workspaceId)}/links/${linkId}/restore`, {
      method: 'POST',
      body: JSON.stringify({ expected_version: expectedVersion, restore_version: restoreVersion, change_reason: changeReason }),
    });
  }

  bulk(workspaceId: string, action: BulkLinkAction, items: Array<{ id: number; version: number }>, changeReason: string): Promise<BulkLinkResponse> {
    return this.request(`/api/workspaces/${encodeURIComponent(workspaceId)}/links/bulk`, {
      method: 'POST', body: JSON.stringify({ action, items, change_reason: changeReason }),
    });
  }

  async exportCsv(workspaceId: string): Promise<string> {
    const headers = new Headers(this.headers?.());
    headers.set('Accept', 'text/csv');
    const response = await this.doFetch(`${this.baseUrl}/api/workspaces/${encodeURIComponent(workspaceId)}/links/export`, { headers });
    if (!response.ok) throw new GoJetApiError(response.status, 'export_failed', `Export failed with HTTP ${response.status}`);
    return response.text();
  }
}
