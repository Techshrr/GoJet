import { GoJetApiError } from './links';
import type { ApiTransport } from './links';

export type WorkspaceDomainViewState =
  | 'locked'
  | 'requested'
  | 'active'
  | 'grace'
  | 'suspended'
  | 'expired'
  | 'revoked'
  | 'partial-axis';

export type DomainRoutingState = 'pending' | 'enabled' | 'suspended' | 'revoked' | 'removed';
export type DomainOwnershipStatus = 'pending' | 'verified' | 'failed' | 'lost';
export type DomainIngressDNSStatus = 'pending' | 'valid' | 'invalid';
export type DomainHTTPSStatus = 'pending' | 'active' | 'error';
export type DomainRiskStatus = 'missing' | 'allow' | 'review' | 'block' | 'malformed' | 'stale';
export type DomainEntitlementSource = 'none' | 'plan' | 'manual_approval';
export type DomainEntitlementStatus = 'requested' | 'active' | 'suspended' | 'expired' | 'revoked';

export type WorkspaceDomainEntitlement = {
  state: WorkspaceDomainViewState;
  source: DomainEntitlementSource;
  status: DomainEntitlementStatus;
  domain_limit: number;
  allocated: number;
  remaining: number;
  grace_period: boolean;
  deadline?: string;
  mutation_allowed: boolean;
  existing_routing_allowed: boolean;
  support_ticket_id?: string;
  security_category?: string;
};

export type WorkspaceDomainRecord = {
  id: number;
  hostname: string;
  display_hostname: string;
  routing_state: DomainRoutingState;
  ownership_status: DomainOwnershipStatus;
  ingress_dns_status: DomainIngressDNSStatus;
  https_status: DomainHTTPSStatus;
  risk_status: DomainRiskStatus;
  ownership_token_version: number;
  ownership_verified_at?: string;
  ingress_dns_checked_at?: string;
  https_checked_at?: string;
  risk_checked_at?: string;
  risk_policy_version?: string;
  security_category?: string;
  ready_for_new_links: boolean;
  ready_for_routing: boolean;
  created_at: string;
  updated_at: string;
};

export type DomainRevalidationRecord = {
  axis: 'entitlement' | 'ownership' | 'ingress_dns' | 'https' | 'risk';
  result: 'pass' | 'fail' | 'pending' | 'stale' | 'error';
  policy_version: string;
  checked_at: string;
  next_due_at?: string;
};

export type DomainAssignedLink = {
  id: number;
  code: string;
  status: string;
};

export type WorkspaceDomainsResponse = {
  entitlement: WorkspaceDomainEntitlement;
  items: WorkspaceDomainRecord[];
};

export type WorkspaceDomainDetailResponse = {
  entitlement: WorkspaceDomainEntitlement;
  domain: WorkspaceDomainRecord;
  assigned_links: DomainAssignedLink[];
  revalidations: DomainRevalidationRecord[];
};

export type CreatedDomainRecord = {
  id: number;
  workspace_id: string;
  hostname_ascii: string;
  display_hostname: string;
  routing_state: DomainRoutingState;
  ownership_status: DomainOwnershipStatus;
  ingress_dns_status: DomainIngressDNSStatus;
  https_status: DomainHTTPSStatus;
  risk_status: DomainRiskStatus;
  ownership_token_version: number;
  ownership_secret_issued_at: string;
  created_at: string;
  updated_at: string;
};

export type CreatedWorkspaceDomain = {
  domain: CreatedDomainRecord;
  ownership_txt_name: string;
  ownership_txt_value: string;
};

export type CreateWorkspaceDomainInput = {
  hostname: string;
  change_reason: string;
};

type ApiErrorEnvelope = { error?: { code?: string; message?: string } };

function normalizeBaseUrl(value: string | undefined): string {
  return value?.replace(/\/$/, '') ?? '';
}

export class GoJetDomainsClient {
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
      throw new GoJetApiError(
        response.status,
        envelope.error?.code ?? 'request_failed',
        envelope.error?.message ?? `Request failed with HTTP ${response.status}`,
      );
    }
    return await response.json() as T;
  }

  list(workspaceId: string): Promise<WorkspaceDomainsResponse> {
    return this.request(`/api/workspaces/${encodeURIComponent(workspaceId)}/domains`);
  }

  get(workspaceId: string, domainId: number): Promise<WorkspaceDomainDetailResponse> {
    return this.request(`/api/workspaces/${encodeURIComponent(workspaceId)}/domains/${domainId}`);
  }

  create(workspaceId: string, input: CreateWorkspaceDomainInput): Promise<CreatedWorkspaceDomain> {
    return this.request(`/api/workspaces/${encodeURIComponent(workspaceId)}/domains`, {
      method: 'POST',
      body: JSON.stringify(input),
    });
  }
}
