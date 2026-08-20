export type ThemePreference = 'light' | 'dark' | 'system';
export type ResolvedTheme = 'light' | 'dark';

const STORAGE_KEY = 'gojet.theme';
const DARK_QUERY = '(prefers-color-scheme: dark)';
const REDUCED_MOTION_QUERY = '(prefers-reduced-motion: reduce)';

export function resolveTheme(preference: ThemePreference, prefersDark: boolean): ResolvedTheme {
  if (preference === 'system') return prefersDark ? 'dark' : 'light';
  return preference;
}

export function applyTheme(preference: ThemePreference): ResolvedTheme {
  if (typeof window === 'undefined' || typeof document === 'undefined') return preference === 'dark' ? 'dark' : 'light';
  const resolved = resolveTheme(preference, window.matchMedia(DARK_QUERY).matches);
  document.documentElement.dataset.theme = resolved;
  document.documentElement.dataset.themePreference = preference;
  return resolved;
}

export function readThemePreference(fallback: ThemePreference): ThemePreference {
  if (typeof window === 'undefined') return fallback;
  const stored = window.localStorage.getItem(STORAGE_KEY);
  return stored === 'light' || stored === 'dark' || stored === 'system' ? stored : fallback;
}

export function setThemePreference(preference: ThemePreference) {
  if (typeof window !== 'undefined') window.localStorage.setItem(STORAGE_KEY, preference);
  return applyTheme(preference);
}

export function subscribeSystemTheme(preference: ThemePreference, listener: (theme: ResolvedTheme) => void) {
  if (typeof window === 'undefined') return () => undefined;
  const media = window.matchMedia(DARK_QUERY);
  const handler = () => {
    if (preference === 'system') listener(applyTheme('system'));
  };
  media.addEventListener('change', handler);
  return () => media.removeEventListener('change', handler);
}

export function prefersReducedMotion() {
  return typeof window !== 'undefined' && window.matchMedia(REDUCED_MOTION_QUERY).matches;
}

export function subscribeReducedMotion(listener: (reduced: boolean) => void) {
  if (typeof window === 'undefined') return () => undefined;
  const media = window.matchMedia(REDUCED_MOTION_QUERY);
  const handler = () => listener(media.matches);
  media.addEventListener('change', handler);
  return () => media.removeEventListener('change', handler);
}

export function themeBootstrapScript(defaultPreference: ThemePreference) {
  const fallback = JSON.stringify(defaultPreference);
  return `(()=>{try{const k='${STORAGE_KEY}',s=localStorage.getItem(k),p=(s==='light'||s==='dark'||s==='system')?s:${fallback},d=p==='system'?(matchMedia('${DARK_QUERY}').matches?'dark':'light'):p;document.documentElement.dataset.theme=d;document.documentElement.dataset.themePreference=p}catch{document.documentElement.dataset.theme=${fallback}==='dark'?'dark':'light'}})();`;
}
