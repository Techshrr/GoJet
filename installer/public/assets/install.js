(() => {
  'use strict';
  const form = document.querySelector('#installer-login');
  const token = document.querySelector('#bootstrap-token');
  const title = document.querySelector('#installer-title');
  const message = document.querySelector('#installer-message');
  const submit = document.querySelector('#installer-apply');
  const refresh = document.querySelector('#installer-refresh');
  const login = form.querySelector('button');
  const preflight = document.querySelector('#installer-preflight');
  const preflightSummary = document.querySelector('#preflight-summary');
  const preflightChecks = document.querySelector('#preflight-checks');
  const checkNames = {package: 'Release integrity', systemd: 'Service manager', nginx: 'Nginx',
    php: 'PHP', mysql: 'MySQL', redis: 'Redis', storage: 'File storage', clamav: 'ClamAV'};
  let executionExpiry = null;
  let expiry = null;
  let csrf = null;
  let busy = false;
  function clearExecution() { clearTimeout(executionExpiry); executionExpiry = null; }
  function clearPreflight() {
    clearTimeout(expiry); expiry = null;
    preflight.hidden = true; preflightChecks.hidden = true; preflightChecks.replaceChildren();
  }
  function showPreflight(value) {
    clearPreflight(); preflight.hidden = false;
    const unavailable = 'Checks unavailable. Ask your server administrator to refresh the checks, then select Refresh status.';
    preflightSummary.textContent = unavailable;
    if (!value || !['ready', 'failed', 'stale', 'unavailable'].includes(value.status)) return;
    if (value.status === 'stale') {
      preflightSummary.textContent = 'Checks expired. Ask your server administrator to refresh the checks, then select Refresh status.';
      return;
    }
    if (!['ready', 'failed'].includes(value.status)) return;
    const keys = Object.keys(checkNames);
    if (!value.checks || Object.keys(value.checks).length !== keys.length
      || keys.some(key => typeof value.checks[key] !== 'boolean') || !Number.isInteger(value.checked_at)
      || value.installation_complete !== false) return;
    const remaining = (value.checked_at + 120) * 1000 - Date.now();
    if (remaining <= 0 || remaining > 120000) { showPreflight({status: 'stale'}); return; }
    const passed = keys.every(key => value.checks[key]);
    if (value.ready !== passed || value.status !== (passed ? 'ready' : 'failed')) return;
    preflightSummary.textContent = (passed ? 'All dependency checks passed.' : 'Some dependency checks failed. Ask your server administrator to correct them.')
      + ' Checked at ' + new Date(value.checked_at * 1000).toLocaleTimeString() + '.';
    for (const key of keys) {
      const row = document.createElement('div');
      const name = document.createElement('dt'); name.textContent = checkNames[key];
      const result = document.createElement('dd'); result.textContent = value.checks[key] ? 'Passed' : 'Failed';
      row.append(name, result); preflightChecks.append(row);
    }
    preflightChecks.hidden = false;
    expiry = setTimeout(() => showPreflight({status: 'stale'}), remaining);
  }
  function showExecution(value) {
    clearExecution();
    const phases = {starting: 'Starting installation', data: 'Initializing data', services: 'Starting services', site: 'Publishing the site', health: 'Checking installation health'};
    const unknown = () => {
      title.textContent = 'Execution status unconfirmed';
      tell('No recent execution update is available. Refresh status or ask your server administrator to inspect the worker. Do not submit again.');
    };
    if (!value || value.status === 'unavailable') return;
    if (value.status === 'stale') { unknown(); return; }
    if (!Object.hasOwn(phases, value.phase) || !Number.isInteger(value.updated_at)
      || value.installation_complete !== false || !['running', 'failed'].includes(value.status)) { unknown(); return; }
    if (value.status === 'failed') {
      title.textContent = 'Installation needs attention';
      tell(phases[value.phase] + ' failed. Your server administrator must inspect the phase records and use the recovery procedure. Do not submit again.');
      return;
    }
    const remaining = (value.updated_at + 120) * 1000 - Date.now();
    if (remaining <= 0 || remaining > 120000) { unknown(); return; }
    title.textContent = phases[value.phase];
    tell('The server worker reported this phase in progress. Refresh status for the next update. Installation is not yet confirmed complete.');
    executionExpiry = setTimeout(unknown, remaining);
  }
  async function loadPreflight(state) {
    if (state?.step === 'services') {
      const result = await request('GET', '/install/services');
      if (result) {
        showPreflight(result.preflight);
        if (result.status === 'checking') showExecution(result.execution);
      }
    }
  }
  function tell(text) { message.textContent = text; }
  function controls() { login.disabled = busy; refresh.disabled = busy; submit.disabled = busy || !csrf; }
  async function request(method, path, body) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 15000);
    try {
      const response = await fetch(path, {method, credentials: 'same-origin', cache: 'no-store',
        redirect: 'error', headers: {'Accept': 'application/json', ...(body ? {'Content-Type': 'application/json'} : {})},
        ...(body ? {body: JSON.stringify(body)} : {}), signal: controller.signal});
      if (response.status === 404) {
        clearPreflight(); clearExecution(); csrf = null; form.hidden = true; submit.hidden = true; refresh.hidden = true;
        title.textContent = 'Installer unavailable';
        tell('The installer is closed or unavailable. Check the installation result with your server administrator before opening Admin.');
        return null;
      }
      if (!response.ok) {
        if ([401, 403].includes(response.status)) { csrf = null; form.hidden = false; }
        const errors = {401: 'Authentication required. Enter the installation access token again.',
          403: 'This request was refused. Authenticate again before submitting.',
          409: 'This step cannot be repeated. Refresh the installation status.',
          429: 'Too many authentication attempts. Wait at least one minute before trying again.',
          503: 'The installer is busy or unavailable. Refresh the status; do not resubmit an installation request.'};
        throw new Error(errors[response.status] || 'The request was refused. Refresh the status before continuing.');
      }
      return await response.json();
    } finally { clearTimeout(timer); }
  }
  function show(state) {
    clearPreflight(); clearExecution();
    if (!state) return;
    submit.hidden = true;
    if (state.step === 'services' && state.status === 'ready') {
      title.textContent = 'Approved plan ready';
      submit.hidden = false;
      if (!csrf) form.hidden = false;
      tell(csrf ? 'The server-approved installation plan is ready. Submit it once to queue installation.'
        : 'Authenticate to submit the server-approved installation plan.');
    } else if (state.step === 'services' && state.status === 'checking') {
      csrf = null;
      title.textContent = 'Installation requested';
      tell('Installation request submitted. Awaiting the server executor or its result. Refresh status to check whether the installer has closed. Do not submit again.');
    } else {
      csrf = null;
      title.textContent = 'Preparation required';
      tell('Interactive configuration is not available in this release. Your server administrator must prepare and approve the installation plan first.');
    }
  }
  async function action(work, isApply = false) {
    if (busy) return;
    clearPreflight(); clearExecution(); busy = true; controls();
    try { await work(); }
    catch (error) {
      if (error.name === 'AbortError' || error instanceof TypeError) {
        tell(isApply ? 'The submission result could not be confirmed. Refresh status before taking any further action; the request may already have been accepted.'
          : 'The server could not be reached. Check your connection, then refresh status.');
      } else { tell(error.message); }
    } finally { busy = false; controls(); }
  }
  form.addEventListener('submit', event => {
    event.preventDefault();
    if (!form.reportValidity()) return;
    const value = token.value; token.value = '';
    action(async () => {
      csrf = null;
      const state = await request('POST', '/install/', {bootstrap_token: value});
      if (state) { csrf = state.csrf; form.hidden = true; show(state); await loadPreflight(state); }
    });
  });
  submit.addEventListener('click', () => {
    if (!csrf || busy) return;
    const once = csrf; csrf = null;
    action(async () => { show(await request('POST', '/install/services', {csrf: once})); }, true);
  });
  refresh.addEventListener('click', () => action(async () => { const state = await request('GET', '/install/'); show(state); await loadPreflight(state); }));
  window.addEventListener('pagehide', () => { clearPreflight(); clearExecution(); csrf = null; token.value = ''; });
  controls();
})();
