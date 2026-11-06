import test from 'node:test';
import assert from 'node:assert/strict';
import {confirmedNoContentDeletion, closeAndValidateDiagnostics} from './network_outcome.mjs';

test('only exact completed 204 deletion with durable and client proof can be classified',()=>{
  const row={url:'http://127.0.0.1:4174/api/workspaces/unit/text-shares/4',method:'DELETE',
    resource_type:'fetch',navigation:false,response_status:204,failure:{errorText:'net::ERR_ABORTED'}};
  const proof={request_url:row.url,client_redirect:true,server_status:204,public_status:410,
    database_tombstone:true,initial_version:1,database_version:2,audit_count:1};
  assert.equal(confirmedNoContentDeletion(row,proof),true);
  assert.equal(confirmedNoContentDeletion(row,null),false);
  for(const [field,value] of [['url','other'],['method','GET'],['response_status',null],['response_status',500],
    ['navigation',true],['resource_type','document'],['failure',{errorText:'net::ERR_FAILED'}]])
    assert.equal(confirmedNoContentDeletion({...row,[field]:value},proof),false);
  for(const [field,value] of [['request_url','other'],['client_redirect',false],['server_status',500],
    ['public_status',200],['database_tombstone',false],['database_version',1],['audit_count',0],['audit_count',2]])
    assert.equal(confirmedNoContentDeletion(row,{...proof,[field]:value}),false);
});

test('late shutdown failures are classified before taking the evidence snapshot', async()=>{
  const row={url:'http://127.0.0.1:4174/api/workspaces/unit/text-shares/4',method:'DELETE',
    resource_type:'fetch',navigation:false,response_status:204,failure:{errorText:'net::ERR_ABORTED'}};
  const proof={request_url:row.url,client_redirect:true,server_status:204,public_status:410,
    database_tombstone:true,initial_version:1,database_version:2,audit_count:1};
  const report={request_failures:[],confirmed_no_content_completions:[]};
  let closed=false;
  const context={async close(){await new Promise(resolve=>setImmediate(resolve));report.request_failures.push(row);closed=true;}};
  const snapshot=await closeAndValidateDiagnostics(context,report,final=>{
    assert.equal(closed,true);
    final.confirmed_no_content_completions=final.request_failures.filter(r=>confirmedNoContentDeletion(r,proof));
    assert.deepEqual(final.confirmed_no_content_completions,final.request_failures);
  });
  assert.deepEqual(snapshot.request_failures,[row]);
  assert.deepEqual(snapshot.confirmed_no_content_completions,[row]);
  report.request_failures[0].failure.errorText='changed after finalization';
  report.request_failures.push({url:'unrelated'});
  assert.equal(snapshot.request_failures.length,1);
  assert.equal(snapshot.request_failures[0].failure.errorText,'net::ERR_ABORTED');
});

test('unexpected late failures and failed context shutdown cannot produce accepted evidence',async()=>{
  const report={request_failures:[]};
  const context={async close(){await new Promise(resolve=>setImmediate(resolve));report.request_failures.push({url:'unrelated',failure:{errorText:'net::ERR_FAILED'}});}};
  await assert.rejects(closeAndValidateDiagnostics(context,report,final=>{
    assert.equal(final.request_failures.length,0,'unexpected request failure');
  }),/unexpected request failure/);
  let validated=false;
  await assert.rejects(closeAndValidateDiagnostics({async close(){throw new Error('close failed');}},report,()=>{validated=true;}),/close failed/);
  assert.equal(validated,false);
});
