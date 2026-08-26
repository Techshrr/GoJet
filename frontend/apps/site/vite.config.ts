import type { ServerResponse } from 'node:http';
import react from '@vitejs/plugin-react';
import { defineConfig, type Plugin } from 'vite';

const configuredApiTarget = process.env.GOJET_PLATFORMAPI_PROXY?.trim();
const devApiTarget = configuredApiTarget || 'http://127.0.0.1:8081';
const devProxy = { '/api': { target: devApiTarget, changeOrigin: false } };
const previewProxy = configuredApiTarget ? { '/api': { target: configuredApiTarget, changeOrigin: false } } : {};

const previewProviderFallback: Plugin = {
  name: 'gojet-preview-provider-fallback',
  configurePreviewServer(server) {
    if (configuredApiTarget) return;
    server.middlewares.use((request, response, next) => {
      const pathname = request.url?.split('?', 1)[0];
      if (request.method !== 'GET' || pathname !== '/api/public/auth/providers') {
        next();
        return;
      }
      response.statusCode = 200;
      response.setHeader('Content-Type', 'application/json; charset=utf-8');
      response.setHeader('Cache-Control', 'no-store');
      response.end(JSON.stringify({ providers: [] }));
    });
  },
};

function isPublicTrustPage(pathname: string | undefined) {
  return pathname === '/linkunavailable' || pathname === '/abuse/report';
}

const publicTrustHeaderValues: Readonly<Record<string, string>> = {
  'cache-control': 'no-store, max-age=0',
  pragma: 'no-cache',
  'x-robots-tag': 'noindex, nofollow, noarchive',
  'referrer-policy': 'no-referrer',
  'x-content-type-options': 'nosniff',
};

function preservePublicTrustHeaders(response: ServerResponse) {
  const setHeader = response.setHeader.bind(response);
  response.setHeader = ((name: string, value: string | number | readonly string[]) =>
    setHeader(name, publicTrustHeaderValues[name.toLowerCase()] ?? value)) as typeof response.setHeader;
  for (const [name, value] of Object.entries(publicTrustHeaderValues)) setHeader(name, value);
}

const publicTrustHeaders: Plugin = {
  name: 'gojet-public-trust-headers',
  configureServer(server) {
    server.middlewares.use((request, response, next) => {
      const pathname = request.url?.split('?', 1)[0];
      if (request.method === 'GET' && isPublicTrustPage(pathname)) preservePublicTrustHeaders(response);
      next();
    });
  },
  configurePreviewServer(server) {
    server.middlewares.use((request, response, next) => {
      const pathname = request.url?.split('?', 1)[0];
      if (request.method === 'GET' && isPublicTrustPage(pathname)) preservePublicTrustHeaders(response);
      next();
    });
  },
};

export default defineConfig({
  plugins: [react(), previewProviderFallback, publicTrustHeaders],
  build: { outDir: 'dist', emptyOutDir: true, manifest: true, sourcemap: false },
  server: { proxy: devProxy },
  preview: { proxy: previewProxy },
});
