"use client";
import {useMemo,useState} from 'react';
import {ArrowRight,RefreshCw,Search,Server,Database,Layers,Globe,Activity,Monitor,Radio} from 'lucide-react';
import {Dialog,DialogContent,DialogDescription,DialogHeader,DialogTitle} from '@/components/ui/dialog';
import {Tabs,TabsList,TabsTrigger} from '@/components/ui/tabs';
import {metrics,metricById,type Metric} from '@/lib/monitoring/catalog';
import {detectIncidents,formatNumber} from '@/lib/monitoring/rules';
import {infrastructureKind,infrastructureLabels,infrastructureName,infrastructureMetrics,primaryMetricIds,type InfrastructureKind} from '@/lib/monitoring/infrastructure';
import type {Target,Snapshot,Incident} from '@/lib/monitoring/types';
import {MetricPanel,shortTime,stateLabel} from './monitoring-chart';
import {useMonitoring} from './use-monitoring';

export const infrastructureIcons={application:Server,database:Database,cache:Layers,frontend:Monitor,probe:Globe,host:Server,monitoring:Activity,other:Layers};
export function InfrastructureIcon({kind}:{kind:InfrastructureKind}){const Icon=infrastructureIcons[kind];return <span className={`infra-icon ${kind}`}><Icon size={20}/></span>}
export function InfrastructureDashboard({target,range,refreshSeconds,onClose,onMetric,onIncident}:{target:Target;range:string;refreshSeconds:number;onClose:()=>void;onMetric:(metric:Metric,snapshot:Snapshot|null,instance:string)=>void;onIncident:(incident:Incident,snapshot:Snapshot|null,instance:string)=>void}){
 const {snapshot,loading,error,reload}=useMonitoring(range,target.instance,true,refreshSeconds);
 const [view,setView]=useState('overview'),[search,setSearch]=useState(''),[page,setPage]=useState(0);
 const kind=infrastructureKind(target);
 const supportedIds=new Set(infrastructureMetrics(kind).map(m=>m.id));
 const observedIds=new Set(snapshot?.metrics.filter(m=>m.state==='ok').map(m=>m.id)||[]);
 const profile=metrics.filter(m=>supportedIds.has(m.id)||observedIds.has(m.id));
 const results=useMemo(()=>new Map(snapshot?.metrics.map(m=>[m.id,m])||[]),[snapshot]);
 const incidents=useMemo(()=>snapshot?detectIncidents(snapshot):[],[snapshot]);
 const currentTarget=snapshot?.targets.find(t=>t.instance===target.instance&&t.job===target.job);
 const health=currentTarget?.health??target.health;
 const primary=primaryMetricIds(kind).map(id=>metricById.get(id)!).filter(Boolean);
 const overviewIds=kind==='database'?['db-connections','db-buffer','db-rollback','db-locks']:kind==='cache'?['redis-memory','redis-hit','redis-expired','redis-blocked']:primary.map(m=>m.id);
 const filtered=profile.filter(m=>`${m.title} ${m.id} ${m.description}`.toLowerCase().includes(search.toLowerCase()));
 const graphMetrics=view==='overview'?overviewIds.map(id=>metricById.get(id)!):filtered.slice(page*6,(page+1)*6);
 return <Dialog open onOpenChange={open=>{if(!open)onClose()}}><DialogContent className="infra-dialog"><DialogHeader className="infra-modal-header"><div className="infra-heading"><InfrastructureIcon kind={kind}/><div><div className="eyebrow">{infrastructureLabels[kind]} DASHBOARD</div><DialogTitle>{infrastructureName(target)}</DialogTitle></div></div><DialogDescription><span className="mono">{target.instance}</span><span className={`health-label ${snapshot&&!snapshot.connected?'unknown':health==='up'?'healthy':'unhealthy'}`}>{snapshot&&!snapshot.connected?'상태 확인 불가':health==='up'?'수집 중':'수집 실패'}</span></DialogDescription></DialogHeader>
 <div className="infra-modal-body">
 <div className="modal-context"><span>{range==='900'?'최근 15분':range==='3600'?'최근 1시간':range==='21600'?'최근 6시간':'최근 24시간'} · 선택한 대상만 조회</span><button className="filter-button" onClick={()=>void reload()} disabled={loading}><RefreshCw size={13} className={loading?'spin':''}/>{loading?'불러오는 중':'새로고침'}</button></div>
 {error&&<p className="status-banner" role="alert">{error}</p>}
 {snapshot&&!snapshot.connected&&<p className="status-banner">수집기에 연결할 수 없어 현재 값을 확인하지 못했습니다.</p>}
 <div className="infra-kpis">{primary.map(m=>{const result=results.get(m.id);return <button key={m.id} onClick={()=>onMetric(m,snapshot,target.instance)}><span>{m.title}</span><strong>{result?.latest!=null?(m.unit==='0/1'?(result.latest===1?'연결됨':'연결 실패'):formatNumber(result.latest)):'—'}{m.unit!=='0/1'&&<small>{m.unit}</small>}</strong><small className={result?.state==='ok'?'mint':''}>{loading&&!snapshot?'수집 중…':stateLabel[result?.state||'missing']}</small></button>})}</div>
 {!!incidents.length&&<div className="asset-incidents"><div><Radio size={15}/> 이 인프라의 이벤트 <span>{incidents.length}</span></div>{incidents.slice(0,3).map(i=><button key={i.id} onClick={()=>onIncident(i,snapshot,target.instance)}><span className={'severity-dot '+i.severity}/><strong>{i.title}</strong><span>{i.value} {i.unit}</span><ArrowRight size={14}/></button>)}{incidents.length>3&&<p>추가 {incidents.length-3}개 · 상세 지표에서 관련 값을 확인하세요.</p>}</div>}
 <div className="modal-viewbar"><Tabs value={view} onValueChange={v=>{setView(v);setPage(0)}}><TabsList><TabsTrigger value="overview">핵심 지표</TabsTrigger><TabsTrigger value="metrics">상세 지표 {profile.length}</TabsTrigger></TabsList></Tabs><span>{snapshot?`${shortTime(snapshot.end)} KST 기준`:'첫 관측 대기'}</span></div>
 {view==='metrics'&&<label className="search-field modal-search"><Search size={16}/><input aria-label="인프라 지표 검색" placeholder="이 인프라의 지표 검색" value={search} onChange={e=>{setSearch(e.target.value);setPage(0)}}/></label>}
 <div className="chart-grid asset-charts" aria-busy={loading}>{graphMetrics.map(m=><MetricPanel key={m.id} metric={m} result={results.get(m.id)} onOpen={()=>onMetric(m,snapshot,target.instance)}/>)}</div>
 {view==='metrics'&&!filtered.length&&<div className="empty-search">검색한 지표가 없습니다.</div>}
 {view==='metrics'&&filtered.length>6&&<div className="pagination"><span>{filtered.length}개 중 {page*6+1}–{Math.min((page+1)*6,filtered.length)}</span><button disabled={page===0} onClick={()=>setPage(p=>p-1)}>이전</button><button disabled={(page+1)*6>=filtered.length} onClick={()=>setPage(p=>p+1)}>다음</button></div>}
 <p className="modal-footnote">수집 성공은 서비스 정상과 다릅니다. 값이 없는 지표는 미관측으로 표시합니다.</p>
 </div></DialogContent></Dialog>;
}
