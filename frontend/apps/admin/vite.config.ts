import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

const apiTarget = process.env.GOJET_PLATFORMAPI_PROXY?.trim();
const proxy = apiTarget ? { '/api': { target: apiTarget, changeOrigin: false, xfwd: true } } : undefined;
const privateHeaders = { 'Cache-Control': 'no-store', 'X-Robots-Tag': 'noindex, nofollow' };

export default defineConfig({
  plugins: [react()],
  build: { outDir: 'dist', emptyOutDir: true, manifest: true, sourcemap: false },
  server: { ...(proxy ? { proxy } : {}), headers: privateHeaders },
  preview: { ...(proxy ? { proxy } : {}), headers: privateHeaders },
});
