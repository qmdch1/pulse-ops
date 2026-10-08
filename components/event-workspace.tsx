"use client";
import {useState,useEffect} from 'react';
import {EvidenceCharts} from './evidence-charts';
import {scopeSnapshot,controlRequest} from '@/lib/monitoring/assets';
import {ArrowRight,Search,ChevronDown,ChevronUp,Radio} from 'lucide-react';
import {Tabs,TabsList,TabsTrigger} from '@/components/ui/tabs';
import {eventRules,ruleState} from '@/lib/monitoring/rule-catalog';
import {metricById} from '@/lib/monitoring/catalog';
import {formatNumber} from '@/lib/monitoring/rules';
import type {Snapshot,Incident} from '@/lib/monitoring/types';
import {fullTime} from './monitoring-chart';

export const severityLabels={critical:'긴급',warning:'주의',notice:'평가 중'};
export const kindLabels={detected:'이상 감지',predicted:'위험 예측',expiring:'만료',collection:'수집 상태'};
export function IncidentRows({incidents,onOpen,limit=5}:{incidents:Incident[];onOpen:(i:Incident)=>void;limit?:number}){
 return <div className="compact-incidents">{incidents.slice(0,limit).map(i=><button key={i.id} onClick={()=>onOpen(i)}><span className={'event-severity '+i.severity}>{severityLabels[i.severity]}</span><div><strong>{i.title}</strong><small>{i.scope==='선택 범위'?'전체 인프라':i.scope} · {kindLabels[i.kind]}</small></div><span className="incident-value">{i.value} <small>{i.unit}</small></span><ArrowRight size={15}/></button>)}</div>;
}
export function EventWorkspace({snapshot,incidents,onIncident}:{snapshot:Snapshot|null;incidents:Incident[];onIncident:(i:Incident)=>void}){
 const [tab,setTab]=useState('rules'),[search,setSearch]=useState(''),[filter,setFilter]=useState('all'),[page,setPage]=useState(0),[expanded,setExpanded]=useState<string|null>(null);
 const [assetFilter,setAssetFilter]=useState('all');
 const scoped=snapshot&&assetFilter!=='all'?scopeSnapshot(snapshot,assetFilter):snapshot,scopedIncidents=incidents.filter(i=>assetFilter==='all'||i.assetId===assetFilter);
 const visible=eventRules.filter(r=>`${r.title} ${r.condition} ${r.metricIds.map(id=>metricById.get(id)?.title).join(' ')}`.toLowerCase().includes(search.toLowerCase())&&(filter==='all'||ruleState(r,scoped,scopedIncidents)===filter));
 const display=visible.slice(page*10,(page+1)*10);
 const result=new Map(snapshot?.metrics.map(m=>[m.id,m])||[]);
 return <>
 <div className="event-flow"><div><span>01</span><strong>지표 관측</strong><small>응답·오류·리소스</small></div><ArrowRight size={17}/><div><span>02</span><strong>조건 교차 확인</strong><small>임계값과 지속 시간</small></div><ArrowRight size={17}/><div><span>03</span><strong>대시보드에 표시</strong><small>긴급·주의 이벤트</small></div></div>
 <Tabs value={tab} onValueChange={setTab}><TabsList className="workspace-tabs"><TabsTrigger value="rules">감지 규칙 <span>{eventRules.length}</span></TabsTrigger><TabsTrigger value="active">현재 이벤트 <span>{incidents.length}</span></TabsTrigger><TabsTrigger value="history">연결·수집 이력</TabsTrigger></TabsList></Tabs>
 {tab==='rules'&&<><div className="rule-target-filter"><span>이벤트 평가 대상</span><select className="native-select" aria-label="이벤트 평가 인프라" value={assetFilter} onChange={e=>{setAssetFilter(e.target.value);setPage(0);setExpanded(null)}}><option value="all">등록한 전체 인프라 · 대상별 개별 평가</option>{snapshot?.assets?.map(a=><option key={a.id} value={a.id}>{a.name}</option>)}</select></div><div className="list-toolbar"><label className="search-field"><Search size={16}/><input aria-label="이벤트 규칙 검색" value={search} onChange={e=>{setSearch(e.target.value);setPage(0)}} placeholder="이벤트 이름, 지표, 조건 검색"/></label><select className="native-select" aria-label="이벤트 규칙 상태" value={filter} onChange={e=>{setFilter(e.target.value);setPage(0)}}><option value="all">모든 상태</option><option value="firing">대시보드 표시 중</option><option value="waiting">조건 평가 중</option><option value="missing">관측 부족</option></select></div>
 <div className="rules-table-wrap"><table className="rules-table"><thead><tr><th>이벤트 규칙</th><th>함께 보는 지표</th><th>발생 조건 · 지속 시간</th><th>대시보드 표시</th><th aria-label="상세"/></tr></thead><tbody>{display.map(rule=>{const state=ruleState(rule,scoped,scopedIncidents),open=expanded===rule.id;return <RuleRows key={rule.id} rule={rule} state={state} open={open} toggle={()=>setExpanded(open?null:rule.id)} result={result} snapshot={snapshot} initialAssetId={assetFilter==='all'?undefined:assetFilter}/>})}</tbody></table>{!visible.length&&<p className="empty-search">일치하는 규칙이 없습니다.</p>}</div>
 <div className="pagination"><span>{visible.length?`${visible.length}개 중 ${page*10+1}–${Math.min((page+1)*10,visible.length)}`:'0개'}</span><button disabled={page===0} onClick={()=>{setPage(p=>p-1);setExpanded(null)}}>이전</button><button disabled={(page+1)*10>=visible.length} onClick={()=>{setPage(p=>p+1);setExpanded(null)}}>다음</button></div><p className="table-explanation">조건 평가 중은 현재 표시할 이벤트가 없다는 뜻입니다. 정상 판정은 아니며, 지속 시간과 표본이 부족하면 이벤트를 만들지 않습니다.</p></>}
 {tab==='active'&&<div className="surface active-events"><div className="surface-heading"><h2><Radio size={17}/> 지금 확인할 이벤트</h2><span>{incidents.length}개</span></div>{incidents.length?<IncidentRows incidents={incidents} onOpen={onIncident} limit={100}/>:<p className="empty-search">현재 표시할 이벤트가 없습니다.</p>}<p className="table-explanation">등록한 인프라별 지표와 지속 시간으로 계산합니다. 서로 다른 대상의 지표를 합쳐 이벤트를 만들지 않습니다.</p></div>}
 {tab==='history'&&<CollectionHistory snapshot={snapshot}/>}
 </>;
}
function RuleRows({rule,state,open,toggle,snapshot,initialAssetId}:{rule:typeof eventRules[number];state:ReturnType<typeof ruleState>;open:boolean;toggle:()=>void;result:Map<string,Snapshot['metrics'][number]>;snapshot:Snapshot|null;initialAssetId?:string}){
 return <><tr className={open?'expanded':''}><td><button className="rule-name" onClick={toggle} aria-expanded={open}><strong>{rule.title}</strong><small>{kindLabels[rule.kind]}</small></button></td><td><div className="rule-metrics">{rule.metricIds.length?rule.metricIds.map(id=><span key={id}>{metricById.get(id)?.title||id}{!rule.requiredIds.includes(id)&&<small>참고</small>}</span>):<span>관리 서비스 연결 상태</span>}</div></td><td><p>{rule.condition}</p><small className="condition-duration">{rule.duration}</small></td><td><span className={'rule-status '+state}>{state==='firing'?`${severityLabels[rule.severity]} 표시 중`:state==='missing'?'관측 부족':state==='pending'?'지속 시간 확인 중':'조건 평가 중'}</span><small className="delivery-label">충족 시 {severityLabels[rule.severity]} 이벤트</small></td><td><button className="icon-button" aria-label={`${rule.title} 규칙 상세`} aria-expanded={open} onClick={toggle}>{open?<ChevronUp size={15}/>:<ChevronDown size={15}/>}</button></td></tr>{open&&<tr className="rule-expanded"><td colSpan={5}><div className="rule-detail"><p><ArrowRight size={15}/><span><strong>{rule.condition}</strong> · {rule.duration}. 조건과 지속 시간을 충족하면 대시보드 상단에 {severityLabels[rule.severity]} 이벤트로 표시합니다. 참고 지표는 원인 확인용입니다.</span></p></div><EvidenceCharts snapshot={snapshot} metricIds={rule.metricIds} initialAssetId={initialAssetId}/></td></tr>}</>;
}

function CollectionHistory({snapshot}:{snapshot:Snapshot|null}){
 const [rows,setRows]=useState<{assetId:string;action:string;status:string;at:string}[]>([]),[error,setError]=useState('');
 useEffect(()=>{let disposed=false;controlRequest<typeof rows>('audit').then(data=>{if(!disposed)setRows(data.filter(r=>r.action.startsWith('collection.')||r.action.startsWith('terminal.')))}).catch(()=>{if(!disposed)setError('연결 이력을 읽을 수 없습니다.')});return()=>{disposed=true}},[]);
 return <div className="rules-table-wrap"><table className="history-table"><thead><tr><th>시각 · KST</th><th>인프라</th><th>변화</th></tr></thead><tbody>{rows.map((r,i)=><tr key={i}><td>{fullTime(r.at)}</td><td>{snapshot?.assets?.find(a=>a.id===r.assetId)?.name||'삭제된 인프라'}</td><td>{({'collection.connected':'수집 연결됨','collection.error':'수집 실패','terminal.open':'터미널 열기','terminal.close':'터미널 종료'} as Record<string,string>)[r.action]||r.action} · {r.status}</td></tr>)}</tbody></table>{(!rows.length||error)&&<p className="empty-search">{error||'아직 기록된 연결 변화가 없습니다.'}</p>}<p className="table-explanation">영구 저장된 연결·수집·터미널 활동입니다. 화면에서 평가하는 상관·예측 이벤트의 발생·해제 이력과는 구분합니다.</p></div>
}
