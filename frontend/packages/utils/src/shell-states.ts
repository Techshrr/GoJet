export const SHELL_STATES = {
  website: ['normal', 'menu-open', 'locale-switch', 'announcement', 'maintenance-banner'],
  auth: ['normal', 'submitting', 'server-error', 'rate-limited', 'provider-error', 'maintenance'],
  docs: ['article', 'search-open', 'nav-drawer', 'not-found', 'offline-static'],
  workspace: ['loading-workspace', 'workspace-empty', 'read-only-role', 'workspace-suspended', 'api-offline', 'notification-attention'],
  admin: ['admin-auth-required', 'permission-denied', 'maintenance', 'partial-service-degradation', 'normal'],
  installer: ['session-ready', 'step-checking', 'step-pass', 'hard-failure', 'retryable-failure', 'install-running', 'lock-failed', 'complete', 'already-locked'],
} as const;

export type ShellSurface = keyof typeof SHELL_STATES;
export type ShellState<S extends ShellSurface> = (typeof SHELL_STATES)[S][number];

export function isShellState<S extends ShellSurface>(surface: S, value: string): value is ShellState<S> {
  return (SHELL_STATES[surface] as readonly string[]).includes(value);
}
