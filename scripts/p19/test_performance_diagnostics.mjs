import test from 'node:test';
import assert from 'node:assert/strict';
import {runInNewContext} from 'node:vm';
import {installFrameDiagnostics} from './performance_diagnostics.mjs';

function install(supported) {
  const instances=[];
  class Observer {
    static supportedEntryTypes=supported;
    constructor(callback){this.callback=callback;this.pending=[];instances.push(this);}
    observe(options){this.options=options;}
    takeRecords(){return this.pending.splice(0);}
  }
  const window={};
  runInNewContext(`(${installFrameDiagnostics.toString()})()`,{PerformanceObserver:Observer,window});
  return {window,instances,read:()=>JSON.parse(JSON.stringify(window.__p19ReadFrameDiagnostics()))};
}

test('unsupported attribution is explicit and never invents a passing verdict',()=>{
  const env=install(['longtask']);
  assert.equal(env.instances.length,0);
  assert.deepEqual(env.read(),{supported:false,frames:[],scope:'supplemental attribution; does not replace long-task or INP admission'});
});

test('serialized browser observer preserves startup, script attribution and pending final entries',()=>{
  const env=install(['long-animation-frame']);const observer=env.instances[0];
  assert.equal(observer.options.type,'long-animation-frame');assert.equal(observer.options.buffered,true);
  const cyclicWindow={};cyclicWindow.self=cyclicWindow;
  const script={startTime:10,duration:75,executionStart:11,forcedStyleAndLayoutDuration:30,
    pauseDuration:0,invoker:'DOMWindow.onclick',invokerType:'event-listener',
    sourceURL:'http://127.0.0.1/assets/index.js',sourceFunctionName:'handler',sourceCharPosition:42,
    windowAttribution:'self',window:cyclicWindow};
  const entry={startTime:0,duration:100,blockingDuration:50,firstUIEventTimestamp:8,
    renderStart:85,styleAndLayoutStart:90,scripts:[script]};
  observer.callback({getEntries:()=>[entry]});
  observer.pending.push({...entry,startTime:200,scripts:[]});
  const result=env.read();
  assert.equal(result.supported,true);assert.equal(result.frames.length,2);
  assert.equal(result.frames[0].startTime,0);assert.equal(result.frames[0].duration,100);
  assert.equal(result.frames[0].scripts[0].forcedStyleAndLayoutDuration,30);
  assert.equal(result.frames[0].scripts[0].sourceCharPosition,42);
  assert.equal('window' in result.frames[0].scripts[0],false);
  assert.deepEqual(result.frames[1].scripts,[]);
  assert.deepEqual(env.read(),result,'draining must not duplicate final entries');
  assert.equal('status' in result,false,'attribution cannot replace the admission verdict');
});
