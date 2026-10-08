import {metrics, scopedQuery} from './catalog';
import type {Snapshot, MetricResult, MetricSeries, Alert, Target, AlertHistory} from './types';
type PromResult={status:string;data:any;error?:string};
const cache=new Map<string,{expires:number,promise:Promise<Snapshot>}>();
let activeSnapshots=0;
function sourceUrl(){
  const raw=process.env.PROMETHEUS_URL;if(!raw)return null;
  const url=new URL(raw);
  if(!['http:','https:'].includes(url.protocol)||url.username||url.password||url.search||url.hash)throw new Error('Invalid source configuration');
  return url;
}
async function query(path:string,params:Record<string,string>={}):Promise<PromResult>{
  const base=sourceUrl();if(!base)throw new Error('Source not configured');
  base.pathname=base.pathname.replace(/\/$/,'')+path;
  for(const [key,value]of Object.entries(params))base.searchParams.set(key,value);
  const headers:Record<string,string>={Accept:'application/json'};
  if(process.env.PROMETHEUS_TOKEN)headers.Authorization=`Bearer ${process.env.PROMETHEUS_TOKEN}`;
  const response=await fetch(base,{headers,signal:AbortSignal.timeout(7000),redirect:'manual',cache:'no-store'});
  if(!response.ok)throw new Error('Upstream request failed');
  const reader=response.body?.getReader();if(!reader)throw new Error('Empty upstream response');
  const parts:Uint8Array[]=[];let size=0;
  try{while(true){const {done,value}=await reader.read();if(done)break;size+=value.byteLength;if(size>1_500_000)throw new Error('Upstream response exceeds limit');parts.push(value);}}
  catch(error){await reader.cancel();throw error;}
  const buffer=new Uint8Array(size);let offset=0;for(const part of parts){buffer.set(part,offset);offset+=part.length;}
  const data=JSON.parse(new TextDecoder().decode(buffer));if(data.status!=='success')throw new Error('Prometheus query failed');return data;
}
const safeLabels=(labels:Record<string,string>)=>Object.fromEntries(Object.entries(labels).filter(([k])=>['instance','job','route','version','le','cookie_name','domain','layer'].includes(k)).map(([k,v])=>[k,String(v).slice(0,200)]));
export async function readSnapshot(range:number,instance:string):Promise<Snapshot>{
  const key=`${range}:${instance}`;const cached=cache.get(key);if(cached&&cached.expires>Date.now())return cached.promise;
  if(activeSnapshots>=3)throw new Error('Collector busy');
  if(cache.size>30)cache.clear();
  const promise=collect(range,instance).finally(()=>{activeSnapshots--});activeSnapshots++;
  cache.set(key,{expires:Date.now()+20_000,promise});
  try{return await promise;}catch(error){cache.delete(key);throw error;}
}
async function collect(range:number,instance:string):Promise<Snapshot>{
  const end=Math.floor(Date.now()/1000/10)*10,start=end-range,step=Math.max(10,Math.ceil(range/120));
  const mode=process.env.MONITORING_MODE==='production'?'production':process.env.MONITORING_MODE==='test'?'test':'unconfigured';
  let grafanaUrl:string|null=null;
  try{const u=new URL(process.env.GRAFANA_PUBLIC_URL||'');if(['http:','https:'].includes(u.protocol)&&!u.username&&!u.password)grafanaUrl=u.origin+u.pathname.replace(/\/$/,'');}catch{}
  const base:Snapshot={mode,connected:false,collectedAt:new Date().toISOString(),start,end,step,metrics:[],targets:[],alerts:[],grafanaUrl};
  if(!process.env.PROMETHEUS_URL)return {...base,message:'데이터 소스가 아직 연결되지 않았습니다. 연결 안내에서 수집기를 설정하세요.'};
  let targets:Target[]=[];
  try{
    const response=await query('/api/v1/targets',{state:'active'});
    targets=response.data.activeTargets.slice(0,100).map((t:any)=>({instance:String(t.labels.instance||'').slice(0,200),job:String(t.labels.job||'').slice(0,80),health:t.health,lastScrape:t.lastScrape,lastError:t.lastError?'대상 수집 실패':undefined}));
  }catch(error){console.warn('Prometheus connection failure:',error instanceof Error?error.message:'unknown');return {...base,message:'Prometheus에 연결하지 못했습니다. 수집기 상태와 서버 측 연결 설정을 확인하세요.'};}
  if(instance!=='all'&&!targets.some(t=>t.instance===instance))throw new Error('Unknown target');
  const results:MetricResult[]=new Array(metrics.length);let next=0;
  await Promise.all(Array.from({length:6},async()=>{while(next<metrics.length){const i=next++,m=metrics[i];try{
    const r=await query('/api/v1/query_range',{query:scopedQuery(m.query,instance),start:String(start),end:String(end),step:String(step),limit:'8'});
    const series:MetricSeries[]=r.data.result.slice(0,8).map((v:any)=>({labels:safeLabels(v.metric),points:v.values.slice(-121).map(([t,x]:[number,string])=>({time:t,value:Number.isFinite(Number(x))?Number(x):null}))}));
    const fresh=series.map(s=>s.points.at(-1)).filter(p=>p&&p.time>=end-step*1.5&&p.value!==null);
    const latest=fresh.length?Math.max(...fresh.map(p=>p!.value!)):null;
    results[i]={id:m.id,series,latest,state:latest!==null?'ok':series.some(s=>s.points.some(p=>p.value!==null))?'stale':'missing',truncated:r.data.result.length>=8};
  }catch{results[i]={id:m.id,state:'error',latest:null,series:[],message:'지표 조회 실패'};}}}));
  let alerts:Alert[]=[];
  try{const r=await query('/api/v1/alerts');alerts=r.data.alerts.slice(0,100).filter((a:any)=>instance==='all'||a.labels.instance===instance).map((a:any)=>({name:String(a.labels.alertname).slice(0,120),state:a.state,severity:a.labels.severity||'warning',instance:a.labels.instance||'전체',activeAt:a.activeAt,summary:String(a.annotations?.summary||a.labels.alertname).slice(0,300)}));}catch{}
  const history:AlertHistory[]=[];
  try{
    const r=await query('/api/v1/query_range',{query:scopedQuery('ALERTS{alertstate="firing",__SCOPE__}',instance),start:String(start),end:String(end),step:String(step),limit:'50'});
    for(const item of r.data.result.slice(0,50)){
      let segment:AlertHistory|null=null,last=0;
      for(const [stamp,val]of item.values){if(Number(val)!==1)continue;if(!segment||stamp-last>step*1.5){if(segment){segment.endedAt=last+step;history.push(segment)}segment={name:String(item.metric.alertname).slice(0,120),instance:String(item.metric.instance||'전체').slice(0,200),severity:item.metric.severity||'warning',startedAt:stamp,endedAt:null,resolutionSeconds:step};}last=stamp;}
      if(segment){if(last<end-step)segment.endedAt=last+step;history.push(segment)}
    }
  }catch{}
  return {...base,connected:true,targets,metrics:results,alerts,history:history.sort((a,b)=>b.startedAt-a.startedAt).slice(0,100)};
}

