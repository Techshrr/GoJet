import { execFileSync, spawn, spawnSync } from 'node:child_process';
import { chmodSync, existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';

export const root = process.cwd();
export const browserDir = `${root}/artifacts/v10/P09/browser`;
export const capturesDir = `${root}/artifacts/v10/P09/captures`;
for (const path of [browserDir, capturesDir]) mkdirSync(path, { recursive: true });

export const WORKSPACE_URL = (process.env.GOJET_TEST_WORKSPACE_URL ?? 'http://127.0.0.1:4174').replace(/\/$/, '');
export const ADMIN_URL = (process.env.GOJET_TEST_ADMIN_URL ?? 'http://127.0.0.1:4175').replace(/\/$/, '');
export const INSTALL_URL = (process.env.GOJET_TEST_INSTALL_URL ?? 'http://127.0.0.1:4176').replace(/\/$/, '');
export const INSTALL_FAULT_URL = (process.env.GOJET_TEST_INSTALL_FAULT_URL ?? 'http://127.0.0.1:4177').replace(/\/$/, '');
export const PLATFORM_URL = (process.env.GOJET_TEST_PLATFORM_URL ?? 'http://127.0.0.1:18081').replace(/\/$/, '');
export const WORKSPACE = process.env.GOJET_TEST_WORKSPACE_ID ?? 'ws-p09-browser';
export const ACTOR = process.env.GOJET_TEST_ACTOR_ID ?? 'p09-browser-owner';
export const MYSQL_HOST = process.env.GOJET_TEST_MYSQL_HOST ?? '127.0.0.1';
export const MYSQL_PORT = process.env.GOJET_TEST_MYSQL_PORT ?? '3306';
export const MYSQL_USER = process.env.GOJET_TEST_MYSQL_USER ?? 'root';
export const MYSQL_PASSWORD = process.env.GOJET_TEST_MYSQL_PASSWORD ?? 'root';
export const MYSQL_DATABASE = process.env.GOJET_TEST_MYSQL_DATABASE ?? 'gojet_test';
export const STORAGE_ROOT = process.env.GOJET_FILE_STORAGE_ROOT ?? '/tmp/gojet-p09-browser/storage';
export const WORKER = process.env.GOJET_P09_FILEWORKER ?? '/tmp/gojet-p09-browser/fileworker';
export const REAL_CLAMD = process.env.GOJET_P09_REAL_CLAMD_ADDRESS ?? '127.0.0.1:3310';
export const FAULT_SERVER = process.env.GOJET_P09_FAULT_SERVER ?? 'scripts/p09/clamd_fault_server.py';
export const HEAD = process.env.GITHUB_SHA || execFileSync('git', ['rev-parse', 'HEAD'], { encoding: 'utf8' }).trim();
export const BENIGN = Buffer.from('GoJet P09 route-backed browser clean fixture.\n');
export const EICAR = Buffer.from('X5O!P%@AP[4\\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*');
export const CASES = new Set(['P09-T021', 'P09-T022', 'P09-T023', 'P09-T024', 'P09-T025']);

export const variables = JSON.parse(readFileSync(`${root}/frontend/packages/tokens/generated/design-variables.json`, 'utf8')).tokens.composite;
export function parseViewport(value, name) {
  const match = /^(\d+)×(\d+)$/.exec(String(value));
  if (!match) throw new Error(`Invalid viewport ${name}: ${String(value)}`);
  return { width: Number(match[1]), height: Number(match[2]) };
}
export const viewports = {
  desktop: parseViewport(variables['viewport.desktop'].dimensions, 'viewport.desktop'),
  tablet: parseViewport(variables['viewport.tablet'].dimensions, 'viewport.tablet'),
  mobile: parseViewport(variables['viewport.mobile'].dimensions, 'viewport.mobile'),
};
export const expectedViewports = {
  desktop: { width: 1440, height: 900 },
  tablet: { width: 1024, height: 768 },
  mobile: { width: 390, height: 844 },
};
for (const [name, expected] of Object.entries(expectedViewports)) {
  const actual = viewports[name];
  if (actual.width !== expected.width || actual.height !== expected.height) {
    throw new Error(`P09 canonical ${name} viewport drift: ${JSON.stringify(actual)}`);
  }
}

export const chromeCandidates = [
  process.env.CHROME_BIN,
  '/usr/bin/google-chrome',
  '/usr/bin/google-chrome-stable',
  '/usr/bin/chromium',
].filter(Boolean);
export const executablePath = chromeCandidates.find((candidate) => existsSync(candidate));
if (!executablePath) throw new Error('System Chrome/Chromium is required for P09 browser evidence');

export function assert(condition, message) {
  if (!condition) throw new Error(message);
}
export function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
export function sqlLiteral(value) {
  return `'${String(value).replaceAll("'", "''")}'`;
}
export function mysqlArgs(sql) {
  return ['--protocol=tcp', '-h', MYSQL_HOST, '-P', String(MYSQL_PORT), '-u', MYSQL_USER, '-N', '-B', MYSQL_DATABASE, '-e', sql];
}
export function mysql(sql) {
  return execFileSync('mysql', mysqlArgs(sql), {
    cwd: root,
    encoding: 'utf8',
    env: { ...process.env, MYSQL_PWD: MYSQL_PASSWORD },
  }).trim();
}
export function resetFiles() {
  mysql(`SET FOREIGN_KEY_CHECKS=0;
TRUNCATE TABLE file_audit_events;
TRUNCATE TABLE file_scan_attempts;
TRUNCATE TABLE files;
TRUNCATE TABLE file_workspace_counters;
SET FOREIGN_KEY_CHECKS=1;`);
  for (const name of ['quarantine', 'published']) {
    const path = `${STORAGE_ROOT}/${name}`;
    rmSync(path, { recursive: true, force: true });
    mkdirSync(path, { recursive: true, mode: 0o700 });
    chmodSync(path, 0o700);
  }
  chmodSync(STORAGE_ROOT, 0o700);
}
export function authHeaders() {
  return {
    Accept: 'application/json',
    'X-GoJet-Test-Actor': ACTOR,
    'X-GoJet-Test-Workspace': WORKSPACE,
    'X-GoJet-Test-Workspace-Role': 'owner',
  };
}
export async function jsonApi(path, init = {}) {
  const headers = { ...authHeaders(), ...(init.headers ?? {}) };
  if (typeof init.body === 'string' && !headers['Content-Type']) headers['Content-Type'] = 'application/json';
  const response = await fetch(`${PLATFORM_URL}${path}`, { ...init, headers });
  let body = null;
  if (response.status !== 204) {
    const type = response.headers.get('content-type') ?? '';
    body = type.includes('application/json') ? await response.json() : await response.text();
  }
  return { response, body };
}
export async function uploadApi(filename, payload = BENIGN, contentType = 'text/plain') {
  const form = new FormData();
  form.append('change_reason', 'P09 browser fixture');
  form.append('file', new File([payload], filename, { type: contentType }), filename);
  const result = await jsonApi(`/api/workspaces/${encodeURIComponent(WORKSPACE)}/files`, { method: 'POST', body: form });
  assert(result.response.status === 201, `upload ${filename} failed: ${result.response.status} ${JSON.stringify(result.body)}`);
  return result.body;
}
export async function patchPolicy(id, payload) {
  const result = await jsonApi(`/api/workspaces/${encodeURIComponent(WORKSPACE)}/files/${id}`, {
    method: 'PATCH',
    body: JSON.stringify({ ...payload, change_reason: 'P09 browser policy fixture' }),
  });
  assert(result.response.status === 200, `patch file ${id} failed: ${result.response.status} ${JSON.stringify(result.body)}`);
  return result.body;
}
export async function action(id, name) {
  const result = await jsonApi(`/api/workspaces/${encodeURIComponent(WORKSPACE)}/files/${id}/${name}`, {
    method: 'POST',
    body: JSON.stringify({ change_reason: `P09 browser ${name}` }),
  });
  assert(result.response.status === 200, `${name} file ${id} failed: ${result.response.status} ${JSON.stringify(result.body)}`);
  return result.body;
}
export function workerEnv(address = REAL_CLAMD, overrides = {}) {
  return {
    ...process.env,
    GOJET_CLAMAV_NETWORK: 'tcp',
    GOJET_CLAMAV_ADDRESS: address,
    GOJET_CLAMAV_DIAL_TIMEOUT: '500ms',
    GOJET_CLAMAV_SCAN_TIMEOUT: '15s',
    GOJET_CLAMAV_MAX_SIGNATURE_AGE: '72h',
    GOJET_FILE_SCAN_CLAIM_LEASE: '2s',
    GOJET_FILE_WORKER_POLL_INTERVAL: '50ms',
    GOJET_FILE_WORKER_MAX_JOBS: '1',
    GOJET_FILE_WORKER_ID: `p09-browser-${Math.random().toString(16).slice(2, 12)}`,
    ...overrides,
  };
}
export function runWorker(address = REAL_CLAMD, overrides = {}) {
  const completed = spawnSync(WORKER, [], {
    cwd: root,
    env: workerEnv(address, overrides),
    encoding: 'utf8',
    timeout: 45000,
  });
  if (completed.status !== 0) {
    throw new Error(`fileworker failed status=${completed.status}: ${(completed.stdout ?? '')}${(completed.stderr ?? '')}`);
  }
  return completed.stdout ?? '';
}
export function workerPopen(address, overrides = {}) {
  return spawn(WORKER, [], {
    cwd: root,
    env: workerEnv(address, overrides),
    stdio: ['ignore', 'pipe', 'pipe'],
  });
}
export async function stopProcess(child, timeoutMs = 2000) {
  if (!child || child.exitCode !== null) return;
  await new Promise((resolve) => {
    let settled = false;
    let timer;
    const finish = () => { if (!settled) { settled = true; if (timer) clearTimeout(timer); resolve(); } };
    child.once('exit', finish);
    child.once('error', finish);
    child.kill('SIGTERM');
    timer = setTimeout(() => {
      if (child.exitCode === null) child.kill('SIGKILL');
      setTimeout(finish, 250).unref();
    }, timeoutMs);
    timer.unref();
  });
}
export function waitForOutput(child, fragment, timeoutMs = 5000) {
  return new Promise((resolve, reject) => {
    let buffer = '';
    const timer = setTimeout(() => reject(new Error(`timeout waiting for ${fragment}; output=${buffer}`)), timeoutMs);
    const listen = (chunk) => {
      buffer += chunk.toString();
      if (buffer.includes(fragment)) {
        clearTimeout(timer);
        child.stdout?.off('data', listen);
        resolve(buffer);
      }
    };
    child.stdout?.on('data', listen);
    child.once('exit', (code) => {
      if (!buffer.includes(fragment)) {
        clearTimeout(timer);
        reject(new Error(`process exited ${code} before ${fragment}; output=${buffer}`));
      }
    });
  });
}
export async function startFault(mode, port, holdSeconds = 20) {
  const child = spawn('python3', [FAULT_SERVER, '--mode', mode, '--port', String(port), '--hold-seconds', String(holdSeconds)], {
    cwd: root,
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  await waitForOutput(child, 'READY');
  return child;
}
export async function waitUntil(fn, timeoutMs, description, intervalMs = 100) {
  const deadline = Date.now() + timeoutMs;
  let last;
  while (Date.now() < deadline) {
    try {
      last = await fn();
      if (last) return last;
    } catch (error) {
      last = String(error);
    }
    await sleep(intervalMs);
  }
  throw new Error(`timeout waiting for ${description}; last=${JSON.stringify(last)}`);
}
export function dbState(id) {
  return mysql(`SELECT scan_state FROM files WHERE id=${Number(id)}`);
}
export function holdFilesWriteLock(seconds = 3) {
  const child = spawn('mysql', mysqlArgs(`LOCK TABLES files WRITE; DO SLEEP(${Number(seconds)}); UNLOCK TABLES;`), {
    cwd: root,
    env: { ...process.env, MYSQL_PWD: MYSQL_PASSWORD },
    stdio: ['ignore', 'ignore', 'pipe'],
  });
  let stderr = '';
  child.stderr?.on('data', (chunk) => { stderr += chunk.toString(); });
  const done = new Promise((resolve, reject) => {
    child.once('error', reject);
    child.once('exit', (code) => code === 0 ? resolve() : reject(new Error(`files write lock exited ${code}: ${stderr}`)));
  });
  return { child, done };
}
