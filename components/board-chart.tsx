"use client";
import {useEffect,useId,useMemo,useRef,useState} from 'react';
import {ArrowUpRight,ChartNoAxesCombined} from 'lucide-react';
import {ResponsiveContainer,LineChart,Line,XAxis,YAxis,CartesianGrid,Tooltip,ReferenceLine} from 'recharts';
import {chartRows} from '@/lib/monitoring/board';
import type {EvidenceLine} from '@/lib/monitoring/evidence';
import type {Snapshot} from '@/lib/monitoring/types';
import {formatNumber} from '@/lib/monitoring/rules';
import {shortTime,fullTime} from './monitoring-chart';

const palette=['#b5a1ff','#63d9bd','#6dbafb','#f3bc77','#ef8eae','#cbd376','#c39beb','#71cad2'];
const hash=(key:string)=>[...key].reduce((n,c)=>(n*31+c.charCodeAt(0))>>>0,0);

export function BoardChart({title,subtitle,lines,snapshot,syncId,unit,warning,onOpen}:{title:string;subtitle?:string;lines:EvidenceLine[];snapshot:Snapshot;syncId:string;unit?:string;warning?:number;onOpen?:()=>void}){
 const [hidden,setHidden]=useState<string[]>([]),[visible,setVisible]=useState(false);
 const ref=useRef<HTMLElement>(null),descriptionId=useId();
 const colors=new Map(lines.map((line,index)=>[line.key,palette[index%palette.length]])),lineColor=(line:EvidenceLine)=>colors.get(line.key)!;
 useEffect(()=>{if(!ref.current)return;const observer=new IntersectionObserver(([entry])=>setVisible(entry.isIntersecting),{rootMargin:'300px'});observer.observe(ref.current);return()=>observer.disconnect()},[]);
 const units=[...new Set(lines.map(l=>l.unit))],data=useMemo(()=>visible?chartRows(lines,snapshot.step):[],[visible,lines,snapshot.step]);
 const liveValue=(line:EvidenceLine)=>{const point=line.points.at(-1),asset=snapshot.assets?.find(a=>line.key.endsWith(`:${a.id}`));return asset?.enabled&&point?.value!=null&&snapshot.end-point.time<=45?point.value:null};
 return <article ref={ref} className="board-chart" aria-label={`${title} 그래프`} aria-describedby={descriptionId}>
  <header><div><h4>{title}</h4>{subtitle&&<p>{subtitle}</p>}</div>{onOpen&&<button className="icon-button" aria-label={`${title} 상세`} onClick={onOpen}><ArrowUpRight size={15}/></button>}</header>
  <div className="board-chart-meta" id={descriptionId}><span>{units.length>1?`왼쪽 ${units[0]} · 오른쪽 ${units[1]}`:units[0]||unit||'관측 대기'}</span><span>{lines.length?`${lines.length}개 시계열 · 동일 시간축`:'관측값 없음'}</span></div>
  <div className="board-plot">
   {!lines.length?<div className="board-empty-plot"><ChartNoAxesCombined size={24}/><span>아직 관측값이 없습니다</span><small>연결·수집 권한·지원 지표를 확인하세요.</small></div>:!visible?<div className="board-plot-placeholder">그래프 표시 준비 중</div>:<ResponsiveContainer width="100%" height="100%" minWidth={0}>
    <LineChart data={data} syncId={syncId} syncMethod="value" margin={{top:8,right:8,left:0,bottom:0}}>
     <CartesianGrid vertical={false} stroke="#2b3040" strokeDasharray="3 5"/>
     <XAxis dataKey="time" type="number" domain={[snapshot.start,snapshot.end]} allowDataOverflow scale="time" tickFormatter={shortTime} minTickGap={52} stroke="#8791a8" tick={{fontSize:11}}/>
     {units.map((u,index)=><YAxis key={u} yAxisId={u} orientation={index?'right':'left'} width={48} tickFormatter={formatNumber} tick={{fontSize:11}} stroke="#8791a8" domain={u==='0/1'?[0,1]:[(minimum:number)=>Math.min(0,minimum),'auto']} ticks={u==='0/1'?[0,1]:undefined}/>)}
     <Tooltip labelFormatter={v=>fullTime(Number(v))} formatter={(value,name,item)=>{const line=lines[Number(String(item.dataKey).slice(1))];return [value==null?'미관측':`${formatNumber(Number(value))} ${line?.unit||''}`,name]}} contentStyle={{background:'#171d29',border:'1px solid #41485d',borderRadius:8,fontSize:11,maxWidth:420}}/>
     {warning!==undefined&&units.length===1&&<ReferenceLine yAxisId={units[0]} y={warning} stroke="#b48d5b" strokeDasharray="4 5"/>}
     {lines.map((line,i)=><Line key={line.key} name={line.name} dataKey={`v${i}`} yAxisId={line.unit} stroke={lineColor(line)} strokeDasharray={['','5 3','2 3'][hash(line.metricId)%3]} strokeWidth={1.8} dot={false} connectNulls={false} isAnimationActive={false} hide={hidden.includes(line.key)}/>)}
    </LineChart>
   </ResponsiveContainer>}
  </div>
  {lines.length>0&&<div className="board-legend" aria-label={`${title} 시계열 표시`}>
   {lines.map(line=>{const value=liveValue(line);return <button key={line.key} title={line.name} aria-pressed={!hidden.includes(line.key)} className={hidden.includes(line.key)?'muted':''} onClick={()=>setHidden(old=>old.includes(line.key)?old.filter(k=>k!==line.key):[...old,line.key])}><i style={{background:lineColor(line)}}/><span>{line.name}</span><strong>{value===null?'—':formatNumber(value)}<small>{line.unit==='0/1'?'(0 / 1)':line.unit}</small></strong></button>})}
  </div>}
 </article>;
}
