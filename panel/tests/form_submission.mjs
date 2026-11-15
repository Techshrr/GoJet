// Exercise production event handlers and validate their payload with the Python backend.
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {spawnSync} from 'node:child_process';
import vm from 'node:vm';
const source=readFileSync(process.env.PANEL_APP_JS||'panel/web/app.js','utf8');
const html=readFileSync('panel/web/index.html','utf8');
const values={admin_email:'admin@example.com',admin_name:'Administrator',admin_password:'test-password-12345',smtp_host:'mail.example.com',smtp_port:'587',smtp_from:'no-reply@example.com',smtp_user:'',smtp_password:'',turnstile_site_key:'test-site-key-1234567890',turnstile_secret:'test-secret-key-1234567890',webrisk_key:'test-google-key-1234567890',turnstile_token:''};
async function exercise(injected){
 const nodes=new Map();
 const node=id=>{if(!nodes.has(id))nodes.set(id,{value:values[id]||'',listeners:{},addEventListener(event,fn){this.listeners[event]=fn;},replaceChildren(){},removeAttribute(){},setAttribute(){}});return nodes.get(id);};
 const names=[...html.matchAll(/<input\b[^>]*\bname="([^"]+)"[^>]*>/g)].map(m=>m[1]).filter(n=>n!=='code');
 const extras=new Map();const requests=[];let opts;
 const document={getElementById:node,querySelectorAll:()=>[],createElement:()=>({})};
 const window={turnstile:{render:(selector,options)=>{opts=options;if(options['response-field']!==false)extras.set('cf-turnstile-response','test-token');options.callback('test-token');return 1;},remove(){},reset(){}}};
 const context=vm.createContext({document,window,FormData:class {constructor(){this.entries=[...names.map(n=>[n,node(n).value]),...extras];}get(n){return this.entries.find(([k])=>k===n)?.[1]??null;}[Symbol.iterator](){return this.entries[Symbol.iterator]();}},fetch:async(url,init)=>{requests.push(JSON.parse(init.body));return {ok:true,json:async()=>({})};},setTimeout:()=>1,clearTimeout(){}});
 vm.runInContext(source,context);
 await node('verify-button').listeners.click();
 if(injected){extras.set('cf-turnstile-response','legacy-hidden-token');extras.set('extension-field','not-configuration');}
 await node('configuration-form').listeners.submit({preventDefault(){},target:node('configuration-form'),submitter:{disabled:false}});
 assert.equal(requests.length,1,'configuration must be submitted');
 const result=spawnSync('python3',['-c','import json,sys;sys.path.insert(0,"panel");from common import validate;validate(json.load(sys.stdin))'],{input:JSON.stringify(requests[0]),encoding:'utf8'});
 assert.equal(result.status,0,'production backend rejected submitted field set: '+result.stderr);
 assert.deepEqual(Object.keys(requests[0]).sort(),Object.keys(values).sort());
 assert.equal(requests[0].turnstile_token,'test-token');
 assert.equal(requests[0].smtp_password,'','optional empty credentials must be retained');
 assert.equal(opts['response-field'],false,'app owns its token input');
 opts['expired-callback']();
 await node('configuration-form').listeners.submit({preventDefault(){},target:node('configuration-form'),submitter:{disabled:false}});
 assert.equal(requests.length,1,'expired token must prevent resubmission');
 assert.equal(node('turnstile_token').value,'');
}
await exercise(false);await exercise(true);
console.log('PASS: real submit handlers/backend contract; injected fields excluded; token expiry blocks submission');
