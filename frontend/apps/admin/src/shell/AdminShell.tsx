import type{ReactNode}from'react';import{Link}from'@tanstack/react-router';import{InlineMessage,useShellViewport}from'@gojet/ui';import type{ShellState}from'@gojet/utils';const groups = [
  ['Users', '/admin/users'],
  ['Workspaces', '/admin/workspaces'],
  ['Trust & Safety', '/admin/trust/destination-risk'],
  ['Operations', '/admin/operations/jobs'],
  ['Tickets', '/admin/tickets'],
  ['Mail', '/admin/mail'],
  ['Commerce', '/admin/commerce/plans'],
  ['Access', '/admin/access/administrators'],
  ['Platform', '/admin/platform/general'],
] as const;
export function AdminShell({children,state='normal'}:{children:ReactNode;state?:ShellState<'admin'>}){const viewport=useShellViewport();return <div className="admin-shell" data-shell="admin" data-state={state} data-viewport={viewport}><aside className="admin-sidebar"><Link to="/admin" className="admin-logo">GoJet Admin</Link><nav aria-label="Admin navigation">{groups.map(([label,to])=><Link key={to} to={to}>{label}</Link>)}</nav><div className="admin-context"><strong>Administrator</strong><span>Production</span><span>Scoped permissions</span><a href="/admin/audit">Audit log</a></div></aside><div className="admin-main"><header className="admin-header"><nav aria-label="Breadcrumb">Admin / Overview</nav><label>Global search <input type="search" aria-label="Search permitted users, workspaces and resources"/></label></header>{state==='admin-auth-required'&&<InlineMessage variant="danger">Administrator authentication is required.</InlineMessage>}{state==='permission-denied'&&<InlineMessage variant="danger">Your administrator permission does not cover this area.</InlineMessage>}{state==='maintenance'&&<InlineMessage variant="warning">Administrative maintenance is active.</InlineMessage>}{state==='partial-service-degradation'&&<InlineMessage variant="warning">Some operational data is temporarily unavailable.</InlineMessage>}<main className="admin-content">{children}</main></div></div>}
