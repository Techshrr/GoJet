import { useEffect } from 'react';

export function usePrivatePublicPage(title: string) {
  useEffect(() => {
    const previousTitle = document.title;
    document.title = title;

    const robots = ensureMeta('robots');
    const previousRobots = robots.getAttribute('content');
    robots.setAttribute('content', 'noindex,nofollow,noarchive');

    const referrer = ensureMeta('referrer');
    const previousReferrer = referrer.getAttribute('content');
    referrer.setAttribute('content', 'no-referrer');

    return () => {
      document.title = previousTitle;
      restoreMeta(robots, previousRobots);
      restoreMeta(referrer, previousReferrer);
    };
  }, [title]);
}

function ensureMeta(name: string): HTMLMetaElement {
  const existing = document.head.querySelector<HTMLMetaElement>(`meta[name="${name}"]`);
  if (existing) return existing;
  const meta = document.createElement('meta');
  meta.setAttribute('name', name);
  meta.dataset.gojetRuntimeMeta = 'true';
  document.head.appendChild(meta);
  return meta;
}

function restoreMeta(meta: HTMLMetaElement, previous: string | null) {
  if (previous !== null) {
    meta.setAttribute('content', previous);
    return;
  }
  if (meta.dataset.gojetRuntimeMeta === 'true') meta.remove();
  else meta.removeAttribute('content');
}
