import { spawnSync } from 'node:child_process';

const index = process.argv.indexOf('--case');
const caseId = index >= 0 ? process.argv[index + 1] : '';
if (!/^P12-T0(19|20|21|22|23)$/.test(caseId)) throw new Error('case must be P12-T019..P12-T023');

const baseScript = caseId === 'P12-T019'
  ? 'scripts/p12/browser_t019.mjs'
  : caseId === 'P12-T022'
    ? 'scripts/p12/browser_t022.mjs'
    : 'scripts/p12/browser_base.mjs';

for (const script of [baseScript, 'scripts/p12/browser_contract.mjs']) {
  const run = spawnSync(process.execPath, [script, '--case', caseId], { cwd: process.cwd(), env: process.env, stdio: 'inherit' });
  if (run.error) throw run.error;
  if (run.status !== 0) process.exit(run.status ?? 1);
}
