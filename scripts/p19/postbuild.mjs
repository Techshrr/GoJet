import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';

const cwd = process.cwd();
const dist = join(cwd, 'dist');
const pages = JSON.parse(readFileSync(join(cwd, 'src/website/content.json'), 'utf8'));
const base = 'https://gojet.cc';
const sourceShell = readFileSync(join(dist, 'index.html'), 'utf8');
writeFileSync(join(dist, 'app-shell.html'), sourceShell);

const esc = (value) => String(value).replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('>','&gt;').replaceAll('"','&quot;').replaceAll("'",'&#39;');
const canonical = (path) => `${base}${path}`;
const localize = (path, locale) => locale === 'en' ? path : path === '/' ? '/zh-CN/' : `/zh-CN${path}`;
const nav = [
  ['/products','Products','产品'],['/solutions','Solutions','解决方案'],['/developers','Developers','开发者'],['/pricing','Pricing','定价']
];

function breadcrumbJson(path, locale) {
  const clean = path.replace(/^\/zh-CN(?=\/|$)/,'') || '/';
  const parts = clean.split('/').filter(Boolean);
  const items = [{ '@type':'ListItem', position:1, name:'GoJet', item:canonical(locale==='en'?'/':'/zh-CN/') }];
  let current='';
  parts.forEach((segment,index)=>{ current += `/${segment}`; items.push({ '@type':'ListItem', position:index+2, name:segment.replaceAll('-',' '), item:canonical(localize(current,locale)) }); });
  return { '@context':'https://schema.org', '@type':'BreadcrumbList', itemListElement:items };
}
function structured(page, path, locale) {
  if (page.routeId === 'WEB-HOME') return { '@context':'https://schema.org', '@graph':[
    { '@type':'WebSite', name:'GoJet', url:base },
    { '@type':'Organization', name:'GoJet', url:base }
  ]};
  if (page.routeId === 'WEB-ABOUT') return { '@context':'https://schema.org', '@graph':[
    { '@type':'Organization', name:'GoJet', url:base }, breadcrumbJson(path,locale)
  ]};
  return breadcrumbJson(path,locale);
}
function staticBody(page, path, locale) {
  const copy = locale === 'en' ? page.en : page.zh;
  const zh = locale === 'zh-CN';
  const related = page.links.map((href)=>`<a href="${esc(localize(href,locale))}">${esc(localize(href,locale))}</a>`).join('');
  const navHtml = nav.map(([href,en,cn])=>`<a href="${esc(localize(href,locale))}">${esc(zh?cn:en)}</a>`).join('');
  return `<div class="site-shell" data-shell="website" data-state="normal" data-locale="${locale}"><a class="skip-link" href="#main-content">${zh?'跳到主要内容':'Skip to main content'}</a><header class="site-header"><a class="site-brand" href="${esc(localize('/',locale))}" aria-label="GoJet ${zh?'首页':'home'}">GoJet</a><nav class="site-nav" aria-label="${zh?'主导航':'Primary navigation'}">${navHtml}<a href="${zh?'/docs/zh-CN/':'/docs/en/'}">${zh?'文档':'Docs'}</a></nav><div class="site-actions"><a href="/login">${zh?'登录':'Sign in'}</a><a class="site-primary-link" href="/register">${zh?'开始使用':'Get started'}</a></div></header><main id="main-content" class="site-content"><article class="website-page" data-route-id="${esc(page.routeId)}" data-locale="${locale}"><header class="website-hero"><p class="website-eyebrow">${esc(copy.eyebrow)}</p><h1>${esc(copy.h1)}</h1><p class="website-lede">${esc(copy.lede)}</p><div class="website-hero-actions"><a class="site-primary-link" href="/register">${zh?'开始使用':'Get started'}</a><a href="${zh?'/docs/zh-CN/':'/docs/en/'}">${zh?'阅读文档':'Read documentation'}</a></div></header><section class="website-principles"><h2>${zh?'本页面承诺的边界':'What this page commits to'}</h2><ul>${copy.points.map((point)=>`<li>${esc(point)}</li>`).join('')}</ul></section>${page.links.length?`<nav class="website-related" aria-label="${zh?'相关页面':'Related pages'}"><h2>${zh?'相关 GoJet 页面':'Related GoJet pages'}</h2><div>${related}</div></nav>`:''}<footer class="website-record-meta"><span>${zh?'简体中文':'English'}</span><span>${zh?'内容负责人':'Content owner'}: ${esc(page.contentOwner)}</span><span>${zh?'更新':'Updated'}: ${esc(page.updatedTime)}</span><a href="${esc(zh?page.path:page.zhPath)}" hreflang="${zh?'en':'zh-CN'}">${zh?'English':'简体中文'}</a></footer></article></main><footer class="site-footer"><span>GoJet V10</span><a href="${esc(localize('/security',locale))}">${zh?'安全':'Security'}</a><a href="${esc(localize('/guides',locale))}">${zh?'指南':'Guides'}</a><a href="${esc(localize('/about',locale))}">${zh?'关于':'About'}</a><a href="${esc(localize('/contact',locale))}">${zh?'联系':'Contact'}</a><a href="${esc(localize('/legal/privacy',locale))}">${zh?'隐私':'Privacy'}</a><a href="${esc(localize('/legal/terms',locale))}">${zh?'条款':'Terms'}</a></footer></div>`;
}
function render(page, path, locale) {
  const copy = locale === 'en' ? page.en : page.zh;
  const body = staticBody(page,path,locale);
  const jsonld = JSON.stringify(structured(page,path,locale)).replaceAll('<','\\u003c');
  const metadata = `<meta name="description" content="${esc(copy.description)}"><meta name="robots" content="index,follow"><link rel="canonical" href="${esc(canonical(path))}"><link rel="alternate" hreflang="en" href="${esc(canonical(page.path))}"><link rel="alternate" hreflang="zh-CN" href="${esc(canonical(page.zhPath))}"><link rel="alternate" hreflang="x-default" href="${esc(canonical(page.path))}"><meta property="og:type" content="website"><meta property="og:site_name" content="GoJet"><meta property="og:title" content="${esc(copy.title)}"><meta property="og:description" content="${esc(copy.description)}"><meta property="og:url" content="${esc(canonical(path))}"><meta name="twitter:card" content="summary"><meta name="twitter:title" content="${esc(copy.title)}"><meta name="twitter:description" content="${esc(copy.description)}"><script type="application/ld+json">${jsonld}</script>`;
  return sourceShell
    .replace('<html lang="en">', `<html lang="${locale}">`)
    .replace(/<title>[^<]*<\/title>/, `<title>${esc(copy.title)}</title>`)
    .replace('</head>', `${metadata}</head>`)
    .replace('<div id="root"></div>', `<div id="root" data-p19-static="true">${body}</div>`);
}
function outputPath(path) {
  if (path === '/') return join(dist,'index.html');
  if (path === '/zh-CN/') return join(dist,'zh-CN','index.html');
  return join(dist,`${path.slice(1)}.html`);
}
for (const page of pages) {
  for (const [path,locale] of [[page.path,'en'],[page.zhPath,'zh-CN']]) {
    const target=outputPath(path); mkdirSync(dirname(target),{recursive:true}); writeFileSync(target,render(page,path,locale));
  }
}
const sitemapEntries=[];
for(const page of pages){ for(const [path,locale] of [[page.path,'en'],[page.zhPath,'zh-CN']]) sitemapEntries.push(`<url><loc>${esc(canonical(path))}</loc><lastmod>${esc(page.updatedTime)}</lastmod><xhtml:link rel="alternate" hreflang="en" href="${esc(canonical(page.path))}"/><xhtml:link rel="alternate" hreflang="zh-CN" href="${esc(canonical(page.zhPath))}"/><xhtml:link rel="alternate" hreflang="x-default" href="${esc(canonical(page.path))}"/></url>`); }
writeFileSync(join(dist,'sitemap-website.xml'),`<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">${sitemapEntries.join('')}</urlset>\n`);
writeFileSync(join(dist,'website-manifest.json'),JSON.stringify({schema:'gojet.website-manifest.v1',generatedFrom:'src/website/content.json',routeIds:pages.map((p)=>p.routeId),pages:pages.flatMap((p)=>[{routeId:p.routeId,locale:'en',path:p.path,lastmod:p.updatedTime,contentOwner:p.contentOwner},{routeId:p.routeId,locale:'zh-CN',path:p.zhPath,lastmod:p.updatedTime,contentOwner:p.contentOwner}])},null,2)+'\n');
console.log(`P19 static Website: ${pages.length} route IDs / ${pages.length*2} canonical pages`);
