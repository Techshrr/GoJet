import { chromium } from 'playwright-core';
import { CASES, HEAD, executablePath } from './browser_env.mjs';
import { writeResult } from './browser_ui.mjs';
import { caseT021, caseT022, caseT023, caseT024, caseT025 } from './browser_cases.mjs';

const caseFunctions = {
  'P09-T021': caseT021,
  'P09-T022': caseT022,
  'P09-T023': caseT023,
  'P09-T024': caseT024,
  'P09-T025': caseT025,
};

async function runCase(browser, caseId) {
  let details = {};
  const errors = [];
  try {
    details = await caseFunctions[caseId](browser);
  } catch (error) {
    errors.push(error instanceof Error ? `${error.name}: ${error.message}` : String(error));
  }
  writeResult(caseId, errors.length ? 'FAIL' : 'PASS', details, errors);
  if (errors.length) throw new Error(`${caseId}: ${errors.join('; ')}`);
  console.log(`${caseId} PASS on ${HEAD}`);
}

async function main() {
  const index = process.argv.indexOf('--case');
  const requested = index >= 0 ? process.argv[index + 1] : 'all';
  if (requested !== 'all' && !CASES.has(requested)) throw new Error(`Unsupported P09 browser case: ${requested}`);
  const browser = await chromium.launch({ executablePath, headless: true, args: ['--no-sandbox', '--disable-dev-shm-usage'] });
  try {
    const run = requested === 'all' ? [...CASES] : [requested];
    for (const caseId of run) await runCase(browser, caseId);
  } finally {
    await browser.close();
  }
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
