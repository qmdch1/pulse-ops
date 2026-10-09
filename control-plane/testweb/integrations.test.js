import {test,assert} from './harness.js';
import {openIntegration,mountIntegrations,integrationInput} from '/assets/integrations.js';
import {registeredIncidents} from '/assets/lib/assets.js';
import {eventRules} from '/assets/lib/rule-catalog.js';
import {simpleRules} from '/assets/lib/rules.js';
import {metrics} from '/assets/lib/catalog.js';

const fixtures=await (await fetch('/__tests__/event-fixtures.json')).json();
test('browser event evaluation agrees with server fixtures for boundaries, gaps and compound rules',()=>{
 for(const f of fixtures){
  const end=10000,asset={id:'a',name:'API',enabled:true,status:'connected'};
  const results=metrics.map(m=>{if(!Object.hasOwn(f.values,m.id))return {...m,latest:null,state:'missing',series:[]};const points=[];for(let age=f.seconds;age>=0;age-=15){if(f.gap&&age===60)continue;points.push({time:end-age-(f.stale?60:0),value:f.nullLast&&age===0?null:f.values[m.id]})}return {...m,latest:f.nullLast||f.stale?null:f.values[m.id],state:f.nullLast||f.stale?'stale':'ok',series:[{labels:{assetId:'a'},points}]}});
  const snapshot={assets:[asset],metrics:results,end,step:15,start:end-3600,connected:true,mode:'test',targets:[],alerts:[]};
  assert.deepEqual(registeredIncidents(snapshot).map(i=>i.ruleId).sort(),f.want,f.name);
 }
});
test('shared server simple boundaries match browser event predicates',()=>{
 for(const [id,,predicate] of simpleRules){const check=eventRules.find(r=>r.id===id).checks[0];for(const value of [check.value-1,check.value,check.value+1])assert.equal(predicate(value),check.op==='<'?value<check.value:value>check.value,id)}
 assert.equal(eventRules.filter(r=>r.serverSupported).length,eventRules.length-1);
});
test('editing integration preserves masked secrets and exposes every provider',()=>{
 const d=openIntegration({id:'saved',name:'Ops',provider:'webhook',hasUrl:true,hasToken:true,version:3,severities:['critical'],enabled:true,recovery:false});
 try{const form=d.querySelector('form');assert.equal(form.elements.url.value,'');assert.equal(form.elements.token.value,'');assert.equal(form.elements.provider.options.length,4);const input=integrationInput(form,{hasUrl:true,version:3});assert.ok(!Object.hasOwn(input,'url'));assert.ok(!Object.hasOwn(input,'token'));assert.deepEqual(input.severities,['critical']);assert.equal(input.enabled,true);assert.equal(input.recovery,false);d.querySelector('[data-clear-token]').click();assert.equal(integrationInput(form,{hasUrl:true,version:3}).token,'')}finally{d.close()}
});
test('integration list escapes untrusted names and retries loading failure',async()=>{
 const root=document.createElement('section');document.body.append(root);let calls=0;
 const api=async()=>{calls++;if(calls===1)throw new Error('연결 실패');return [{id:'x',name:'<img src=x onerror=alert(1)>',provider:'discord',host:'discord.com',enabled:false,severities:['critical'],pending:0}]};
 try{await mountIntegrations(root,api);assert.ok(root.textContent.includes('연결 실패'));root.querySelector('[data-retry]').click();await new Promise(resolve=>setTimeout(resolve,0));assert.equal(calls,2);assert.equal(root.querySelectorAll('img').length,0);assert.ok(root.textContent.includes('<img src=x onerror=alert(1)>'));assert.ok(root.textContent.includes('테스트 전송'))}finally{root.remove()}
});
test('failed test delivery remains visible and can be retried',async()=>{
 const root=document.createElement('section');document.body.append(root);let sends=0;
 const api=async(path)=>{if(path.endsWith('/test')){sends++;throw new Error('웹훅이 HTTP 429로 응답했습니다')}return [{id:'x',name:'Ops',provider:'slack',host:'hooks.slack.com',enabled:false,severities:['warning'],pending:0}]};
 try{await mountIntegrations(root,api);const b=root.querySelector('[data-integration-test]');b.click();await new Promise(resolve=>setTimeout(resolve,0));assert.ok(root.textContent.includes('HTTP 429'));assert.equal(b.disabled,false);b.click();await new Promise(resolve=>setTimeout(resolve,0));assert.equal(sends,2)}finally{root.remove()}
});
test('saving an integration updates the current settings section after an intervening page render',async()=>{
 const original=document.createElement('section');original.id='integration-remount-test';document.body.append(original);let items=[];
 const api=async(path,method,input)=>{if(method==='POST'){items=[{id:'saved',...input,host:'receiver.example',pending:0}];return items[0]}return items};
 let current,d;
 try{await mountIntegrations(original,api);original.querySelector('[data-integration-add]').click();d=document.querySelector('dialog[open]');d.querySelector('[name=name]').value='After refresh';d.querySelector('[name=url]').value='https://receiver.example/events';current=document.createElement('section');current.id=original.id;original.replaceWith(current);await mountIntegrations(current,api);d.querySelector('form').requestSubmit();await new Promise(resolve=>setTimeout(resolve,0));assert.ok(current.textContent.includes('After refresh'));assert.equal(current.querySelectorAll('[data-integration-edit]').length,1)}finally{d?.close();original.remove();current?.remove()}
});
