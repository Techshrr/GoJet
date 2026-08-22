import { GoJetApiError } from './links';
import type { ApiTransport } from './links';

export type AnalyticsState = 'success' | 'empty' | 'partial' | 'stale' | 'retention-limited';
export type AnalyticsGranularity = 'hour' | 'day';

export type AnalyticsBucket = {
  key: string;
  clicks: number;
};

export type AnalyticsDimensionCount = {
  value: string;
  clicks: number;
};

export type AnalyticsReport = {
  state: AnalyticsState;
  state_reason: string;
  requested_from: string;
  effective_from: string;
  to: string;
  timezone: string;
  granularity: AnalyticsGranularity;
  retention_limited: boolean;
  retention_cutoff: string;
  data_through_at?: string;
  total_clicks: number;
  total_conversions: number;
  buckets: AnalyticsBucket[];
  dimensions: {
    country: AnalyticsDimensionCount[];
    device: AnalyticsDimensionCount[];
    language: AnalyticsDimensionCount[];
    source: AnalyticsDimensionCount[];
    campaign: AnalyticsDimensionCount[];
  };
  generated_at: string;
};

export type AnalyticsQueryInput = {
  from: string;
  to: string;
  timezone?: string;
  granularity?: AnalyticsGranularity;
  country?: string;
  device?: string;
  language?: string;
  source?: string;
  campaign?: string;
};

export type AnalyticsConversionInput = {
  conversion_id: string;
  campaign_id: string;
  link_id: number;
  occurred_at?: string;
};

export type AnalyticsConversionResponse = {
  conversion_id: string;
  recorded: boolean;
  idempotent_duplicate: boolean;
};

type ApiErrorEnvelope = { error?: { code?: string; message?: string } };

function normalizeBaseUrl(value: string | undefined): string {
  return value?.replace(/\/$/, '') ?? '';
}

function queryString(input: AnalyticsQueryInput): string {
  const params = new URLSearchParams();
  params.set('from', input.from);
  params.set('to', input.to);
  if (input.timezone) params.set('timezone', input.timezone);
  if (input.granularity) params.set('granularity', input.granularity);
  if (input.country) params.set('country', input.country);
  if (input.device) params.set('device', input.device);
  if (input.language) params.set('language', input.language);
  if (input.source) params.set('source', input.source);
  if (input.campaign) params.set('campaign', input.campaign);
  return params.toString();
}

export class GoJetAnalyticsClient {
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

  overview(workspaceId: string, input: AnalyticsQueryInput): Promise<AnalyticsReport> {
    return this.request(`/api/workspaces/${encodeURIComponent(workspaceId)}/analytics/overview?${queryString(input)}`);
  }

  link(workspaceId: string, linkId: number, input: AnalyticsQueryInput): Promise<AnalyticsReport> {
    return this.request(`/api/workspaces/${encodeURIComponent(workspaceId)}/analytics/links/${linkId}?${queryString(input)}`);
  }

  conversion(workspaceId: string, input: AnalyticsConversionInput): Promise<AnalyticsConversionResponse> {
    return this.request(`/api/workspaces/${encodeURIComponent(workspaceId)}/analytics/conversions`, {
      method: 'POST',
      body: JSON.stringify(input),
    });
  }
}
