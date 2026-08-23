import { spawnSync } from 'node:child_process';

const index = process.argv.indexOf('--case');
const caseId = index >= 0 ? process.argv[index + 1] : '';
if (!/^P12-T0(19|20|21|22|23)$/.test(caseId)) throw new Error('case must be P12-T019..P12-T023');

const scripts = [];
if (caseId === 'P12-T019') scripts.push('scripts/p12/browser_t019.mjs');
else {
  if (caseId === 'P12-T020') scripts.push('scripts/p12/browser_t020_probe.mjs');
  scripts.push('scripts/p12/browser_base.mjs');
}
scripts.push('scripts/p12/browser_contract.mjs');

for (const script of scripts) {
  const run = spawnSync(process.execPath, [script, '--case', caseId], { cwd: process.cwd(), env: process.env, stdio: 'inherit' });
  if (run.error) throw run.error;
  if (run.status !== 0) process.exit(run.status ?? 1);
}
