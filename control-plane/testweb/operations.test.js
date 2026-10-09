import {test,assert} from './harness.js';
import {parseAssetCSV,splitLabels,assetSearchText,mountViews,mountDeliveries,mountEventHistory,openMute,openGlobalSearch,openDiagnostic} from '/assets/operations.js';
import {openIntegration,integrationInput} from '/assets/integrations.js';
const tick=()=>new Promise(r=>setTimeout(r,0));
function root(){const el=document.createElement('section');document.body.append(el);return el}
test('CSV handles BOM, CRLF, quoted commas and invalid input before registration',()=>{
 const rows=parseAssetCSV('\uFEFFname,kind,address,port,tags\r\n"API, Seoul",http,https://example.invalid,443,"team-api,region-seoul"\r\n');
 assert.equal(rows.length,1);assert.equal(rows[0].name,'API, Seoul');assert.equal(rows[0].port,443);assert.deepEqual(rows[0].tags,['team-api','region-seoul']);
 for(const raw of ['name,kind\nx,unknown','name,kind,password\nx,http,secret','name,kind\n"unclosed,http','name,kind\n"x"oops,http','name,kind\nx,http,extra','name,kind,port\nx,http,nope'])assert.throws(()=>parseAssetCSV(raw));
 assert.deepEqual(splitLabels(' team-api, team-api, region-seoul '),['team-api','region-seoul']);
 assert.ok(assetSearchText({name:'API',environment:'prod',tags:['team-api']}).includes('team-api'));
});
test('integration routing, grouping and summary selections survive editing masked secrets',()=>{
 const d=openIntegration({id:'x',name:'Ops',provider:'webhook',hasUrl:true,severities:['critical'],assetIds:['a'],environments:['prod'],tags:['team-api'],ruleIds:['connection'],groupSeconds:60,summary:'weekly',summaryHour:10},()=>{},async()=>{},[{id:'a',name:'API'}]);
 try{const input=integrationInput(d.querySelector('form'),{hasUrl:true});assert.deepEqual(input.assetIds,['a']);assert.deepEqual(input.environments,['prod']);assert.deepEqual(input.tags,['team-api']);assert.deepEqual(input.ruleIds,['connection']);assert.equal(input.groupSeconds,60);assert.equal(input.summary,'weekly');assert.equal(input.summaryHour,10);assert.ok(!Object.hasOwn(input,'url'))}finally{d.close()}
});
test('maintenance form saves explicit scopes and a bounded time window',async()=>{
 let saved;
 const d=openMute({},[{id:'a',name:'API'}],()=>{},async(path,method,input)=>saved=input);
 try{const form=d.querySelector('form');form.elements.name.value='점검';form.elements.assets.options[0].selected=true;form.elements.envs.value='prod';form.elements.tags.value='team-api';d.querySelector('[data-minutes="15"]').click();form.requestSubmit();await tick();assert.deepEqual(saved.assetIds,['a']);assert.deepEqual(saved.environments,['prod']);assert.ok(saved.endsAt>saved.startsAt);assert.equal(saved.endsAt-saved.startsAt,900)}finally{d.close()}
});
test('delivery history displays escaped evidence and retries a failed version once',async()=>{
 const el=root();let calls=0;const record={id:'d',version:4,integrationId:'i',integrationName:'<img src=x>',event:{title:'Error',assetName:'API'},status:'failed',attempts:5,at:100,error:'HTTP 429'};
 const api=async(path,method,input)=>{if(method==='POST'){calls++;assert.equal(path,'deliveries/d/retry');assert.equal(input.version,4);record.status='requeued';return {message:'재전송 예약'}}return [record]};
 try{await mountDeliveries(el,api);assert.equal(el.querySelectorAll('img').length,0);el.querySelector('[data-retry]').click();await tick();assert.equal(calls,1);assert.ok(el.textContent.includes('재전송 예약'));assert.equal(el.querySelectorAll('[data-retry]').length,0)}finally{el.remove()}
});
test('event workflow note submission preserves live monitoring evidence and optimistic version',async()=>{
 const el=root();let updated;
 const v={id:'e',version:3,event:{title:'CPU',assetName:'API',environment:'prod',tags:['team-api'],status:'firing',at:100},workflow:'open',notes:[]};
 const api=async(path,method,input)=>{if(method==='PUT'){updated=input;v.workflow=input.workflow;v.notes=[{text:input.note,at:100}];return v}return [v]};
 let d;try{await mountEventHistory(el,api);el.querySelector('[data-event-record]').click();d=document.querySelector('dialog[open]');d.querySelector('[name=workflow]').value='acknowledged';d.querySelector('[name=note]').value='<script>조치 확인</script>';d.querySelector('form').requestSubmit();await tick();assert.equal(updated.version,3);assert.equal(updated.workflow,'acknowledged');assert.ok(el.textContent.includes('장애 지속'));assert.ok(el.textContent.includes('확인함'));assert.equal(el.querySelectorAll('script').length,0)}finally{d?.close();el.remove()}
});
test('named dashboard stores selection, range and cadence and can apply a shared view',async()=>{
 const el=root();let saved,applied;const view={id:'v',name:'DB 운영',version:2,assetIds:['db'],range:86400,refreshSeconds:60};
 const api=async(path,method,input)=>{if(method==='POST'){saved=input;return {...input,id:'new'}}return [view]};
 let d;try{await mountViews(el,()=>({assetIds:['api'],range:3600,refreshSeconds:15}),v=>applied=v,api);const select=el.querySelector('select');select.value='v';select.dispatchEvent(new Event('change'));assert.equal(applied,view);el.querySelector('[data-save-view]').click();d=document.querySelector('dialog[open]');d.querySelector('input').value='API 운영';d.querySelector('form').requestSubmit();await tick();assert.equal(saved.name,'API 운영');assert.deepEqual(saved.assetIds,['api']);assert.equal(saved.range,3600);assert.equal(saved.refreshSeconds,15)}finally{d?.close();el.remove()}
});
test('global search finds team tags and routes the matching asset',async()=>{
 let selected;const a={id:'a',name:'API',tags:['team-platform']},d=await openGlobalSearch([a],{asset:v=>selected=v,rule:()=>{},history:()=>{},view:()=>{}},async()=>[]);
 try{d.querySelector('input').value='team-platform';d.querySelector('input').dispatchEvent(new Event('input'));assert.ok(d.textContent.includes('API'));d.querySelector('[data-result]').click();assert.equal(selected,a)}finally{d.close()}
});
test('diagnostics render actual DNS and protocol failures as actionable escaped guidance',async()=>{
 const d=await openDiagnostic({id:'a',name:'API'},async path=>{assert.equal(path,'assets/a/diagnose');return [{name:'DNS',status:'failed',detail:'<img src=x> DNS 주소를 확인하세요'}]});
 try{assert.ok(d.textContent.includes('DNS 주소를 확인하세요'));assert.equal(d.querySelectorAll('img').length,0);assert.ok(d.textContent.includes('실패'))}finally{d.close()}
});
