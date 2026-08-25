import { GoJetApiError } from './links';
import type { ApiTransport } from './links';

export type AuthProvider = { provider: 'google' | 'facebook' | 'github' | 'qq' | 'wechat' | 'rainbow'; enabled: boolean };
export type AuthProvidersResponse = { providers: AuthProvider[] };
export type AuthStatusResponse = { status: string; expires_at?: string; verified_at?: string };
export type OAuthCallbackResponse = { status: 'handoff_ready'; handoff_code: string; expires_at: string };
export type OAuthHandoffResponse =
  | { status: 'authenticated'; expires_at: string }
  | { status: 'registration_required'; registration_code: string; expires_at: string };
export type SocialRegistrationState = {
  provider: AuthProvider['provider'];
  email: string;
  provider_email_verified: boolean;
  requires_email_verification: boolean;
  display_name: string;
  expires_at: string;
};

type ApiErrorEnvelope = { error?: { code?: string; message?: string } };

function normalizeBaseUrl(value: string | undefined): string {
  return value?.replace(/\/$/, '') ?? '';
}

function correlationId(): string {
  const random = globalThis.crypto?.randomUUID?.() ?? Math.random().toString(36).slice(2);
  return `ui-${Date.now().toString(36)}-${random}`.slice(0, 120);
}

export class GoJetAuthClient {
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
    const response = await this.doFetch(`${this.baseUrl}${path}`, { ...init, headers, credentials: 'include' });
    if (!response.ok) {
      let envelope: ApiErrorEnvelope = {};
      try { envelope = await response.json() as ApiErrorEnvelope; } catch { /* non-JSON */ }
      throw new GoJetApiError(
        response.status,
        envelope.error?.code ?? 'request_failed',
        envelope.error?.message ?? `Request failed with HTTP ${response.status}`,
      );
    }
    if (response.status === 204) return undefined as T;
    return await response.json() as T;
  }

  providers(): Promise<AuthProvidersResponse> {
    return this.request('/api/public/auth/providers');
  }

  login(email: string, password: string): Promise<AuthStatusResponse> {
    return this.request('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ email, password, correlation_id: correlationId() }),
    });
  }

  requestLoginCode(email: string): Promise<AuthStatusResponse> {
    return this.request('/api/public/login-email-code', {
      method: 'POST',
      body: JSON.stringify({ email, correlation_id: correlationId() }),
    });
  }

  loginWithCode(code: string): Promise<AuthStatusResponse> {
    return this.request('/api/public/login-email-code', {
      method: 'POST',
      body: JSON.stringify({ code, correlation_id: correlationId() }),
    });
  }

  register(email: string, displayName: string, password: string): Promise<AuthStatusResponse> {
    return this.request('/api/auth/register', {
      method: 'POST',
      body: JSON.stringify({ email, display_name: displayName, password, correlation_id: correlationId() }),
    });
  }

  verifyEmail(code: string): Promise<AuthStatusResponse> {
    return this.request('/api/auth/verifyemail', {
      method: 'POST',
      body: JSON.stringify({ code, correlation_id: correlationId() }),
    });
  }

  verifyRegistrationCode(code: string): Promise<AuthStatusResponse> {
    return this.request('/api/public/register-email-code', {
      method: 'POST',
      body: JSON.stringify({ code, correlation_id: correlationId() }),
    });
  }

  resendVerification(email: string): Promise<AuthStatusResponse> {
    return this.request('/api/mail/verification', {
      method: 'POST',
      body: JSON.stringify({ email, correlation_id: correlationId() }),
    });
  }

  forgotPassword(email: string): Promise<AuthStatusResponse> {
    return this.request('/api/auth/forgotpassword', {
      method: 'POST',
      body: JSON.stringify({ email, correlation_id: correlationId() }),
    });
  }

  resetPassword(token: string, password: string): Promise<AuthStatusResponse> {
    return this.request('/api/auth/resetpassword', {
      method: 'POST',
      body: JSON.stringify({ token, password, correlation_id: correlationId() }),
    });
  }

  oauthCallback(provider: string, state: string, code: string): Promise<OAuthCallbackResponse> {
    const query = new URLSearchParams({ state, code });
    return this.request(`/api/public/auth/${encodeURIComponent(provider)}/callback?${query}`);
  }

  exchangeHandoff(code: string): Promise<OAuthHandoffResponse> {
    return this.request('/api/public/auth/handoff', {
      method: 'POST',
      body: JSON.stringify({ code, correlation_id: correlationId() }),
    });
  }

  socialRegistration(code: string): Promise<SocialRegistrationState> {
    return this.request(`/api/public/auth/social-registration?${new URLSearchParams({ code })}`);
  }

  completeSocialRegistration(code: string, email = '', verificationCode = ''): Promise<AuthStatusResponse> {
    return this.request('/api/public/auth/social-registration/complete', {
      method: 'POST',
      body: JSON.stringify({
        code,
        email,
        verification_code: verificationCode,
        correlation_id: correlationId(),
      }),
    });
  }
}
