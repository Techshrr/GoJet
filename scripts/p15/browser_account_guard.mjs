import { spawn } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const root = process.cwd();
const evidencePath = resolve(root, 'artifacts/v10/P15/browser/P15-T025.json');
const childPath = resolve(root, 'scripts/p15/browser_account.mjs');
const child = spawn(process.execPath, [childPath, '--case', 'P15-T025'], {
  cwd: root,
  env: process.env,
  stdio: 'inherit',
});

let failureObserved = false;
const watcher = setInterval(() => {
  if (!existsSync(evidencePath)) return;
  try {
    const evidence = JSON.parse(readFileSync(evidencePath, 'utf8'));
    if (evidence.status === 'FAIL' && child.exitCode === null && !failureObserved) {
      failureObserved = true;
      console.error('P15-T025 emitted FAIL evidence; terminating the browser runner so CI can surface the defect.');
      child.kill('SIGTERM');
    }
  } catch {
    // The writer may be between create/truncate and the final JSON flush. Retry.
  }
}, 250);

const timeout = setTimeout(() => {
  if (child.exitCode === null) {
    failureObserved = true;
    console.error('P15-T025 browser runner exceeded the five-minute execution guard.');
    child.kill('SIGTERM');
  }
}, 5 * 60 * 1000);

const result = await new Promise((resolveResult) => {
  child.once('exit', (code, signal) => resolveResult({ code, signal }));
  child.once('error', (error) => resolveResult({ code: 1, signal: null, error }));
});
clearInterval(watcher);
clearTimeout(timeout);

if (result.error) throw result.error;
if (result.code !== 0) {
  throw new Error(`P15-T025 browser runner failed (code=${String(result.code)}, signal=${String(result.signal)}, fail_evidence=${String(failureObserved)})`);
}
