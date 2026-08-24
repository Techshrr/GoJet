import { GoJetSupportClient } from '@gojet/api-client';
import { readWorkspaceRuntime } from '../links/runtime';

export type SupportRuntime = {
  workspaceId: string;
  actorId: string;
  role: 'owner' | 'admin' | 'member' | 'viewer';
  email: string;
  displayName: string;
  turnstileToken: string;
  testAuthority: boolean;
};

export function readSupportRuntime(): SupportRuntime | null {
  const workspace = readWorkspaceRuntime();
  if (!workspace) return null;
  const email = String(import.meta.env.VITE_GOJET_TEST_EMAIL ?? '').trim();
  if (workspace.testAuthority && !email) return null;
  return {
    workspaceId: workspace.workspaceId,
    actorId: workspace.actorId,
    role: workspace.role,
    email,
    displayName: String(import.meta.env.VITE_GOJET_TEST_DISPLAY_NAME ?? '').trim(),
    turnstileToken: String(import.meta.env.VITE_GOJET_TEST_SUPPORT_TURNSTILE_TOKEN ?? '').trim(),
    testAuthority: workspace.testAuthority,
  };
}

export function createSupportClient(runtime: SupportRuntime): GoJetSupportClient {
  return new GoJetSupportClient({
    headers: () => ({
      'X-GoJet-Test-Actor': runtime.actorId,
      'X-GoJet-Test-Email': runtime.email,
      'X-GoJet-Test-Display-Name': runtime.displayName,
    }),
  });
}
