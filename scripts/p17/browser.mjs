import { chromium } from 'playwright-core';
import { executablePath, writeResult } from './browser_common.mjs';
import { run as run030 } from './browser_030.mjs';
import { run as run031 } from './browser_031.mjs';
import { run as run032 } from './browser_032.mjs';
import { run as run033 } from './browser_033.mjs';

const index = process.argv.indexOf('--case');
const caseId = index >= 0 ? process.argv[index + 1] : '';
const cases = {
  'P17-T030': run030,
  'P17-T031': run031,
  'P17-T032': run032,
  'P17-T033': run033,
};

if (!cases[caseId]) {
  console.error(`unsupported P17 browser case: ${caseId || '<missing>'}`);
  process.exit(2);
}

const browser = await chromium.launch({
  executablePath,
  headless: true,
  args: ['--no-sandbox', '--disable-dev-shm-usage'],
});

try {
  const result = await cases[caseId](browser);
  writeResult(caseId, 'PASS', result.checks, result.captures, result.details, []);
  console.log(JSON.stringify({ case: caseId, status: 'PASS' }));
} catch (error) {
  const message = error instanceof Error ? error.message : String(error);
  try { writeResult(caseId, 'FAIL', {}, [], { frozen_contract_completion: false, closure_claim: false }, [message]); } catch {}
  console.error(message);
  process.exitCode = 1;
} finally {
  await browser.close();
}
