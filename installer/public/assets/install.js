(() => {
  'use strict';
  const configuration = document.querySelector('#installer-configuration');
  const saveConfiguration = document.querySelector('#installer-save-configuration');
  const form = document.querySelector('#installer-login');
  const token = document.querySelector('#bootstrap-token');
  const title = document.querySelector('#installer-title');
  const message = document.querySelector('#installer-message');
  const submit = document.querySelector('#installer-apply');
  const approve = document.querySelector('#installer-approve');
  const refresh = document.querySelector('#installer-refresh');
  const login = form.querySelector('button');
  const preflight = document.querySelector('#installer-preflight');
  const preflightSummary = document.querySelector('#preflight-summary');
  const preflightChecks = document.querySelector('#preflight-checks');
  const checkNames = {package: 'Release integrity', systemd: 'Service manager', nginx: 'Nginx',
    php: 'PHP', mysql: 'MySQL', redis: 'Redis', storage: 'File storage', clamav: 'ClamAV'};
  const openAdmin = document.querySelector('#installer-open-admin');
  const openSite = document.querySelector('#installer-open-site');
  let resultId = null;
  let entryClosed = false;
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
        resultId = typeof result.result_id === 'string' && /^[a-f0-9]{64}$/.test(result.result_id) ? result.result_id : null;
        showPreflight(result.preflight);
        if (result.status === 'checking') showExecution(result.execution);
      }
    }
  }
  function tell(text) { message.textContent = text; }
  function controls() { login.disabled = busy; refresh.disabled = busy; submit.disabled = busy || !csrf; approve.disabled = busy || !csrf; saveConfiguration.disabled = busy || !csrf; }
  async function confirmResult() {
    clearPreflight(); clearExecution(); csrf = null;
    configuration.reset(); configuration.hidden = true; form.hidden = true; submit.hidden = true; approve.hidden = true;
    openAdmin.hidden = true; openSite.hidden = true;
    if (!resultId) {
      refresh.hidden = true;
      title.textContent = 'Installer unavailable';
      tell('The installer is closed or unavailable. Check the installation result with your server administrator before opening Admin.');
      return;
    }
    entryClosed = true; refresh.hidden = false;
    refresh.textContent = 'Check installation result';
    title.textContent = 'Installation result unconfirmed';
    tell('The installation entry has closed, but completion has not yet been confirmed. Check the result again. If it remains unavailable, ask your server administrator to inspect the installation. Do not submit again.');
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 10000);
    try {
      // No credentials, token, identifier in the URL, redirect, or cross-origin request.
      const response = await fetch('/.well-known/gojet-installation-result', {method: 'GET', credentials: 'omit',
        cache: 'no-store', redirect: 'error', headers: {'Accept': 'application/json'}, signal: controller.signal});
      if (response.status !== 200 || !/^application\/json(?:;|$)/i.test(response.headers.get('content-type') || '')) return;
      const reader = response.body.getReader(); let raw = ''; let size = 0;
      const decoder = new TextDecoder('utf-8', {fatal: true});
      try {
        while (true) {
          const {done, value} = await reader.read();
          if (done) break;
          size += value.byteLength;
          if (size > 1024) { await reader.cancel(); return; }
          raw += decoder.decode(value, {stream: true});
        }
        raw += decoder.decode();
      } finally { reader.releaseLock(); }
      const value = JSON.parse(raw);
      if (!value || Object.keys(value).length !== 3 || value.schema !== 'gojet.installation-result.v1'
        || value.result_id !== resultId || value.installation_complete !== true) return;
      title.textContent = 'Installation completed';
      tell('The server confirmed completion of your approved installation after its health checks. You can now open Admin or the site.');
      openAdmin.hidden = false; openSite.hidden = false; refresh.hidden = true;
    } catch { /* A missing, invalid, offline or unrelated receipt never means success. */ }
    finally { clearTimeout(timer); }
  }
  async function request(method, path, body) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 15000);
    try {
      const response = await fetch(path, {method, credentials: 'same-origin', cache: 'no-store',
        redirect: 'error', headers: {'Accept': 'application/json', ...(body ? {'Content-Type': 'application/json'} : {})},
        ...(body ? {body: JSON.stringify(body)} : {}), signal: controller.signal});
      if (response.status === 404) {
        await confirmResult();
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
    submit.hidden = true; approve.hidden = true; configuration.hidden = true;
    if (state.step === 'services' && state.status === 'ready') {
      title.textContent = 'Approved plan ready';
      submit.hidden = false;
      if (!csrf) form.hidden = false;
      tell(csrf ? 'The server-approved installation plan is ready. Submit it once to queue installation.'
        : 'Authenticate to submit the server-approved installation plan.');
    } else if (state.step === 'services' && state.status === 'checking') {
      csrf = null;
      form.hidden = true;
      title.textContent = 'Installation requested';
      tell('Installation request submitted. Awaiting the server executor or its result. Refresh status to check whether the installer has closed. Do not submit again.');
    } else if (state.step === 'environment' && state.status === 'ready') {
      configuration.hidden = !csrf;
      form.hidden = Boolean(csrf);
      title.textContent = 'Configure installation';
      tell(csrf ? 'Enter and review the settings below. Saving sends them for server validation and approval; it does not start installation.'
        : 'Authenticate to enter installation settings. Saved passwords are never returned to this page.');
    } else if (state.step === 'environment' && state.status === 'checking') {
      approve.hidden = false;
      form.hidden = Boolean(csrf);
      title.textContent = 'Configuration submitted';
      tell(csrf ? 'Your configuration is saved. Ask your server administrator to validate and approve it, then confirm server approval below. Installation has not started.'
        : 'Your configuration is saved. After your server administrator approves it, authenticate again to confirm approval. Installation has not started.');
    } else {
      csrf = null;
      title.textContent = 'Preparation required';
      tell('This installation state requires attention from your server administrator. Refresh status before continuing.');
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
      csrf = null; resultId = null; entryClosed = false;
      const state = await request('POST', '/install/', {bootstrap_token: value});
      if (state) { csrf = state.csrf; form.hidden = true; show(state); await loadPreflight(state); }
    });
  });
  function draftFromForm() {
    const draft = {configuration: {schema: 'gojet.native-configuration.v1'}, admin: {}};
    const encoder = new TextEncoder();
    function invalid(input, message) {
      input.setCustomValidity(message); input.reportValidity(); input.focus();
      throw new Error(message);
    }
    for (const input of configuration.querySelectorAll('[data-config-path]')) {
      const parts = input.dataset.configPath.split('.');
      let target = parts[0] === 'admin' ? draft : draft.configuration;
      for (const part of parts.slice(0, -1)) target = target[part] ??= {};
      const value = input.type === 'number' ? input.valueAsNumber : input.value;
      if (input.type === 'number' && !Number.isSafeInteger(value)) invalid(input, 'Enter a whole number within the displayed limits.');
      if (input.type !== 'number' && (encoder.encode(value).length > input.maxLength || /[\x00-\x1f\x7f]/.test(value))) {
        invalid(input, 'This value exceeds the byte limit or contains unsupported control characters.');
      }
      target[parts.at(-1)] = value;
    }
    const c = draft.configuration;
    function check(path, valid, message) { if (!valid) invalid(document.querySelector('#cfg-'+path.replaceAll('.', '-')), message); }
    const host = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*$/;
    check('origin', /^https:\/\/[a-z0-9.-]+(?::[1-9][0-9]{0,4})?$/.test(c.origin), 'Enter an HTTPS hostname without a path or trailing slash.');
    const origin = new URL(c.origin);
    check('origin', host.test(origin.hostname) && !/^[0-9.]+$/.test(origin.hostname), 'Use the final DNS hostname, not an IP address.');
    for (const key of ['name', 'user']) check('database.'+key, /^[A-Za-z0-9_]{1,64}$/.test(c.database[key]), 'Use letters, numbers and underscores for the database identity.');
    check('redis.port', c.redis.port !== c.database.port, 'MySQL and Redis must use different ports.');
    check('files.max_bytes', c.files.max_bytes >= c.files.max_upload_bytes, 'Workspace storage must be at least the maximum upload size.');
    check('smtp.host', host.test(c.smtp.host), 'Enter a lowercase SMTP hostname.');
    check('smtp.username', (c.smtp.username === '') === (c.smtp.password === '') && c.smtp.username === c.smtp.username.trim(), 'Enter both SMTP username and password, or leave both empty.');
    for (const key of ['name', 'policy_version']) check('risk.'+key, /^[A-Za-z0-9_.-]{1,64}$/.test(c.risk[key]), 'Use letters, numbers, dots, underscores or hyphens.');
    let risk;
    try { risk = new URL(c.risk.endpoint); } catch { check('risk.endpoint', false, 'Enter a valid HTTPS risk endpoint.'); }
    check('risk.endpoint', risk.protocol === 'https:' && host.test(risk.hostname) && !risk.username && !risk.password && !risk.search && !risk.hash, 'Use an HTTPS endpoint without credentials, a query or a fragment.');
    check('turnstile_secret', c.turnstile_secret.trim() === c.turnstile_secret, 'Remove surrounding spaces from the Turnstile key.');
    for (const key of ['email', 'display_name', 'password']) check('admin.'+key, draft.admin[key].trim() !== '', 'Enter a nonempty administrator value.');
    const policy = document.querySelector('#cfg-file-types');
    const types = Object.create(null);
    const lines = policy.value.trim().split(/\r?\n/);
    if (lines.length > 64) invalid(policy, 'Use at most 64 file extensions.');
    for (const line of lines) {
      const parts = line.trim().split('=');
      const ext = parts[0]; const mimes = (parts[1] || '').split('|');
      if (parts.length !== 2 || !/^[a-z0-9]{1,16}$/.test(ext) || Object.hasOwn(types, ext)
        || mimes.length > 8 || mimes.some(mime => !/^[a-z0-9][a-z0-9.+-]{0,63}\/[a-z0-9][a-z0-9.+-]{0,127}$/.test(mime))) {
        invalid(policy, 'Use a unique extension=MIME entry on each line; separate multiple MIME types with |.');
      }
      types[ext] = mimes;
    }
    c.files.types = types;
    if (encoder.encode(JSON.stringify(draft)).length + 1 > 24576) throw new Error('The combined configuration exceeds 24 KiB. Shorten the settings before saving.');
    return draft;
  }
  configuration.addEventListener('input', event => event.target.setCustomValidity?.(''));
  configuration.addEventListener('submit', event => {
    event.preventDefault();
    if (busy || !csrf || !configuration.reportValidity()) return;
    let draft;
    try { draft = draftFromForm(); } catch (error) { tell(error.message); return; }
    const once = csrf; csrf = null;
    // Clear secret controls immediately. Never retry an uncertain POST automatically.
    for (const input of configuration.querySelectorAll('input[type=password]')) input.value = '';
    configuration.hidden = true;
    action(async () => {
      try {
        const state = await request('POST', '/install/configuration', {csrf: once, draft});
        if (state) { configuration.reset(); show(state); }
      } catch (error) { form.hidden = false; throw error; }
      finally { draft = null; }
    }, true);
  });
  submit.addEventListener('click', () => {
    if (!csrf || busy) return;
    const once = csrf; csrf = null;
    action(async () => { show(await request('POST', '/install/services', {csrf: once})); }, true);
  });
  approve.addEventListener('click', () => {
    if (!csrf || busy) return;
    const once = csrf; csrf = null;
    action(async () => {
      let state;
      try { state = await request('POST', '/install/configuration/approve', {csrf: once}); }
      catch (error) {
        form.hidden = false;
        if (error.name === 'AbortError' || error instanceof TypeError) throw error;
        throw new Error('Approval could not be confirmed. Refresh status first. If configuration is still pending, ask your server administrator to check approval, then authenticate again.');
      }
      if (state) { csrf = state.csrf; show(state); await loadPreflight(state); }
    }, true);
  });
  refresh.addEventListener('click', () => action(async () => {
    if (entryClosed) { await confirmResult(); return; }
    const state = await request('GET', '/install/'); show(state); await loadPreflight(state);
  }));
  window.addEventListener('pagehide', () => { clearPreflight(); clearExecution(); csrf = null; token.value = ''; configuration.reset(); configuration.hidden = true; controls(); });
  controls();
})();
