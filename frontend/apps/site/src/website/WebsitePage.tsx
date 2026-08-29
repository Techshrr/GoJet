import { useEffect } from 'react';
import content from './content.json';
import authority from './authority.json';
import { WebsiteShell } from '../shell/SiteShells';

type Locale = 'en' | 'zh-CN';
type Copy = { title:string; description:string; h1:string; eyebrow:string; lede:string; points:string[] };
type PageRecord = { routeId:string; path:string; zhPath:string; updatedTime:string; contentOwner:string; structuredData:string[]; links:string[]; en:Copy; zh:Copy };
type SurfaceState = { kind: 'info' | 'warning'; en: string; zh: string };
const pages = content as PageRecord[];
const publicBase = 'https://gojet.cc';

function localizePath(path: string, locale: Locale): string {
  if (locale === 'en') return path;
  if (path === '/') return '/zh-CN/';
  return `/zh-CN${path}`;
}
function normalized(path: string): string { return path !== '/' && path !== '/zh-CN/' && path.endsWith('/') ? path.slice(0,-1) : path; }
function findPage(pathname: string): { page:PageRecord; locale:Locale } | null {
  const current=normalized(pathname);
  for(const page of pages){ if(current===normalized(page.path)) return {page,locale:'en'}; if(current===normalized(page.zhPath)) return {page,locale:'zh-CN'}; }
  return null;
}
function ensureMeta(name:string, contentValue:string){ let node=document.head.querySelector<HTMLMetaElement>(`meta[name="${name}"]`); if(!node){node=document.createElement('meta');node.name=name;document.head.append(node);} node.content=contentValue; }
function ensureLink(rel:string, href:string, hrefLang?:string){ const selector=hrefLang?`link[rel="${rel}"][hreflang="${hrefLang}"]`:`link[rel="${rel}"]:not([hreflang])`; let node=document.head.querySelector<HTMLLinkElement>(selector); if(!node){node=document.createElement('link');node.rel=rel;if(hrefLang)node.hreflang=hrefLang;document.head.append(node);} node.href=href; }
function requestedState(): string {
  if (typeof window === 'undefined') return 'default';
  return new URLSearchParams(window.location.search).get('state') ?? 'default';
}
function stateMessage(routeId:string,state:string): SurfaceState | null {
  if(routeId==='WEB-HOME'){
    if(state==='announcement-partial') return {kind:'info',en:'Announcements are temporarily unavailable; core product content remains available.',zh:'服务公告暂时不可用；核心产品内容仍可访问。'};
    if(state==='pricing-partial') return {kind:'warning',en:'Authoritative plan data is unavailable; GoJet is not showing estimated prices.',zh:'权威套餐数据暂不可用；GoJet 不展示估算价格。'};
    if(state==='maintenance') return {kind:'warning',en:'Website maintenance is in progress; public product information remains read-only.',zh:'Website 正在维护；公开产品信息保持只读可访问。'};
  }
  if(routeId==='WEB-PRICING'){
    if(state==='loading-data') return {kind:'info',en:'Loading authoritative plan data. No estimated commercial values are shown.',zh:'正在加载权威套餐数据；不会展示估算商业数值。'};
    if(state==='data-unavailable') return {kind:'warning',en:'Authoritative plan data is unavailable. Prices and availability are intentionally not estimated.',zh:'权威套餐数据暂不可用。价格与可用性不会被估算。'};
    if(state==='maintenance') return {kind:'warning',en:'Pricing data is unavailable during maintenance; no cached value is presented as current.',zh:'维护期间定价数据不可用；不会把缓存数值展示为当前价格。'};
  }
  return null;
}

export function WebsitePage({pathname}:{pathname:string}){
  const match=findPage(pathname);
  if(!match) return <WebsiteShell><section className="website-page"><p className="website-eyebrow">GoJet V10</p><h1>Page not found</h1><p>The requested Website route is not part of the approved GoJet V10 Website registry.</p></section></WebsiteShell>;
  const {page,locale}=match; const copy=locale==='en'?page.en:page.zh; const canonical=locale==='en'?page.path:page.zhPath; const state=requestedState(); const notice=stateMessage(page.routeId,state);
  useEffect(()=>{ document.documentElement.lang=locale; document.title=copy.title; ensureMeta('description',copy.description); ensureMeta('robots','index,follow'); ensureLink('canonical',`${publicBase}${canonical}`); ensureLink('alternate',`${publicBase}${page.path}`,'en'); ensureLink('alternate',`${publicBase}${page.zhPath}`,'zh-CN'); ensureLink('alternate',`${publicBase}${page.path}`,'x-default'); },[canonical,copy.description,copy.title,locale,page.path,page.zhPath]);
  const peerHref=locale==='en'?page.zhPath:page.path;
  return <WebsiteShell locale={locale}><article className="website-page" data-route-id={page.routeId} data-locale={locale} data-surface-state={state}>
    {notice?<aside className="website-state" data-kind={notice.kind} role="status"><strong>{locale==='en'?'Current state':'当前状态'}</strong><span>{locale==='en'?notice.en:notice.zh}</span></aside>:null}
    <header className="website-hero"><p className="website-eyebrow">{copy.eyebrow}</p><h1>{copy.h1}</h1><p className="website-lede">{copy.lede}</p><div className="website-hero-actions"><a className="site-primary-link" href="/register">{locale==='en'?'Get started':'开始使用'}</a><a href={locale==='en'?'/docs/en/':'/docs/zh-CN/'}>{locale==='en'?'Read documentation':'阅读文档'}</a></div></header>
    <section className="website-principles" aria-labelledby={`${page.routeId}-principles`}><h2 id={`${page.routeId}-principles`}>{locale==='en'?'What this page commits to':'本页面承诺的边界'}</h2><ul>{copy.points.map((point)=><li key={point}>{point}</li>)}</ul></section>
    {page.links.length?<nav className="website-related" aria-label={locale==='en'?'Related pages':'相关页面'}><h2>{locale==='en'?'Related GoJet pages':'相关 GoJet 页面'}</h2><div>{page.links.map((href)=><a key={href} href={localizePath(href,locale)}>{localizePath(href,locale)}</a>)}</div></nav>:null}
    <footer className="website-record-meta"><span>{locale==='en'?'English':'简体中文'}</span><span>{locale==='en'?'Content owner':'内容负责人'}: {page.contentOwner}</span><span>{locale==='en'?'Updated':'更新'}: {page.updatedTime}</span><a href={peerHref} hrefLang={locale==='en'?'zh-CN':'en'}>{locale==='en'?'简体中文':'English'}</a></footer>
    <script type="application/json" data-website-authority dangerouslySetInnerHTML={{__html:JSON.stringify({schema:authority.schema,routeId:page.routeId})}} />
  </article></WebsiteShell>;
}
export { pages as websitePages };
