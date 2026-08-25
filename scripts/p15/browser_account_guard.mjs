import { chromium } from 'playwright-core';

// P15-T025 asserts application state explicitly after every navigation. Waiting
// for Playwright's networkidle heuristic is not an authority signal and can hang
// on an otherwise-ready SPA, so normalize only that legacy wait mode to the
// deterministic DOM-ready boundary used before the explicit state assertions.
const launchedBrowsers = new Set();
const originalLaunch = chromium.launch.bind(chromium);
chromium.launch = async (...args) => {
  const browser = await originalLaunch(...args);
  launchedBrowsers.add(browser);
  browser.once('disconnected', () => launchedBrowsers.delete(browser));

  const originalNewContext = browser.newContext.bind(browser);
  browser.newContext = async (...contextArgs) => {
    const context = await originalNewContext(...contextArgs);
    const originalNewPage = context.newPage.bind(context);
    context.newPage = async (...pageArgs) => {
      const page = await originalNewPage(...pageArgs);
      const originalGoto = page.goto.bind(page);
      page.goto = (url, options = {}) => originalGoto(url, {
        ...options,
        waitUntil: options.waitUntil === 'networkidle' ? 'domcontentloaded' : options.waitUntil,
      });
      const originalReload = page.reload.bind(page);
      page.reload = (options = {}) => originalReload({
        ...options,
        waitUntil: options.waitUntil === 'networkidle' ? 'domcontentloaded' : options.waitUntil,
      });
      return page;
    };
    return context;
  };
  return browser;
};

try {
  await import('./browser_account.mjs');
} finally {
  // The underlying runner writes FAIL evidence in its catch path. Always closing
  // any still-live browser here guarantees that a real assertion failure exits
  // the Action promptly instead of idling until the job timeout.
  await Promise.allSettled([...launchedBrowsers].map((browser) => browser.close()));
}
