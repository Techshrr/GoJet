// Transport regression checks only; not formal browser or product evidence.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { stripTypeScriptTypes } from 'node:module';
const source = readFileSync('frontend/apps/workspace/src/workspace/runtime.ts', 'utf8')
  .replace("import { GoJetWorkspaceClient } from '@gojet/api-client';", 'class GoJetWorkspaceClient { constructor(transport) { this.transport = transport; } }')
  .replaceAll('import.meta.env', '({})');
const mod = await import(`data:text/javascript;base64,${Buffer.from(stripTypeScriptTypes(source)).toString('base64')}`);
globalThis.window = { sessionStorage: { getItem: () => 'remembered-workspace' } };
let identity = 'user-one';
let reads = 0;
const requests = [];
globalThis.fetch = async (input, init) => {
  if (input === '/api/me') {
    reads++;
    return Response.json({ user: { id: identity, email: 'one@example.test', display_name: 'One' }, csrf_token: `once-${reads}` });
  }
  requests.push({ input, init });
  return Response.json({});
};
assert.equal(mod.readP12Runtime(), null);
const runtime = await mod.loadP12Session();
assert.equal(runtime.testAuthority, false);
assert.equal(runtime.workspaceId, 'remembered-workspace');
const { transport } = mod.createP12Client(runtime);
assert.deepEqual(transport.headers(), {});
await transport.fetch('/api/workspaces', { method: 'GET' });
assert.equal(reads, 1);
await transport.fetch('/api/workspaces/one', { method: 'PATCH', headers: { 'Content-Type': 'application/json' } });
await transport.fetch('/api/workspaces/one', { method: 'DELETE' });
assert.equal(requests[1].init.headers.get('X-CSRF-Token'), 'once-2');
assert.equal(requests[2].init.headers.get('X-CSRF-Token'), 'once-3');
assert.equal(requests[1].init.headers.get('Content-Type'), 'application/json');
assert(requests.every(({ init }) => init.credentials === 'same-origin'));
identity = 'different-user';
await assert.rejects(transport.fetch('/api/workspaces/one', { method: 'PATCH' }), /session changed/);
assert.equal(requests.length, 3);
globalThis.fetch = async () => new Response('', { status: 401 });
await assert.rejects(mod.loadP12Session(), /session unavailable/);
await assert.rejects(transport.fetch('/api/workspaces/one', { method: 'DELETE' }), /session unavailable/);
console.log('PASS: session loading, production headers, fresh CSRF, identity changes and expired sessions');
