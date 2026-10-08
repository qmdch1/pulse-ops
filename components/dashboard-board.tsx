"use client";
import {useEffect,useId,useMemo,useRef,useState} from 'react';
import {Activity,ArrowDown,ArrowRight,Check,CheckCheck,ChevronDown,ChevronUp,Columns3,Layers,Radio,Search,Server,SlidersHorizontal,X} from 'lucide-react';
import {assetLabels,scopeSnapshot,statusLabels,type Asset} from '@/lib/monitoring/assets';
import {metrics,groupLabels,type Metric,type Group} from '@/lib/monitoring/catalog';
import {boardSelection,chartGroups,comparisonLines,comparisonSnapshot,eventMetricIds,groupEvents,metricsForAsset,observedForAsset,relatedAssets,type EventGroup} from '@/lib/monitoring/board';
import {ruleById} from '@/lib/monitoring/rule-catalog';
import type {Snapshot,Incident} from '@/lib/monitoring/types';
import {InfrastructureIcon} from './infrastructure-dashboard';
import {BoardChart} from './board-chart';
import {kindLabels,severityLabels} from './event-workspace';

type View='assets'|'compare';
type FocusRequest={incident:Incident;sequence:number}|null;
const preferenceKey='pulse-dashboard-board-v1';
const initialMetrics=['p99','db-probe','redis-probe','node-cpu','memory-host'];

export function DashboardBoard({snapshot,incidents,onManage,onRules,onMetric,focusRequest}:{snapshot:Snapshot|null;incidents:Incident[];onManage:()=>void;onRules:()=>void;onMetric:(metric:Metric,snapshot:Snapshot)=>void;focusRequest:FocusRequest}){
 const [selection,setSelection]=useState<string[]|null>(null),[view,setView]=useState<View>('assets'),[showMissing,setShowMissing]=useState(false),[search,setSearch]=useState(''),[kind,setKind]=useState('all'),[group,setGroup]=useState<Group|'all'>('all');
 const [compareMetrics,setCompareMetrics]=useState(initialMetrics),[metricSearch,setMetricSearch]=useState(''),[focus,setFocus]=useState<EventGroup|null>(null),[showAllEvents,setShowAllEvents]=useState(false),[prefsReady,setPrefsReady]=useState(false);
 const graphRef=useRef<HTMLElement>(null),focusRef=useRef<HTMLDivElement>(null),sync=useId();
 const assets=snapshot?.assets||[],ids=boardSelection(selection,assets),selected=assets.filter(a=>ids.includes(a.id));
 const eventGroups=useMemo(()=>groupEvents(incidents),[incidents]);
 const groupsShown=showAllEvents?eventGroups:eventGroups.slice(0,6),liveFocus=eventGroups.find(g=>g.id===focus?.id);
 useEffect(()=>{try{const pref=JSON.parse(localStorage.getItem(preferenceKey)||'{}');if(Array.isArray(pref.ids))setSelection(pref.ids.filter((v:unknown)=>typeof v==='string'));if(pref.view==='assets'||pref.view==='compare')setView(pref.view);if(typeof pref.showMissing==='boolean')setShowMissing(pref.showMissing);if(Array.isArray(pref.metrics))setCompareMetrics(pref.metrics.filter((v:unknown)=>typeof v==='string'&&metrics.some(m=>m.id===v)))}catch{}setPrefsReady(true)},[]);
 useEffect(()=>{if(prefsReady)try{localStorage.setItem(preferenceKey,JSON.stringify({ids:selection,view,showMissing,metrics:compareMetrics}))}catch{}},[prefsReady,selection,view,showMissing,compareMetrics]);
 useEffect(()=>{if(focusRequest){setFocus(groupEvents([focusRequest.incident])[0]);requestAnimationFrame(()=>focusRef.current?.scrollIntoView({behavior:'smooth',block:'start'}))}},[focusRequest]);
 const focusGroup=(event:EventGroup)=>{setFocus(current=>current?.id===event.id?null:event);requestAnimationFrame(()=>focusRef.current?.scrollIntoView({behavior:'smooth',block:'start'}))};
 const toggle=(id:string)=>setSelection(old=>{const current=boardSelection(old,assets);return current.includes(id)?current.filter(v=>v!==id):[...current,id]});
 const inspectRelated=(related:string[])=>{setSelection(related);setView('assets');setSearch('');setKind('all');setGroup('all');requestAnimationFrame(()=>graphRef.current?.scrollIntoView({behavior:'smooth',block:'start'}))};
 const assetSearch=assets.filter(a=>(kind==='all'||a.kind===kind)&&`${a.name} ${a.address} ${assetLabels[a.kind]}`.toLowerCase().includes(search.toLowerCase()));
 const metricCandidates=snapshot?metrics.filter(m=>selected.some(a=>metricsForAsset(snapshot,a).some(candidate=>candidate.id===m.id))):[];
 const displayedMetrics=metricCandidates.filter(m=>(group==='all'||m.group===group)&&(showMissing||selected.some(a=>snapshot&&observedForAsset(snapshot,m.id,a.id))));
 const metricPicker=metricCandidates.filter(m=>`${m.title} ${m.unit}`.toLowerCase().includes(metricSearch.toLowerCase()));
 const compareLines=snapshot?comparisonLines(snapshot,ids,compareMetrics):[],compareGroups=chartGroups(compareLines);
 const perAsset=snapshot?selected.map(a=>({asset:a,metrics:metricsForAsset(snapshot,a).filter(m=>(group==='all'||m.group===group)&&(showMissing||observedForAsset(snapshot,m.id,a.id)))})):[];
 const count=view==='assets'?perAsset.reduce((n,row)=>n+row.metrics.length,0):displayedMetrics.length;
 const active=assets.filter(a=>a.enabled&&a.status==='connected').length;

 return <div className="monitoring-board">
  <div className="board-summary"><div><span>등록 인프라</span><strong>{assets.length}<small>개</small></strong></div><div><span>수집 중</span><strong>{active}<small>/ {assets.length}</small></strong></div><div><span>지금 확인할 이벤트</span><strong className={eventGroups.length?'board-attention':''}>{eventGroups.length}<small>종류 · {incidents.length}건</small></strong></div><button onClick={onManage}>인프라 관리 <ArrowRight size={15}/></button></div>

  <section className="board-events" aria-label="지금 확인할 이벤트">
   <div className="board-section-heading"><div><span className="board-overline">01 / ATTENTION</span><h2><Radio size={18}/> 지금 확인할 이벤트</h2><p>이벤트를 선택하면 발생 대상과 연결된 인프라의 그래프가 함께 펼쳐집니다.</p></div><button className="plain-link" onClick={onRules}>감지 규칙 보기 <ArrowRight size={14}/></button></div>
   {groupsShown.length?<div className="board-event-grid">{groupsShown.map(event=><button key={event.id} className={`board-event-card ${event.severity} ${focus?.id===event.id?'selected':''}`} aria-expanded={focus?.id===event.id} aria-label={`${event.title} 관련 그래프`} onClick={()=>focusGroup(event)}><span className="board-event-meta"><span className={`event-severity ${event.severity}`}>{severityLabels[event.severity]}</span><small>{kindLabels[event.kind]}</small></span><strong>{event.title}</strong><span className="board-event-bottom"><span>{event.assetIds.length?`${event.assetIds.length}개 인프라 · ${event.incidents.length}건`:'관리 서비스 상태'}</span>{focus?.id===event.id?<ChevronUp size={16}/>:<ChevronDown size={16}/>}</span></button>)}</div>:<div className="board-quiet"><CheckCheck size={21}/><div><strong>{snapshot?.connected?'현재 발생한 이벤트가 없습니다':'인프라 연결을 기다리고 있습니다'}</strong><p>{snapshot?.connected?'아래 그래프에서 상태를 살펴보세요. 미관측과 정상은 구분합니다.':'인프라 관리에서 대상을 등록하고 연결을 시작하세요.'}</p></div></div>}
   {eventGroups.length>6&&<button className="board-more" onClick={()=>setShowAllEvents(v=>!v)}>{showAllEvents?'이벤트 접기':`나머지 ${eventGroups.length-6}개 이벤트 보기`}<ChevronDown size={14}/></button>}
   <div ref={focusRef} className="board-event-anchor">{focus&&snapshot&&<EventFocus group={liveFocus||focus} resolved={!liveFocus} snapshot={snapshot} syncId={sync} onClose={()=>setFocus(null)} onRelated={inspectRelated}/>}</div>
  </section>

  <section ref={graphRef} className="board-explorer" aria-label="인프라 그래프">
   <div className="board-section-heading"><div><span className="board-overline">02 / INFRASTRUCTURE</span><h2><Activity size={19}/> 인프라 그래프</h2><p>인프라를 클릭해 집중하거나, 체크박스로 여러 대를 선택해 비교하세요.</p></div><div className="board-view-switch" role="group" aria-label="그래프 보기 방식"><button aria-pressed={view==='assets'} onClick={()=>setView('assets')}><Layers size={15}/>인프라별 보기</button><button aria-pressed={view==='compare'} onClick={()=>setView('compare')}><Columns3 size={15}/>여러 인프라 비교</button></div></div>
   <div className="board-selector">
    <div className="board-selector-toolbar"><label className="search-field"><Search size={15}/><input aria-label="그래프 인프라 검색" placeholder="이름, 주소, 종류로 검색" value={search} onChange={e=>setSearch(e.target.value)}/></label><select className="native-select" aria-label="그래프 인프라 종류" value={kind} onChange={e=>setKind(e.target.value)}><option value="all">모든 종류</option>{Object.entries(assetLabels).filter(([k])=>assets.some(a=>a.kind===k)).map(([k,label])=><option key={k} value={k}>{label}</option>)}</select><button className="plain-link" onClick={()=>setSelection(null)}>전체 선택</button><button className="plain-link" onClick={()=>setSelection([])}>선택 해제</button></div>
    <div className="board-asset-picker" aria-label="그래프에 표시할 인프라">{assetSearch.map(a=><div className={`board-asset-chip ${ids.includes(a.id)?'selected':''}`} key={a.id}><input type="checkbox" aria-label={`${a.name} 그래프 선택`} checked={ids.includes(a.id)} onChange={()=>toggle(a.id)}/><button title={`${a.name}만 보기`} aria-label={`${a.name}만 보기`} onClick={()=>setSelection([a.id])}><InfrastructureIcon kind={a.kind}/><span><strong>{a.name}</strong><small>{assetLabels[a.kind]}</small></span><i className={a.status==='connected'?'connected':a.status==='error'?'error':''}/></button></div>)}{!assetSearch.length&&<p className="board-empty-note">{assets.length?'검색 결과가 없습니다. 선택된 인프라는 그대로 유지됩니다.':'등록된 인프라가 없습니다.'}</p>}</div>
    <div className="board-selection-note"><span><Check size={13}/>{selected.length} / {assets.length}개 선택</span><span>선택한 인프라의 전체 지표를 아래에 표시합니다.</span>{selected.length>1&&view==='assets'&&<button className="plain-link" onClick={()=>setView('compare')}>같은 지표끼리 비교 <ArrowRight size={13}/></button>}</div>
   </div>

   {selected.length>0&&snapshot?<>
    {view==='compare'&&<section className="board-composer"><header><div><h3><SlidersHorizontal size={17}/> 서로 다른 지표 함께 비교</h3><p>예: API P99 + DB 읽기 응답 + 서버 CPU. 비율이나 백분위를 합산하지 않고 각 시계열을 유지합니다.</p></div><details className="board-metric-menu"><summary>비교 지표 선택 <span>{compareMetrics.length}</span><ChevronDown size={14}/></summary><div className="board-metric-options"><label className="search-field"><Search size={14}/><input aria-label="비교 지표 검색" value={metricSearch} onChange={e=>setMetricSearch(e.target.value)} placeholder="지표 검색"/></label><div className="board-metric-option-actions"><button onClick={()=>setCompareMetrics(initialMetrics)}>추천 지표</button><button onClick={()=>setCompareMetrics([])}>모두 해제</button></div>{metricPicker.map(m=><label key={m.id}><input type="checkbox" checked={compareMetrics.includes(m.id)} onChange={e=>setCompareMetrics(old=>e.target.checked?[...old,m.id]:old.filter(id=>id!==m.id))}/><span>{m.title}<small>{m.unit}</small></span></label>)}</div></details></header><div className="board-compare-tags">{compareMetrics.map(id=>{const m=metrics.find(m=>m.id===id);return m?<button key={id} onClick={()=>setCompareMetrics(old=>old.filter(v=>v!==id))} aria-label={`${m.title} 비교에서 제외`}>{m.title}<X size={12}/></button>:null})}</div>{compareGroups.length?<div className="board-chart-grid comparison-charts">{compareGroups.map((lines,index)=><BoardChart key={lines.map(l=>l.key).join('|')} title={`통합 비교 ${index+1}`} subtitle={lines.map(l=>l.unit).filter((v,i,a)=>a.indexOf(v)===i).join(' + ')} lines={lines} snapshot={snapshot} syncId={sync}/>)}</div>:<div className="board-empty-note">선택한 인프라에서 관측한 지표를 선택하세요. 미관측 시계열은 만들지 않습니다.</div>}<p className="board-footnote">단위는 최대 두 축, 선은 차트당 최대 8개로 나눕니다. 모든 비교 차트는 조회 기간과 마우스 위치를 공유합니다.</p></section>}

    <div className="board-graph-toolbar"><div><strong>{view==='assets'?'인프라별 전체 그래프':'같은 지표로 인프라 비교'}</strong><span>{count}개 {view==='assets'?'그래프':'지표'}</span></div><div><select className="native-select" aria-label="그래프 지표 영역" value={group} onChange={e=>setGroup(e.target.value as Group|'all')}><option value="all">모든 지표 영역</option>{Object.entries(groupLabels).map(([key,title])=><option key={key} value={key}>{title}</option>)}</select><label className="board-toggle"><input type="checkbox" checked={showMissing} onChange={e=>setShowMissing(e.target.checked)}/>미관측 지표 포함</label></div></div>
    {view==='assets'?perAsset.map(({asset,metrics:assetMetrics})=><section className="board-asset-section" key={asset.id}><header><InfrastructureIcon kind={asset.kind}/><div><h3>{asset.name}</h3><p>{assetLabels[asset.kind]} · {asset.address||'주소 미입력'}</p></div><span className={`health-label ${asset.status==='connected'?'healthy':asset.status==='error'?'unhealthy':'unknown'}`}>{statusLabels[asset.status]}</span><span>{assetMetrics.length}개 그래프</span></header>{assetMetrics.length?<div className="board-chart-grid">{assetMetrics.map(metric=><BoardChart key={metric.id} title={metric.title} subtitle={groupLabels[metric.group]} unit={metric.unit} warning={metric.warning} lines={comparisonLines(snapshot,[asset.id],[metric.id])} snapshot={snapshot} syncId={sync} onOpen={()=>onMetric(metric,scopeSnapshot(snapshot,asset.id))}/>)}</div>:<div className="board-empty-note">{showMissing?'해당 영역의 지표가 없습니다.':'관측된 그래프가 없습니다. 연결 상태를 확인하거나 ‘미관측 지표 포함’을 선택하세요.'}</div>}</section>):<div className="board-chart-grid">{displayedMetrics.flatMap(metric=>{const lines=comparisonLines(snapshot,ids,[metric.id]),chunks=lines.length?Array.from({length:Math.ceil(lines.length/8)},(_,i)=>lines.slice(i*8,(i+1)*8)):[[]];return chunks.map((part,index)=><BoardChart key={`${metric.id}:${index}`} title={metric.title+(chunks.length>1?` · ${index+1}/${chunks.length}`:'')} subtitle={groupLabels[metric.group]} unit={metric.unit} warning={metric.warning} lines={part} snapshot={snapshot} syncId={sync} onOpen={()=>onMetric(metric,comparisonSnapshot(snapshot,ids))}/>);})}</div>}
    <p className="board-footnote">비어 있는 구간은 미관측입니다. 일시정지·오래된 값은 최근 값으로 표시하지 않으며, 이벤트 조건은 인프라별로 독립 평가합니다.</p>
   </>:<div className="board-select-empty"><Server size={28}/><strong>{assets.length?'보고 싶은 인프라를 선택하세요':'첫 인프라를 연결하세요'}</strong><p>{assets.length?'한 대를 클릭하거나 여러 체크박스를 선택할 수 있습니다.':'인프라 관리에서 주소와 접속 정보를 등록합니다.'}</p><button className="filter-button" onClick={assets.length?()=>setSelection(null):onManage}>{assets.length?'전체 인프라 선택':'인프라 관리로 이동'}<ArrowRight size={14}/></button></div>}
  </section>
 </div>;
}

function EventFocus({group,resolved,snapshot,syncId,onClose,onRelated}:{group:EventGroup;resolved:boolean;snapshot:Snapshot;syncId:string;onClose:()=>void;onRelated:(ids:string[])=>void}){
 const related=relatedAssets(snapshot,group.assetIds),rule=ruleById.get(group.id),metricIds=eventMetricIds(group,related),lines=comparisonLines(snapshot,related.map(a=>a.id),metricIds),groups=chartGroups(lines);
 const missing=group.metricIds.filter(id=>!lines.some(l=>l.metricId===id&&group.assetIds.some(assetId=>l.key.endsWith(`:${assetId}`))));
 return <section className="board-event-focus" aria-label={`${group.title} 관련 그래프 모음`}>
  <header><div><span className="board-overline">EVENT INVESTIGATION</span><h3>{group.title}</h3><p>{resolved?'현재 평가에서는 이 이벤트가 표시되지 않습니다. 선택했던 대상의 그래프를 확인할 수 있습니다.':rule?`${rule.condition} · ${rule.duration}`:group.incidents[0].summary}</p></div><button className="icon-button" onClick={onClose} aria-label="이벤트 그래프 닫기"><X size={17}/></button></header>
  <div className="board-related">{related.map(a=><span className={group.assetIds.includes(a.id)?'affected':''} key={a.id}>{a.name}<small>{group.assetIds.includes(a.id)?'발생 대상':'연결된 인프라'}</small></span>)}{!related.length&&<span>등록 인프라와 관측 상태를 확인하세요.</span>}</div>
  <div className="board-event-evidence">{group.incidents.map(i=><div key={i.id}><strong>{i.scope}</strong><span>{i.value} {i.unit}</span><small>{i.evidence.join(' · ')}</small></div>)}</div>
  <div className="board-focus-caption"><strong>발생 지표 + 연결된 인프라의 상태</strong><span>{lines.length}개 시계열 · {groups.length}개 그래프</span></div>
  {groups.length?<div className="board-chart-grid">{groups.map((part,index)=><BoardChart key={part.map(l=>l.key).join('|')} title={`관련 지표 ${index+1}`} subtitle={[...new Set(part.map(l=>l.unit))].join(' + ')} lines={part} snapshot={snapshot} syncId={syncId}/>)}</div>:<div className="board-empty-note">관련 관측값이 없습니다. 연결 실패나 수집 중단일 수 있습니다.</div>}
  {missing.length>0&&<p className="board-footnote">발생 지표 중 미관측: {missing.map(id=>metrics.find(m=>m.id===id)?.title||id).join(' · ')}. 값을 0으로 대체하지 않습니다.</p>}
  <footer><p>{group.incidents[0].steps[0]} 관련 인프라의 그래프는 원인 확인에 사용하며 발생 조건은 대상별로 평가합니다.</p>{related.length>0&&<button className="filter-button" onClick={()=>onRelated(related.map(a=>a.id))}>관련 인프라의 전체 그래프 <ArrowDown size={14}/></button>}</footer>
 </section>;
}
