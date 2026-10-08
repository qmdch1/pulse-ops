"use client";
import {useId,useState} from 'react';
import {ResponsiveContainer,LineChart,Line,XAxis,YAxis,CartesianGrid,Tooltip,Legend} from 'recharts';
import {evidenceGroups,type EvidenceLine} from '@/lib/monitoring/evidence';
import {metricById} from '@/lib/monitoring/catalog';
import type {Snapshot} from '@/lib/monitoring/types';
import {shortTime,fullTime} from './monitoring-chart';
import {formatNumber} from '@/lib/monitoring/rules';
const colors=['#a395fa','#61d8bb','#f2bb72','#71b9fa','#f28aab','#d7d77a','#e29cff','#76d4e8'];
export function EvidenceCharts({snapshot,metricIds,initialAssetId}:{snapshot:Snapshot|null;metricIds:string[];initialAssetId?:string}){
 const available=snapshot?.assets||[],[selection,setSelection]=useState(initialAssetId||available.find(a=>a.enabled&&snapshot?.metrics.some(m=>metricIds.includes(m.id)&&m.series.some(s=>s.labels.assetId===a.id)))?.id||available[0]?.id||'');
 const assetId=available.some(a=>a.id===selection)?selection:available[0]?.id||'',asset=available.find(a=>a.id===assetId);
 const groups=snapshot&&assetId?evidenceGroups(snapshot,metricIds,assetId):[];
 const missing=metricIds.filter(id=>!groups.flat().some(l=>l.metricId===id&&l.key.endsWith(`:${assetId}`)));
 const sync=useId();
 const observedStart=Math.max(snapshot?.start||0,Math.min(...groups.flat().flatMap(l=>l.points.map(p=>p.time))));
 return <div className="evidence-charts"><div className="evidence-heading"><div><strong>조건의 근거 그래프</strong><small>관측 시작부터 표시 · 같은 단위는 한 축 · 최대 두 단위를 함께 비교</small></div><select className="native-select" aria-label="근거 그래프 인프라" value={assetId} onChange={e=>setSelection(e.target.value)}>{available.map(a=><option key={a.id} value={a.id}>{a.name}</option>)}</select></div>{asset&&<p className="evidence-scope">평가 대상 <b>{asset.name}</b>{asset.dependencies.length>0&&<> · 연결 관계 {asset.dependencies.map(id=>available.find(a=>a.id===id)?.name||id).join(', ')}</>}<span>관련 인프라의 그래프는 원인 확인용입니다. 서로 다른 서버의 지표를 임의로 섞어 조건을 충족시키지 않습니다.</span></p>}{groups.map((lines,i)=><CombinedChart key={`${assetId}:${i}`} lines={lines} sync={sync} start={Number.isFinite(observedStart)?observedStart:snapshot!.start} end={snapshot!.end} step={snapshot!.step}/>)}{!groups.length&&<div className="evidence-empty">이 대상의 관련 관측값이 아직 없습니다. 인프라를 연결하거나 필요한 앱 계측을 추가하세요.</div>}{missing.length>0&&<p className="evidence-missing">미관측: {missing.map(id=>metricById.get(id)?.title||id).join(' · ')}. 변화율·P99는 연결 후 5분 표본이 쌓이면 표시됩니다.</p>}</div>
}
function CombinedChart({lines,sync,start,end,step}:{lines:EvidenceLine[];sync:string;start:number;end:number;step:number}){
 const [hidden,setHidden]=useState<string[]>([]),units=[...new Set(lines.map(l=>l.unit))];
 const rows=new Map<number,Record<string,number|null>>();
 for(const line of lines)for(const p of line.points){const time=Math.floor(p.time/step)*step;const row=rows.get(time)||{time};row[line.key]=p.value;rows.set(time,row)}
 const data=[...rows.values()].sort((a,b)=>Number(a.time)-Number(b.time));
 return <div className="evidence-plot"><div className="combined-caption"><span>{units.join(' + ')}</span><small>{lines.length}개 시계열 · 범례를 눌러 표시 전환</small></div><div style={{height:280,minWidth:0}}><ResponsiveContainer width="100%" height="100%"><LineChart data={data} syncId={sync} syncMethod="value" margin={{top:12,right:8,left:0,bottom:4}}><CartesianGrid stroke="#252b38" strokeDasharray="3 6" vertical={false}/><XAxis dataKey="time" type="number" domain={[start,end]} scale="time" tickFormatter={shortTime} stroke="#6e778b" minTickGap={45} tick={{fontSize:12}}/>{units.map((unit,i)=><YAxis key={unit} yAxisId={unit} orientation={i===0?'left':'right'} width={58} stroke={i===0?'#a395fa':'#61d8bb'} tickFormatter={formatNumber} tick={{fontSize:12}} label={{value:unit,position:'insideTopLeft',offset:0,fontSize:12}} domain={unit==='0/1'?[0,1]:['ms','%','req/s','개','회/s'].includes(unit)?[0,'auto']:['auto','auto']} ticks={unit==='0/1'?[0,1]:undefined}/>)}<Tooltip labelFormatter={value=>fullTime(Number(value))} formatter={(value,name,item)=>[`${formatNumber(Number(value))} ${lines.find(l=>l.key===String(item.dataKey))?.unit||''}`,name]} contentStyle={{background:'#151b27',border:'1px solid #394151',borderRadius:10,fontSize:12}}/><Legend onClick={item=>{const key=String(item.dataKey);setHidden(h=>h.includes(key)?h.filter(k=>k!==key):[...h,key])}} wrapperStyle={{fontSize:12,paddingTop:12,cursor:'pointer'}}/>{lines.map((line,i)=><Line key={line.key} name={line.name} dataKey={line.key} yAxisId={line.unit} stroke={colors[i%colors.length]} strokeWidth={1.8} strokeDasharray={i>=colors.length?'5 3':undefined} dot={false} connectNulls={false} isAnimationActive={false} hide={hidden.includes(line.key)}/>)}</LineChart></ResponsiveContainer></div></div>
}
