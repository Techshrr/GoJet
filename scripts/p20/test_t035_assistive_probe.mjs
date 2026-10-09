import test from 'node:test';
import assert from 'node:assert/strict';
import {speechReceipt} from './t035_assistive_probe.mjs';
test('Orca samples require fresh real speech-dispatcher output and matching name/role', () => {
  const out = "SPEECH OUTPUT: 'GoJet link' {}";
  const sent = "SPEECH DISPATCHER: Speaking 'GoJet link' as string";
  assert.ok(speechReceipt(out+'\n'+sent, 'GoJet', 'link'));
  assert.equal(speechReceipt(out, 'GoJet', 'link'), null);
  assert.equal(speechReceipt(sent, 'GoJet', 'link'), null);
  assert.equal(speechReceipt(out+'\n'+sent, 'Save', 'button'), null);
  assert.equal(speechReceipt(out+'\n'+sent, 'GoJet', 'button'), null);
  assert.equal(speechReceipt(out+'\n'+sent, 'user@example.test', 'link'), null);
  assert.ok(speechReceipt("SPEECH OUTPUT: 'GoJet' {}\nSPEECH OUTPUT: 'link' {}\nSPEECH DISPATCHER: Speaking 'GoJet' as string\nSPEECH DISPATCHER: Speaking 'link' as string", 'GoJet', 'link'));
});
test('delayed previous-control role cannot replace the current control role', () => {
  const old = "SPEECH DISPATCHER: Speaking 'Revoke push button.' as string";
  const current = "SPEECH OUTPUT: 'Confirm revoke push button.' {}\nSPEECH DISPATCHER: Speaking 'Confirm revoke push button.' as string";
  const receipt = speechReceipt(old+'\n'+current, 'Confirm revoke', 'button');
  assert.equal(receipt.role_dispatcher_output, current.split('\n')[1]);
  assert.equal(speechReceipt(old+"\nSPEECH OUTPUT: 'Confirm revoke' {}\nSPEECH DISPATCHER: Speaking 'Confirm revoke' as string", 'Confirm revoke', 'button'),null);
});
