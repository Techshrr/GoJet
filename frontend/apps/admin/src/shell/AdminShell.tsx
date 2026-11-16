import type{ReactNode}from'react';import{Link}from'@tanstack/react-router';import{InlineMessage,useShellViewport}from'@gojet/ui';import type{ShellState}from'@gojet/utils';const groups = [
  ['Overview', '/admin'],
  ['Users', '/admin/users'],
  ['Workspaces', '/admin/workspaces'],
  ['Resources', '/admin/resources/links'],
  ['Trust & Safety', '/admin/trust/destination-risk'],
  ['Operations', '/admin/operations/jobs'],
  ['Tickets', '/admin/tickets'],
  ['Mail', '/admin/mail'],
  ['Mail templates', '/admin/platform/mail-templates'],
  ['Commerce', '/admin/commerce/plans'],
  ['Access', '/admin/access/administrators'],
  ['Platform', '/admin/platform/general'],
  ['Sign-in providers', '/admin/platform/oauth'],
] as const;
export function AdminShell({children,state='normal'}:{children:ReactNode;state?:ShellState<'admin'>}){const viewport=useShellViewport();return <div className="admin-shell" data-shell="admin" data-state={state} data-viewport={viewport}><aside className="admin-sidebar"><Link to="/admin" className="admin-logo">Linketic</Link><nav aria-label="Admin navigation">{groups.map(([label,to])=><Link key={to} to={to} activeOptions={{ exact: true }} activeProps={{ className: 'is-current' }}>{label}</Link>)}</nav><div className="admin-context"><strong>Administrator</strong><a href="/">View website</a><a href="/admin/audit">Audit log</a></div></aside><div className="admin-main"><header className="admin-header"><nav aria-label="Breadcrumb">Administration / {[...groups].sort((a,b)=>b[1].length-a[1].length).find(([,path]) => window.location.pathname===path || (path!=='/admin' && window.location.pathname.startsWith(path+'/')))?.[0] || 'Overview'}</nav><a href="/app">Customer workspace</a></header><details className="admin-mobile-navigation" data-mobile-navigation onKeyDown={event=>{
 if(event.key==='Escape'){event.preventDefault();event.currentTarget.open=false;event.currentTarget.querySelector('summary')?.focus();}
}} onClick={event=>{if(event.target instanceof Element&&event.target.closest('a[href]'))event.currentTarget.open=false;}}>
 <summary>Admin navigation</summary><nav aria-label="Mobile Admin navigation">
 {groups.map(([label,to])=><Link key={to} to={to} activeOptions={{ exact: true }} activeProps={{ className: 'is-current' }}>{label}</Link>)}<a href="/admin/audit">Audit log</a>
 </nav></details>{state==='admin-auth-required'&&<InlineMessage variant="danger">Administrator authentication is required.</InlineMessage>}{state==='permission-denied'&&<InlineMessage variant="danger">Your administrator permission does not cover this area.</InlineMessage>}{state==='maintenance'&&<InlineMessage variant="warning">Administrative maintenance is active.</InlineMessage>}{state==='partial-service-degradation'&&<InlineMessage variant="warning">Some operational data is temporarily unavailable.</InlineMessage>}<main className="admin-content">{children}</main></div></div>}
