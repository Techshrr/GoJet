import { useAdminSession, ErrorNotice } from './api';
import { AdminShell } from '../shell/AdminShell';

export type ModuleRuntime = { actorId: string; email: string; displayName: string; csrfToken?: string; testAuth?: boolean };

export function useModuleRuntime(fixture: ModuleRuntime | null) {
  const auth = useAdminSession(fixture === null);
  const runtime: ModuleRuntime | null = fixture ?? (auth.session ? {
    actorId: auth.session.administrator.id,
    email: auth.session.administrator.email,
    displayName: auth.session.administrator.display_name,
    csrfToken: auth.session.csrf_token,
  } : null);
  return { runtime, busy: fixture === null && auth.busy, error: fixture === null ? auth.error : '' };
}

export function ModuleSessionNotice({ busy, error }: { busy: boolean; error: string }) {
  return <AdminShell><section className="p17-admin-page">{busy ? <p role="status">Checking administrator session…</p> : <ErrorNotice error={error || 'unauthorized'} />}</section></AdminShell>;
}

export function moduleHeaders(runtime: ModuleRuntime): Record<string, string> {
  return runtime.testAuth ? {
    'X-GoJet-Test-Actor': runtime.actorId,
    'X-GoJet-Test-Email': runtime.email,
    'X-GoJet-Test-Display-Name': runtime.displayName,
  } : {};
}
