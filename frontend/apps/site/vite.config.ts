import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

const configuredApiTarget = process.env.GOJET_PLATFORMAPI_PROXY?.trim();
const devApiTarget = configuredApiTarget || 'http://127.0.0.1:8081';
const devProxy = { '/api': { target: devApiTarget, changeOrigin: false } };
const previewProxy = configuredApiTarget ? { '/api': { target: configuredApiTarget, changeOrigin: false } } : undefined;

export default defineConfig({
  plugins: [react()],
  build: { outDir: 'dist', emptyOutDir: true, manifest: true, sourcemap: false },
  server: { proxy: devProxy },
  preview: previewProxy ? { proxy: previewProxy } : {},
});
