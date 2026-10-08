import {metrics,metricById,type Metric} from './catalog.ts';
import {databaseProfiles,isSQLDatabase,scopeSnapshot,type Asset} from './assets.ts';
import {eventRules} from './rule-catalog.ts';
import type {Incident,Snapshot} from './types';
import type {EvidenceLine} from './evidence.ts';

export type EventGroup={id:string;title:string;severity:Incident['severity'];kind:Incident['kind'];incidents:Incident[];assetIds:string[];metricIds:string[]};
const severityRank={critical:0,warning:1,notice:2};
export function groupEvents(incidents:Incident[]):EventGroup[]{
 const groups=new Map<string,EventGroup>();
 for(const incident of incidents){
  const id=incident.ruleId||incident.id,rule=eventRules.find(r=>r.id===id);
  const group=groups.get(id)||{id,title:rule?.title||incident.title,severity:incident.severity,kind:incident.kind,incidents:[],assetIds:[],metricIds:[]};
  group.incidents.push(incident);
  if(incident.assetId&&!group.assetIds.includes(incident.assetId))group.assetIds.push(incident.assetId);
  group.metricIds=[...new Set([...group.metricIds,...incident.metricIds,...(rule?.metricIds||[])])];
  if(severityRank[incident.severity]<severityRank[group.severity])group.severity=incident.severity;
  groups.set(id,group);
 }
 return [...groups.values()].sort((a,b)=>severityRank[a.severity]-severityRank[b.severity]||b.assetIds.length-a.assetIds.length||a.id.localeCompare(b.id));
}

export const contextMetrics:Record<string,string[]>={
 server:['node-cpu','memory-host','load','disk','network-in','network-out'],
 application:['requests','p99','p97','errors','cpu','memory','pool-wait','db-latency','cache-hit'],
 postgres:['db-up','db-probe','db-connections','db-transactions','db-locks','db-rollback','db-buffer'],
 mysql:['db-up','db-probe','db-connections','db-active','db-connection-usage','db-lock-waiters','mysql-buffer-hit'],
 mariadb:['db-up','db-probe','db-connections','db-active','db-connection-usage','db-lock-waiters','mysql-buffer-hit'],
 oracle:['db-up','db-probe','db-connections','db-active','db-connection-usage','db-lock-waiters','oracle-buffer-hit'],
 redis:['redis-up','redis-probe','redis-hit','redis-memory','redis-blocked','redis-expired'],
 http:['probe','probe-latency','http-status','tls-expiry'],
};
const sources:Record<string,string[]>={
 server:['Node exporter','cAdvisor','kube-state-metrics'],
 application:['Application','Process collector','Session exporter','Credential exporter','Node.js exporter'],
 redis:['Redis exporter'],http:['Blackbox exporter'],
};
export function metricsForAsset(snapshot:Snapshot,asset:Asset):Metric[]{
 const directProfiles:Record<string,string[]>={server:['uptime','disk-free'],redis:['redis-used','redis-clients','redis-commands']};
 const ids=new Set([...(databaseProfiles[asset.kind]||contextMetrics[asset.kind]||[]),...(directProfiles[asset.kind]||[]),'targets','targets-down','scrape-duration','scrape-age']);
 for(const m of snapshot.metrics)if(m.series.some(s=>s.labels.assetId===asset.id&&s.points.some(p=>p.value!==null)))ids.add(m.id);
 return metrics.filter(m=>ids.has(m.id)||(!isSQLDatabase(asset.kind)&&(sources[asset.kind]||[]).includes(m.source)));
}
export function observedForAsset(snapshot:Snapshot,metricId:string,assetId:string){
 return !!snapshot.metrics.find(m=>m.id===metricId)?.series.some(s=>s.labels.assetId===assetId&&s.points.some(p=>p.value!==null));
}
export function relatedAssets(snapshot:Snapshot,rootIds:string[]):Asset[]{
 const byId=new Map(snapshot.assets?.map(a=>[a.id,a])||[]),seen=new Set<string>(),pending=[...rootIds];
 while(pending.length){const id=pending.shift()!;if(seen.has(id))continue;const asset=byId.get(id);if(!asset)continue;seen.add(id);pending.push(...asset.dependencies)}
 return [...seen].map(id=>byId.get(id)!);
}
export function eventMetricIds(group:EventGroup,assets:Asset[]){return [...new Set([...group.metricIds,...assets.flatMap(a=>contextMetrics[a.kind]||[])])]}

// Comparison is a presentation scope only. No averaging of percentiles and no
// sharing measurements between assets during incident evaluation.
export function comparisonSnapshot(snapshot:Snapshot,ids:string[]):Snapshot{
 const allowed=new Set(ids),assets=snapshot.assets?.filter(a=>allowed.has(a.id))||[];
 if(assets.length===1)return scopeSnapshot(snapshot,assets[0].id);
 const restrict=(rows:Snapshot['metrics'])=>rows.map(m=>{
  const series=m.series.filter(s=>allowed.has(s.labels.assetId));
  const live=series.some(s=>assets.find(a=>a.id===s.labels.assetId)?.enabled&&s.points.at(-1)?.value!=null&&snapshot.end-s.points.at(-1)!.time<=45);
  return {...m,series,latest:null,state:(!series.length?'missing':live?'ok':'stale') as typeof m.state};
 });
 return {...snapshot,assets,metrics:restrict(snapshot.metrics),evaluationMetrics:snapshot.evaluationMetrics?restrict(snapshot.evaluationMetrics):undefined,targets:snapshot.targets.filter(t=>allowed.has(t.instance)),alerts:[]};
}
export function comparisonLines(snapshot:Snapshot,ids:string[],metricIds:string[]):EvidenceLine[]{
 const allowed=new Set(ids),lines:EvidenceLine[]=[];
 for(const id of [...new Set(metricIds)]){
  const metric=metricById.get(id);if(!metric)continue;
  for(const series of snapshot.metrics.find(m=>m.id===id)?.series||[]){
   if(!allowed.has(series.labels.assetId)||!series.points.some(p=>p.value!==null))continue;
   lines.push({key:`${id}:${series.labels.assetId}`,metricId:id,name:`${series.labels.name||series.labels.assetId} · ${metric.title}`,unit:metric.unit,points:series.points});
  }
 }
 return lines;
}
// Preserve every selected line, split crowded charts rather than truncating.
export function chartGroups(lines:EvidenceLine[],maxLines=8):EvidenceLine[][]{
 const byUnit=new Map<string,EvidenceLine[]>();
 for(const line of lines)byUnit.set(line.unit,[...(byUnit.get(line.unit)||[]),line]);
 const groups:EvidenceLine[][]=[];
 for(const values of byUnit.values())for(let i=0;i<values.length;i+=maxLines){
  const chunk=values.slice(i,i+maxLines),last=groups.at(-1);
  if(last&&last.length+chunk.length<=maxLines&&new Set([...last,...chunk].map(l=>l.unit)).size<=2)last.push(...chunk);else groups.push(chunk);
 }
 return groups;
}
export type ChartRows=Record<string,number|null>[];
export function chartRows(lines:EvidenceLine[],step:number):ChartRows{
 const rows=new Map<number,Record<string,number|null>>(),bucket=Math.max(1,step);
 lines.forEach((line,index)=>line.points.forEach(point=>{
  const time=Math.floor(point.time/bucket)*bucket;
  const row=rows.get(time)||{time};row[`v${index}`]=point.value;rows.set(time,row);
 }));
 const ordered=[...rows.values()].sort((a,b)=>Number(a.time)-Number(b.time)),result:ChartRows=[];
 for(const row of ordered){
  const previous=result.at(-1);
  // An absent collection interval must break the line, including when every
  // selected asset has a gap and there is no other series to supply a row.
  if(previous&&Number(row.time)-Number(previous.time)>bucket){
   const gap:Record<string,number|null>={time:Number(previous.time)+bucket};
   lines.forEach((_,i)=>gap[`v${i}`]=null);result.push(gap);
  }
  result.push(row);
 }
 return result;
}
export function boardSelection(raw:unknown,assets:Asset[]):string[]{
 if(!Array.isArray(raw))return assets.map(a=>a.id);
 const allowed=new Set(assets.map(a=>a.id));return [...new Set(raw.filter((id):id is string=>typeof id==='string'&&allowed.has(id)))];
}
