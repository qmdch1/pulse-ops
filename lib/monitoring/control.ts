import {metrics} from './catalog';
import type {Asset} from './assets';
import type {Snapshot,MetricResult} from './types';

export async function controlFetch(path:string,init?:RequestInit){
 const base=process.env.CONTROL_PLANE_URL,token=process.env.CONTROL_API_TOKEN;
 if(!base||!token)throw new Error('Go 인프라 관리 서비스 연결이 필요합니다.');
 return fetch(`${base.replace(/\/$/,'')}/${path}`,{...init,cache:'no-store',headers:{...init?.headers,Authorization:`Bearer ${token}`},signal:AbortSignal.timeout(40000)});
}
type NativeObservation={assetId:string;time:number;values:Record<string,number>;error?:string};
type NativeResponse={assets:Asset[];observations:NativeObservation[];evaluation?:NativeObservation[];end:number;step:number};
export function nativeSnapshot(data:NativeResponse,range:number,selected:string,mode:Snapshot['mode']):Snapshot{
 const assets=data.assets,observations=data.observations;
 const byAsset=new Map<string,NativeObservation[]>();for(const o of observations){const rows=byAsset.get(o.assetId)||[];rows.push(o);byAsset.set(o.assetId,rows)}
 return {mode,connected:true,collectedAt:new Date(data.end*1000).toISOString(),start:data.end-range,end:data.end,step:data.step,assets,evaluationMetrics:data.evaluation?nativeSnapshot({...data,observations:data.evaluation,evaluation:undefined,step:15},3600,selected,mode).metrics:undefined,targets:assets.filter(a=>selected==='all'||a.id===selected).map(a=>({instance:a.id,job:a.kind,health:a.status==='connected'?'up':a.status,lastScrape:a.lastSeen,lastError:a.message})),alerts:[],history:[],grafanaUrl:null,metrics:metrics.map(m=>{
  const series=assets.filter(a=>selected==='all'||a.id===selected).map(a=>{
   const samples=byAsset.get(a.id)||[];
   if(!samples.some(o=>Number.isFinite(o.values[m.id])))return null;
   const points=samples.map(o=>({time:o.time,value:Number.isFinite(o.values[m.id])?o.values[m.id]:null}));
   if(points.length&&data.end-points.at(-1)!.time>data.step*1.5)points.push({time:data.end,value:null});
   return {labels:{assetId:a.id,instance:a.id,name:a.name,kind:a.kind},points};
  }).filter(s=>s!==null);
  const last=series.length===1?series[0].points.at(-1):null;
  const enabled=assets.some(a=>a.enabled&&series.some(s=>s.labels.assetId===a.id));
  const fresh=series.some(s=>{const p=s.points.at(-1);return p&&p.value!==null&&data.end-p.time<=45});
  return {id:m.id,series,latest:enabled&&last&&data.end-last.time<=45?last.value:null,state:(!series.length?'missing':enabled&&fresh?'ok':'stale') as MetricResult['state'],message:'등록 인프라 직접 수집 · 애플리케이션 변화율/백분위는 5분 표본, 호스트·DB·Redis 변화율은 수집 간격(15초) 기준. 계측이 없으면 미관측.'};
 })};
}
export async function readNativeSnapshot(range:number,selected:string):Promise<Snapshot>{
 const mode=process.env.MONITORING_MODE==='production'?'production':process.env.MONITORING_MODE==='test'?'test':'unconfigured';
 try{const response=await controlFetch(`observations?range=${range}&asset=${encodeURIComponent(selected)}`);if(!response.ok)throw new Error();return nativeSnapshot(await response.json(),range,selected,mode)}
 catch{const end=Math.floor(Date.now()/1000);return {mode,connected:false,collectedAt:new Date().toISOString(),start:end-range,end,step:15,assets:[],metrics:[],targets:[],alerts:[],grafanaUrl:null,message:'인프라 관리 서비스에 연결할 수 없습니다. 로컬 Docker 환경의 Go 서비스 상태를 확인하세요.'}}
}
