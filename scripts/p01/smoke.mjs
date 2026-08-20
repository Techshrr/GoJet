import { existsSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const root = process.cwd();
const required = [
  'frontend/apps/site/dist/index.html',
  'frontend/apps/site/dist/.vite/manifest.json',
  'frontend/apps/workspace/dist/index.html',
  'frontend/apps/workspace/dist/.vite/manifest.json',
  'frontend/apps/admin/dist/index.html',
  'frontend/apps/admin/dist/.vite/manifest.json',
  'frontend/apps/docs/dist/en/index.html',
  'frontend/apps/docs/dist/zh-CN/index.html',
];
const missing = required.filter((path) => !existsSync(resolve(root, path)));
if (missing.length > 0) {
  console.error(`Missing static build outputs:\n${missing.join('\n')}`);
  process.exit(1);
}
for (const app of ['site', 'workspace', 'admin']) {
  const manifestPath = resolve(root, `frontend/apps/${app}/dist/.vite/manifest.json`);
  const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'));
  const hasDynamicEntry = Object.values(manifest).some((entry) => entry && typeof entry === 'object' && entry.isDynamicEntry === true);
  if (!hasDynamicEntry) {
    console.error(`${app} build has no dynamic route chunk; P01 code-splitting contract failed.`);
    process.exit(1);
  }
}
console.log('P01 static-output smoke: PASS');
