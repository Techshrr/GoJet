import { chromium } from 'playwright-core';
import { executablePath, writeResult } from './browser_common.mjs';

const index = process.argv.indexOf('--case');
const caseId = index >= 0 ? process.argv[index + 1] : '';
if (!/^P13-T0(21|22|23|24|25)$/.test(caseId)) throw new Error('case must be P13-T021..P13-T025');
const module = await import(`./browser_${caseId.slice(-3)}.mjs`);
const browser = await chromium.launch({ executablePath, headless: true, args: ['--no-sandbox', '--disable-dev-shm-usage'] });
try {
  const details = await module.run(browser);
  writeResult(caseId, 'PASS', { ...details, frozen_contract_completion: true }, []);
  console.log(`${caseId}: PASS`);
} catch (error) {
  const message = error instanceof Error ? `${error.name}: ${error.message}` : String(error);
  writeResult(caseId, 'FAIL', { frozen_contract_completion: false }, [message]);
  console.error(`${caseId}: FAIL\n${message}`);
  process.exitCode = 1;
} finally {
  await browser.close();
}
