import type { ReactNode } from 'react';
import { useState } from 'react';
import { Link } from '@tanstack/react-router';
import { Button, InlineMessage, useShellViewport } from '@gojet/ui';
import type { ShellState } from '@gojet/utils';

type WebsiteLocale = 'en' | 'zh-CN';
const primaryNav = [
  { en: 'Products', zh: '产品', path: '/products' },
  { en: 'Solutions', zh: '解决方案', path: '/solutions' },
  { en: 'Developers', zh: '开发者', path: '/developers' },
  { en: 'Pricing', zh: '定价', path: '/pricing' },
] as const;

function websitePath(path: string, locale: WebsiteLocale): string {
  if (locale === 'en') return path;
  if (path === '/') return '/zh-CN/';
  return `/zh-CN${path}`;
}

export function WebsiteShell({children,state='normal',locale='en'}:{children:ReactNode;state?:ShellState<'website'>;locale?:WebsiteLocale}){
 const [menuOpen,setMenuOpen]=useState(state==='menu-open'); const viewport=useShellViewport(); const zh=locale==='zh-CN';
 const home=websitePath('/',locale); const docs=zh?'/docs/zh-CN/':'/docs/en/';
 return <div className="site-shell" data-shell="website" data-state={state} data-viewport={viewport} data-locale={locale}>
  {state==='announcement'&&<InlineMessage variant="info">{zh?'当前有一条服务公告。':'Service announcement is active.'}</InlineMessage>}{state==='maintenance-banner'&&<InlineMessage variant="warning">{zh?'正在进行维护，公开内容仍可访问。':'Maintenance is in progress. Public content remains available.'}</InlineMessage>}
  <a className="skip-link" href="#main-content">{zh?'跳到主要内容':'Skip to main content'}</a>
  <header className="site-header">{zh?<a href={home} className="site-brand" aria-label="GoJet 首页">GoJet</a>:<Link to="/" className="site-brand" aria-label="GoJet home">GoJet</Link>}<nav className="site-nav" aria-label={zh?'主导航':'Primary navigation'}>{primaryNav.map((item)=>zh?<a key={item.path} href={websitePath(item.path,locale)}>{item.zh}</a>:<Link key={item.path} to={item.path}>{item.en}</Link>)}<a href={docs}>{zh?'文档':'Docs'}</a></nav><div className="site-actions"><Link to="/login">{zh?'登录':'Sign in'}</Link><Link to="/register" className="site-primary-link">{zh?'开始使用':'Get started'}</Link><Button variant="ghost" className="site-menu-trigger" aria-expanded={menuOpen} onClick={()=>setMenuOpen(open=>!open)}>{zh?'菜单':'Menu'}</Button></div></header>
  {menuOpen&&<div className="site-mobile-sheet" role="dialog" aria-modal="true" aria-label={zh?'移动导航':'Mobile navigation'}><Button variant="ghost" onClick={()=>setMenuOpen(false)}>{zh?'关闭菜单':'Close menu'}</Button>{primaryNav.map((item)=>zh?<a key={item.path} href={websitePath(item.path,locale)} onClick={()=>setMenuOpen(false)}>{item.zh}</a>:<Link key={item.path} to={item.path} onClick={()=>setMenuOpen(false)}>{item.en}</Link>)}<a href={docs}>{zh?'文档':'Docs'}</a></div>}
  <main id="main-content" className="site-content">{children}</main><footer className="site-footer"><span>GoJet V10</span><a href={websitePath('/security',locale)}>{zh?'安全':'Security'}</a><a href={websitePath('/guides',locale)}>{zh?'指南':'Guides'}</a><a href={websitePath('/about',locale)}>{zh?'关于':'About'}</a><a href={websitePath('/contact',locale)}>{zh?'联系':'Contact'}</a><a href={websitePath('/legal/privacy',locale)}>{zh?'隐私':'Privacy'}</a><a href={websitePath('/legal/terms',locale)}>{zh?'条款':'Terms'}</a></footer>
 </div>;
}
export function AuthShell({children,state='normal'}:{children:ReactNode;state?:ShellState<'auth'>}){const viewport=useShellViewport();return <div className="auth-shell" data-shell="auth" data-state={state} data-viewport={viewport}><aside className="auth-brand" aria-label="GoJet product context"><Link to="/" className="site-brand">GoJet</Link><h1>Controlled links, clear operations.</h1><p>Authentication tasks stay focused while product context remains visible.</p></aside><main className="auth-main">{state==='server-error'&&<InlineMessage variant="danger">The authentication service is unavailable.</InlineMessage>}{state==='rate-limited'&&<InlineMessage variant="warning">Too many attempts. Try again later.</InlineMessage>}{state==='provider-error'&&<InlineMessage variant="danger">The identity provider did not complete the request.</InlineMessage>}{state==='maintenance'&&<InlineMessage variant="warning">Authentication maintenance is in progress.</InlineMessage>}<div className="auth-card" aria-busy={state==='submitting'}>{children}</div></main></div>}
