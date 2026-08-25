const caseId = process.argv[2] === '--case' ? process.argv[3] : '';

switch (caseId) {
  case 'P15-T024':
    await import('./browser_auth.mjs');
    break;
  case 'P15-T025':
    await import('./browser_account.mjs');
    break;
  default:
    throw new Error(`Unsupported P15 browser case: ${caseId || '<missing>'}`);
}
