import { chromium } from 'playwright-core';
import { executablePath, writeResult } from './browser_common.mjs';

const index = process.argv.indexOf('--case');
const caseId = index >= 0 ? process.argv[index + 1] : '';
const modules = {
  'P16-T026': './browser_026_authority.mjs',
  'P16-T027': './browser_027.mjs',
};

if (!(caseId in modules)) throw new Error('P16 browser authority supports P16-T026 and P16-T027 only');
const module = await import(modules[caseId]);
const browser = await chromium.launch({
  executablePath,
  headless: true,
  args: ['--no-sandbox', '--disable-dev-shm-usage'],
});

try {
  const details = await module.run(browser);
  writeResult(caseId, 'PASS', { ...details, frozen_contract_completion: true, closure_claim: false }, []);
  console.log(`${caseId}: PASS`);
} catch (error) {
  const message = error instanceof Error ? `${error.name}: ${error.message}` : String(error);
  writeResult(caseId, 'FAIL', { frozen_contract_completion: false, closure_claim: false }, [message]);
  console.error(`${caseId}: FAIL\n${message}`);
  process.exitCode = 1;
} finally {
  await browser.close();
}
