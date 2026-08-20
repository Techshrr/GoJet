import { useEffect, useState } from 'react';
import { TOKENS } from '@gojet/tokens';

export type ShellViewport = 'mobile' | 'tablet' | 'desktop';

const md = String(TOKENS.composite['breakpoint.md'].min_width);
const lg = String(TOKENS.composite['breakpoint.lg'].min_width);

function resolveViewport(): ShellViewport {
  if (typeof window === 'undefined') return 'desktop';
  if (window.matchMedia(`(width < ${md})`).matches) return 'mobile';
  if (window.matchMedia(`(width < ${lg})`).matches) return 'tablet';
  return 'desktop';
}

export function useShellViewport(): ShellViewport {
  const [viewport, setViewport] = useState<ShellViewport>(resolveViewport);

  useEffect(() => {
    const mdQuery = window.matchMedia(`(width < ${md})`);
    const lgQuery = window.matchMedia(`(width < ${lg})`);
    const update = () => setViewport(resolveViewport());

    mdQuery.addEventListener('change', update);
    lgQuery.addEventListener('change', update);
    update();

    return () => {
      mdQuery.removeEventListener('change', update);
      lgQuery.removeEventListener('change', update);
    };
  }, []);

  return viewport;
}
