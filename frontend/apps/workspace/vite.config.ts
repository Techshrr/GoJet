import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

const apiTarget = process.env.GOJET_PLATFORMAPI_PROXY?.trim() || 'http://127.0.0.1:8081';
const proxy = {
  '/api': { target: apiTarget, changeOrigin: false, xfwd: true },
  '/f/': { target: apiTarget, changeOrigin: false, xfwd: true },
};
const privateHeaders = { 'Cache-Control': 'no-store', 'X-Robots-Tag': 'noindex, nofollow' };

export default defineConfig({
  plugins: [react()],
  build: { outDir: 'dist', emptyOutDir: true, manifest: true, sourcemap: false },
  server: { proxy, headers: privateHeaders },
  preview: { proxy, headers: privateHeaders },
});
