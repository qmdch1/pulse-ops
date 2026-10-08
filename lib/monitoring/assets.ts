import type {Snapshot,MetricResult,Incident} from './types';
import {detectIncidents} from './rules.ts';
export type AssetKind='server'|'application'|'postgres'|'redis'|'http';
export type Asset={id:string;name:string;kind:AssetKind;address:string;port:number;username:string;database:string;tlsMode:string;os:string;metricsUrl:string;environment:string;ssh:{host:string;port:number;username:string;jumpId:string;fingerprint:string};dependencies:string[];enabled:boolean;version:number;status:string;lastSeen:string;message:string;hasPassword:boolean;hasSshPassword:boolean;hasPrivateKey:boolean;hasPassphrase:boolean};
export type AssetInput=Partial<Asset>&{password?:string;sshPassword?:string;privateKey?:string;passphrase?:string};
export const assetLabels:Record<AssetKind,string>={server:'서버',application:'애플리케이션',postgres:'PostgreSQL',redis:'Redis · 캐시',http:'웹 · HTTP'};
export const statusLabels:Record<string,string>={draft:'등록 초안',connecting:'연결 확인 중',connected:'수집 중',paused:'일시정지',error:'연결 확인 필요'};
export function scopeSnapshot(snapshot:Snapshot,id:string):Snapshot{
 const asset=snapshot.assets?.find(a=>a.id===id);
 return {...snapshot,evaluationMetrics:snapshot.evaluationMetrics?.map(m=>({...m,series:m.series.filter(s=>s.labels.assetId===id)})),assets:asset?[asset]:[],targets:snapshot.targets.filter(t=>t.instance===id),metrics:snapshot.metrics.map(m=>{
  const series=m.series.filter(s=>s.labels.assetId===id),point=series[0]?.points.at(-1),fresh=!!point&&snapshot.end-point.time<=45&&asset?.enabled;
  return {...m,series,latest:fresh?point.value:null,state:(!series.length?'missing':fresh?'ok':'stale') as MetricResult['state']};
 }),alerts:[]};
}
export function registeredIncidents(snapshot:Snapshot):Incident[]{
 if(!snapshot.assets)return detectIncidents(snapshot);
 if(!snapshot.connected)return detectIncidents(snapshot);
 const evaluated=snapshot.evaluationMetrics?{...snapshot,metrics:snapshot.evaluationMetrics,step:15,start:snapshot.end-3600}:snapshot;
 return snapshot.assets.filter(a=>a.enabled).flatMap(a=>{
  const found=detectIncidents(scopeSnapshot(evaluated,a.id)).map(i=>({...i,id:`${i.id}:${a.id}`,ruleId:i.id,assetId:a.id,scope:a.name}));
  if(a.status==='error')found.unshift({id:`connection:${a.id}`,ruleId:'connection',assetId:a.id,title:'등록 인프라 연결 실패',severity:'critical',kind:'collection',summary:a.message,metricIds:['targets-down'],evidence:[a.message],steps:['등록 주소와 인증 정보를 확인하세요.','점프 서버와 대상의 접근 경로를 확인하세요.'],value:'실패',unit:'',status:'firing',scope:a.name});
  return found;
 });
}
export async function controlRequest<T=Asset>(path:string,method='GET',body?:unknown):Promise<T>{
 const response=await fetch(`/api/control/${path}`,{method,headers:body?{'Content-Type':'application/json'}:undefined,body:body?JSON.stringify(body):undefined});
 const data=await response.json() as T&{error?:string};if(!response.ok)throw new Error(data.error||'요청을 처리하지 못했습니다.');return data;
}
