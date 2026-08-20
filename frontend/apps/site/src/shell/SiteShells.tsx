import type { ReactNode } from 'react';
import { useState } from 'react';
import { Link } from '@tanstack/react-router';
import { Button, InlineMessage } from '@gojet/ui';
import { useShellViewport } from '@gojet/shell-runtime';
import type { ShellState } from '@gojet/utils';

const primaryNav = [['Products','/products'],['Solutions','/solutions'],['Developers','/developers'],['Pricing','/pricing']] as const;
export function WebsiteShell({children,state='normal'}:{children:ReactNode;state?:ShellState<'website'>}){
 const [menuOpen,setMenuOpen]=useState(state==='menu-open'); const viewport=useShellViewport();
 return <div className="site-shell" data-shell="website" data-state={state} data-viewport={viewport}>
  {state==='announcement'&&<InlineMessage variant="info">Service announcement is active.</InlineMessage>}{state==='maintenance-banner'&&<InlineMessage variant="warning">Maintenance is in progress. Public content remains available.</InlineMessage>}
  <header className="site-header"><Link to="/" className="site-brand" aria-label="GoJet home">GoJet</Link><nav className="site-nav" aria-label="Primary navigation">{primaryNav.map(([label,to])=><Link key={to} to={to}>{label}</Link>)}<a href="/docs/en/">Docs</a></nav><div className="site-actions"><Link to="/login">Sign in</Link><Link to="/register" className="site-primary-link">Get started</Link><Button variant="ghost" className="site-menu-trigger" aria-expanded={menuOpen} onClick={()=>setMenuOpen(open=>!open)}>Menu</Button></div></header>
  {menuOpen&&<div className="site-mobile-sheet" role="dialog" aria-modal="true" aria-label="Mobile navigation"><Button variant="ghost" onClick={()=>setMenuOpen(false)}>Close menu</Button>{primaryNav.map(([label,to])=><Link key={to} to={to} onClick={()=>setMenuOpen(false)}>{label}</Link>)}<a href="/docs/en/">Docs</a></div>}
  <main id="main-content" className="site-content">{children}</main><footer className="site-footer"><span>GoJet V10</span><a href="/legal/privacy">Privacy</a><a href="/legal/terms">Terms</a></footer>
 </div>;
}
export function AuthShell({children,state='normal'}:{children:ReactNode;state?:ShellState<'auth'>}){const viewport=useShellViewport();return <div className="auth-shell" data-shell="auth" data-state={state} data-viewport={viewport}><aside className="auth-brand" aria-label="GoJet product context"><Link to="/" className="site-brand">GoJet</Link><h1>Controlled links, clear operations.</h1><p>Authentication tasks stay focused while product context remains visible.</p></aside><main className="auth-main">{state==='server-error'&&<InlineMessage variant="danger">The authentication service is unavailable.</InlineMessage>}{state==='rate-limited'&&<InlineMessage variant="warning">Too many attempts. Try again later.</InlineMessage>}{state==='provider-error'&&<InlineMessage variant="danger">The identity provider did not complete the request.</InlineMessage>}{state==='maintenance'&&<InlineMessage variant="warning">Authentication maintenance is in progress.</InlineMessage>}<div className="auth-card" aria-busy={state==='submitting'}>{children}</div></main></div>}
