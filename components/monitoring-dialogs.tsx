"use client";
import {useEffect,useState} from 'react';
import {Activity,Check,Download,ExternalLink,Info,Plus,Server,ShieldCheck,Terminal} from 'lucide-react';
import {Sheet,SheetContent,SheetHeader,SheetTitle,SheetDescription} from '@/components/ui/sheet';
import {Dialog,DialogContent,DialogHeader,DialogTitle,DialogDescription} from '@/components/ui/dialog';
import {Tabs,TabsList,TabsTrigger,TabsContent} from '@/components/ui/tabs';
import {metricById,type Metric} from '@/lib/monitoring/catalog';
import type {Snapshot,Incident} from '@/lib/monitoring/types';
import {formatNumber} from '@/lib/monitoring/rules';
import {Plot,exportJson,fullTime,stateLabel} from './monitoring-chart';
export function DetailSheet({incident,metric,snapshot,instance,onClose}:{incident:Incident|null;metric:Metric|null;snapshot:Snapshot|null;instance:string;onClose:()=>void}){
 const results=new Map(snapshot?.metrics.map(m=>[m.id,m])||[]);
 return <Sheet open={!!incident||!!metric} onOpenChange={v=>{if(!v)onClose()}}><SheetContent className="detail-sheet"><SheetHeader><div className="eyebrow">{incident?'INVESTIGATION':'METRIC INSIGHT'}</div><SheetTitle>{incident?.title||metric?.title}</SheetTitle><SheetDescription>{incident?.summary||metric?.description}</SheetDescription></SheetHeader><div className="sheet-body">{incident&&<><div className="detail-status"><span className={'event-severity '+incident.severity}>{incident.severity==='critical'?'긴급':'주의'}</span><span>선택 범위 · {snapshot?fullTime(snapshot.collectedAt):''}</span></div><h4>판단 근거</h4><ul className="evidence-list">{incident.evidence.map(e=><li key={e}><Activity size={15}/>{e}</li>)}</ul><h4>권장 확인 순서</h4><ol className="runbook">{incident.steps.map((s,i)=><li key={s}><span>{String(i+1).padStart(2,'0')}</span>{s}</li>)}</ol><div className="notice-box"><Info size={17}/><p>자동 복구 작업은 실행하지 않습니다. 근거를 확인하고 서비스별 복구 절차에 따라 판단하세요.</p></div></>}
 {(incident?incident.metricIds:metric?[metric.id]:[]).map(id=>{const m=metricById.get(id);return m?<div className="panel detail-chart" key={id}><div className="panel-title"><h3>{m.title}</h3><span>{results.get(id)?.latest!=null?formatNumber(results.get(id)!.latest!):'—'} {m.unit}</span></div><Plot metric={m} result={results.get(id)} compact/><p className="detail-description">{m.description}</p></div>:null})}
 {metric&&<><h4>관측 계약</h4><dl className="definition-list"><dt>수집 방식</dt><dd>등록 인프라 직접 수집</dd><dt>단위</dt><dd>{metric.unit}</dd><dt>신선도</dt><dd>{stateLabel[results.get(metric.id)?.state||'missing']}</dd><dt>해석</dt><dd>측정값 없음은 정상 0과 다릅니다. 경계값은 서비스 환경에 맞춰 검토해야 합니다.</dd></dl><h4>측정 기준</h4><p className="detail-description">{results.get(metric.id)?.message||'이 인프라에 필요한 계측이 없으면 미관측으로 표시합니다.'}</p>{metric.id==='cookie-expiry'&&<div className="notice-box"><ShieldCheck size={18}/><p>쿠키 원문, 세션 ID, 비밀번호를 저장하지 않습니다. 만료 시각을 알 수 없는 세션 쿠키는 임의의 만료 시간을 만들지 않습니다.</p></div>}</>}
 <div className="sheet-actions"><button className="primary-button" onClick={()=>exportJson('pulse-investigation.json',{exportedAt:new Date().toISOString(),incident,metric,observation:snapshot})}><Download size={15}/> 조사 근거 내보내기</button>{snapshot?.grafanaUrl&&<a href={snapshot.grafanaUrl+'/d/pulse-ops'} target="_blank" rel="noreferrer" className="filter-button">Grafana에서 보기<ExternalLink size={14}/></a>}</div></div></SheetContent></Sheet>
}
