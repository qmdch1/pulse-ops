import test from 'node:test';
import assert from 'node:assert/strict';
import {eventRules,ruleState} from '../lib/monitoring/rule-catalog.ts';
import {simpleRules,detectIncidents} from '../lib/monitoring/rules.ts';
import {metricById} from '../lib/monitoring/catalog.ts';
import {infrastructureKind,infrastructureMetrics} from '../lib/monitoring/infrastructure.ts';
import type {Snapshot,MetricResult,Target} from '../lib/monitoring/types.ts';

test('displayed simple rule boundaries match executable predicates, including equality',()=>{
 for(const [id,,predicate]of simpleRules){const m=metricById.get(id)!;assert.notEqual(m.warning,undefined,id);for(const value of [m.warning!-1,m.warning!,m.warning!+1])assert.equal(predicate(value),m.direction==='below'?value<m.warning!:value>m.warning!,id)}
});
test('rule explanations only reference known metrics and required evidence',()=>{
 assert.equal(new Set(eventRules.map(r=>r.id)).size,eventRules.length);
 for(const rule of eventRules){for(const id of rule.metricIds)assert.ok(metricById.has(id),id);for(const id of rule.requiredIds)assert.ok(rule.metricIds.includes(id),id)}
});
test('rule state distinguishes missing input, live event, and no current event',()=>{
 const metric:MetricResult={id:'cookie-expiry',state:'ok',latest:20,series:[{labels:{},points:Array.from({length:20},(_,i)=>({time:10000-(19-i)*10,value:20}))}]};
 const snapshot:Snapshot={mode:'test',connected:true,collectedAt:'2026-10-08T00:00:00Z',start:9000,end:10000,step:10,targets:[],alerts:[],metrics:[metric],grafanaUrl:null};
 const rule=eventRules.find(r=>r.id==='cookie-expiry')!;
 assert.equal(ruleState(rule,null,[]),'missing');
 assert.equal(ruleState(rule,snapshot,detectIncidents(snapshot)),'firing');
 assert.equal(ruleState(rule,snapshot,[]),'waiting');
 assert.equal(ruleState(rule,{...snapshot,metrics:[{...metric,state:'stale',latest:null}]},[]),'missing');
});
test('database and cache profiles exclude unrelated application latency',()=>{
 const target=(job:string):Target=>({job,instance:'test:1234',health:'up',lastScrape:''});
 assert.equal(infrastructureKind(target('postgres')),'database');
 assert.equal(infrastructureKind(target('redis')),'cache');
 assert.equal(infrastructureKind(target('application')),'application');
 assert.ok(infrastructureMetrics('database').some(m=>m.id==='db-up'));
 assert.ok(!infrastructureMetrics('database').some(m=>m.id==='p99'));
 assert.ok(infrastructureMetrics('cache').some(m=>m.id==='redis-memory'));
 assert.ok(!infrastructureMetrics('cache').some(m=>m.id==='db-connections'));
});
