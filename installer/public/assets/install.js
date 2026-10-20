(() => {
  'use strict';
  const form = document.querySelector('#installer-login');
  const token = document.querySelector('#bootstrap-token');
  const message = document.querySelector('#installer-message');
  const submit = document.querySelector('#installer-apply');
  const refresh = document.querySelector('#installer-refresh');
  const login = form.querySelector('button');
  let csrf = null;
  let busy = false;
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
        csrf = null; form.hidden = true; submit.hidden = true; refresh.hidden = true;
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
    if (!state) return;
    submit.hidden = true;
    if (state.step === 'services' && state.status === 'ready') {
      submit.hidden = false;
      if (!csrf) form.hidden = false;
      tell(csrf ? 'The server-approved installation plan is ready. Submit it once to queue installation.'
        : 'Authenticate to submit the server-approved installation plan.');
    } else if (state.step === 'services' && state.status === 'checking') {
      csrf = null;
      tell('Installation request submitted. Awaiting the server executor or its result. Refresh status to check whether the installer has closed. Do not submit again.');
    } else {
      csrf = null;
      tell('Interactive configuration is not available in this release. Your server administrator must prepare and approve the installation plan first.');
    }
  }
  async function action(work, isApply = false) {
    if (busy) return;
    busy = true; controls();
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
      if (state) { csrf = state.csrf; form.hidden = true; show(state); }
    });
  });
  submit.addEventListener('click', () => {
    if (!csrf || busy) return;
    const once = csrf; csrf = null;
    action(async () => { show(await request('POST', '/install/services', {csrf: once})); }, true);
  });
  refresh.addEventListener('click', () => action(async () => { show(await request('GET', '/install/')); }));
  window.addEventListener('pagehide', () => { csrf = null; token.value = ''; });
  controls();
})();
