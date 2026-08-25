export type OAuthProviderConfig = {
  provider: string;
  enabled: boolean;
  configured: boolean;
  client_id: string;
  authorization_url: string;
  token_url: string;
  userinfo_url: string;
  redirect_uri: string;
  scopes: string[];
  secret_configured: boolean;
  version: number;
};

export type OAuthProviderInput = {
  enabled: boolean;
  client_id: string;
  client_secret: string;
  authorization_url: string;
  token_url: string;
  userinfo_url: string;
  redirect_uri: string;
  scopes: string[];
};

type ProviderListResponse = { providers: OAuthProviderConfig[]; csrf_token: string };
type ProviderUpdateResponse = { provider: OAuthProviderConfig };
export type ProviderTestResponse = {
  provider: string;
  status: string;
  configured: boolean;
  enabled: boolean;
  secret_configured: boolean;
};

export class AdminOAuthAPIError extends Error {
  constructor(public readonly status: number) {
    super(`Admin OAuth request failed with ${status}`);
  }
}

async function requestJSON<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set('Accept', 'application/json');
  if (init.body !== undefined && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
  const response = await fetch(path, { ...init, credentials: 'include', headers });
  if (!response.ok) throw new AdminOAuthAPIError(response.status);
  return response.json() as Promise<T>;
}

export async function listOAuthProviders(): Promise<ProviderListResponse> {
  return requestJSON<ProviderListResponse>('/api/admin/oauth/providers');
}

function correlationID(action: string): string {
  return `p15-admin-oauth-${action}-${crypto.randomUUID()}`;
}

export async function updateOAuthProvider(provider: string, input: OAuthProviderInput): Promise<OAuthProviderConfig> {
  const authority = await listOAuthProviders();
  const response = await requestJSON<ProviderUpdateResponse>(`/api/admin/oauth/providers/${encodeURIComponent(provider)}`, {
    method: 'PATCH',
    headers: {
      'X-CSRF-Token': authority.csrf_token,
      'X-Request-ID': correlationID('update'),
    },
    body: JSON.stringify(input),
  });
  return response.provider;
}

export async function testOAuthProvider(provider: string): Promise<ProviderTestResponse> {
  const authority = await listOAuthProviders();
  return requestJSON<ProviderTestResponse>(`/api/admin/oauth/providers/${encodeURIComponent(provider)}/test`, {
    method: 'POST',
    headers: {
      'X-CSRF-Token': authority.csrf_token,
      'X-Request-ID': correlationID('test'),
    },
    body: JSON.stringify({}),
  });
}
