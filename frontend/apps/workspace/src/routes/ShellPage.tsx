import { EmptyState } from '@gojet/ui';
import { WorkspaceShell } from '../shell/WorkspaceShell';
export default function ShellPage(){return <WorkspaceShell state="notification-attention"><section><h1>Workspace overview</h1><p>P04 validates shared workspace navigation and service-state boundaries. Resource CRUD begins in its owning node.</p><EmptyState title="No shell-level attention required" description="Resource empty states are intentionally not inherited by the shell."/></section></WorkspaceShell>}
