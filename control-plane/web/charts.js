import {comparisonLines,chartRows,chartGroups} from './lib/board.js';
import {recipeKey,readLayout,mergeLayout,activeGroups,removeGroup,refreshPeriod,refreshDue} from './lib/chart-layout.js';
import {formatNumber} from './lib/rules.js';
import {axisScale,formatAxisTick,axisRangeLabel} from './lib/chart-scale.js';
import {metricById} from './lib/catalog.js';
import {esc,icon,shortTime,fullTime,preference,savePreference} from './ui.js';

const palette=['#b5a1ff','#63d9bd','#6dbafb','#f3bc77','#ef8eae','#cbd376','#c39beb','#71cad2'];
const format=v=>v===null||v===undefined?'—':formatNumber(v);
const clockFormat=new Intl.DateTimeFormat('en-GB',{hour:'2-digit',minute:'2-digit',second:'2-digit',timeZone:'Asia/Seoul'});
const groups=new Set();
const layout=readLayout(preference('pulse-chart-layout-v1',{}));
let layoutFrame=0,drag=null;
let defaultRefresh=30;
const saveLayout=()=>savePreference('pulse-chart-layout-v1',layout);
const scope=grid=>grid.root.closest('dialog')||document.querySelector('main');
export function chartInteractionActive(){return !!drag||!!document.activeElement?.matches('.chart-refresh input')||!!document.activeElement?.closest('.chart-merge-picker')}
export function nextRefreshSeconds(fallback){const custom=[...groups].flatMap(g=>g.charts.filter(c=>c.visible).map(c=>refreshPeriod(layout.periods[c.key])).filter(Boolean));return custom.length?Math.min(...custom,fallback||3600):fallback}
export function updateAllCharts(snapshot,seconds,force=false){defaultRefresh=seconds;for(const grid of groups)grid.update(snapshot,force)}
function changed(){saveLayout();queueLayout();window.dispatchEvent(new Event('pulse:chart-refresh-changed'))}
function announce(message){let live=document.querySelector('#chart-announcement');if(!live){live=document.createElement('div');live.id='chart-announcement';live.className='chart-announcement';live.setAttribute('role','status');document.body.append(live)}live.textContent=message;live.hidden=false;clearTimeout(announce.timer);announce.timer=setTimeout(()=>live.hidden=true,4000)}
function linesFor(recipe,snapshot){const all=comparisonLines(snapshot,recipe.assetIds,recipe.metricIds);return recipe.keys?all.filter(l=>recipe.keys.includes(l.key)):all}
function expand(grid){return grid.baseRecipes.flatMap(recipe=>{const key=recipeKey(recipe);if(!layout.split.includes(key))return [{...recipe,layoutKey:key}];const lines=linesFor(recipe,grid.snapshot);if(lines.length<2)return [{...recipe,layoutKey:key}];return lines.map(line=>{const part={title:line.name,subtitle:'분리한 시계열 · 원래 시간축 유지',assetIds:recipe.assetIds,metricIds:[line.metricId],keys:[line.key],unit:line.unit,parentKey:key};return {...part,layoutKey:recipeKey(part)}})})}
function queueLayout(){if(layoutFrame)return;layoutFrame=requestAnimationFrame(()=>{layoutFrame=0;applyLayouts()})}
function applyLayouts(){const scopes=new Set([...groups].map(scope));for(const area of scopes){const grids=[...groups].filter(g=>scope(g)===area),recipes=new Map(),owners=new Map();for(const grid of grids){grid.expanded=expand(grid);for(const recipe of grid.expanded){recipes.set(recipe.layoutKey,recipe);owners.set(recipe.layoutKey,grid)}}const merged=activeGroups(layout.groups,new Set(recipes.keys())),hidden=new Set(merged.flatMap(g=>g.slice(1))),targets=new Map(merged.map(g=>[g[0],g]));for(const grid of grids){const resolved=grid.expanded.filter(r=>!hidden.has(r.layoutKey)).flatMap(r=>{const keys=targets.get(r.layoutKey);if(!keys)return[r];const parts=keys.map(k=>recipes.get(k)),lines=[...new Map(parts.flatMap(p=>linesFor(p,grid.snapshot)).map(l=>[l.key,l])).values()],chunks=chartGroups(lines);if(!chunks.length)return[r];return chunks.map((chunk,i)=>({...r,title:[...new Set(chunk.map(l=>metricById.get(l.metricId)?.title||l.metricId))].join(' · ')+(chunks.length>1?' · '+(i+1):''),subtitle:parts.map(p=>p.title).join(' + '),assetIds:[...new Set(parts.flatMap(p=>p.assetIds))],metricIds:[...new Set(chunk.map(l=>l.metricId))],keys:chunk.map(l=>l.key),metric:undefined,warning:undefined,mergeKeys:keys,layoutKey:r.layoutKey,chunk:i}))});const signature=JSON.stringify(resolved.map(r=>[r.layoutKey,r.keys,r.mergeKeys,r.parentKey,r.chunk]));if(signature!==grid.layoutSignature){grid.layoutSignature=signature;grid.reset(resolved)}}}}
function merge(source,target){if(scope(source.grid)!==scope(target.grid)||source.key===target.key)return;layout.groups=mergeLayout(layout.groups,source.key,target.key);changed();announce('그래프를 합쳤습니다. 시간과 단위는 각각 유지됩니다.');requestAnimationFrame(()=>{for(const grid of groups)for(const chart of grid.charts)if(chart.key===target.key)chart.node.classList.add('chart-joined')})}
function split(chart){if(chart.recipe.mergeKeys){layout.groups=removeGroup(layout.groups,chart.key);announce('합치기를 해제하고 원래 그래프로 분리했습니다.')}else if(chart.recipe.parentKey){layout.split=layout.split.filter(k=>k!==chart.recipe.parentKey);layout.groups=layout.groups.filter(g=>!g.some(k=>chart.grid.expanded.some(r=>r.parentKey===chart.recipe.parentKey&&r.layoutKey===k)));announce('분리 전 그래프로 되돌렸습니다.')}else{layout.split=[...new Set([...layout.split,chart.key])];announce('각 시계열을 개별 그래프로 분리했습니다.')}changed()}
function startDrag(chart,event){if(event.button!==0||drag)return;const handle=event.currentTarget,startX=event.clientX,startY=event.clientY;let preview,target,moved=false,raf=0,x=startX,y=startY;const area=scope(chart.grid);handle.setPointerCapture(event.pointerId);drag={chart};
 const paint=()=>{if(!drag)return;if(preview){preview.style.left=`${x+16}px`;preview.style.top=`${y+16}px`;const node=document.elementFromPoint(x,y)?.closest('.board-chart');const candidate=node?._pulseChart;target?.node.classList.remove('chart-drop-target');target=candidate&&candidate!==chart&&candidate.key!==chart.key&&scope(candidate.grid)===area?candidate:null;target?.node.classList.add('chart-drop-target');const edge=65,speed=y<edge?-13:y>innerHeight-edge?13:0;if(speed){if(area?.tagName==='DIALOG')area.scrollTop+=speed;else window.scrollBy(0,speed)}}raf=requestAnimationFrame(paint)};
 const move=e=>{x=e.clientX;y=e.clientY;if(!moved&&Math.hypot(x-startX,y-startY)>5){moved=true;chart.suppressClick=true;preview=chart.node.cloneNode(true);preview.className='board-chart chart-drag-preview';preview.setAttribute('aria-hidden','true');preview.inert=true;preview.style.width=Math.min(chart.node.clientWidth,420)+'px';const original=chart.node.querySelectorAll('canvas');preview.querySelectorAll('canvas').forEach((c,i)=>{c.width=original[i].width;c.height=original[i].height;c.getContext('2d').drawImage(original[i],0,0)});area?.tagName==='DIALOG'?area.append(preview):document.body.append(preview);chart.node.classList.add('chart-lifted');document.body.classList.add('chart-dragging');raf=requestAnimationFrame(paint)}};
 const end=(e,cancel=false)=>{cancelAnimationFrame(raf);if(moved&&!cancel){target?.node.classList.remove('chart-drop-target');const candidate=document.elementFromPoint(e.clientX,e.clientY)?.closest('.board-chart')?._pulseChart;target=candidate&&candidate!==chart&&candidate.key!==chart.key&&scope(candidate.grid)===area?candidate:null;}handle.removeEventListener('pointermove',move);handle.removeEventListener('pointerup',up);handle.removeEventListener('pointercancel',cancelled);window.removeEventListener('keydown',key);window.removeEventListener('blur',cancelled);try{handle.releasePointerCapture(event.pointerId)}catch{}preview?.remove();target?.node.classList.remove('chart-drop-target');chart.node.classList.remove('chart-lifted');document.body.classList.remove('chart-dragging');drag=null;if(moved&&!cancel&&target)merge(chart,target);else if(moved)announce('합치기를 취소했습니다. 그래프가 원래 위치로 돌아왔습니다.')};
 const up=e=>end(e),cancelled=e=>end(e,true),key=e=>{if(e.key==='Escape'){e.preventDefault();end(e,true)}};drag.cancel=cancelled;handle.addEventListener('pointermove',move);handle.addEventListener('pointerup',up);handle.addEventListener('pointercancel',cancelled);window.addEventListener('keydown',key);window.addEventListener('blur',cancelled);
}
let cursor=null,frame=0;
function broadcast(time){cursor=time;if(frame)return;frame=requestAnimationFrame(()=>{frame=0;for(const grid of groups)for(const chart of grid.charts)if(chart.visible)chart.drawCursor()})}

// Canvas and tooltip work only near the viewport. Lazy batches keep long
// multi-infrastructure boards from constructing thousands of charts at once.
export class ChartGrid {
 constructor(root,recipes,snapshot,onDetail){
  this.root=root;this.baseRecipes=recipes;this.recipes=recipes;this.snapshot=snapshot;this.onDetail=onDetail;this.charts=[];this.count=0;
  this.observer=new IntersectionObserver(entries=>{for(const entry of entries){const chart=this.charts.find(c=>c.node===entry.target);if(chart){const becameVisible=!chart.visible&&entry.isIntersecting;chart.visible=entry.isIntersecting;if(chart.visible)chart.draw();if(becameVisible&&refreshPeriod(layout.periods[chart.key]))window.dispatchEvent(new Event('pulse:chart-refresh-changed'))}}},{rootMargin:'250px'});
  this.sentinel=document.createElement('div');this.sentinel.className='chart-sentinel';this.sentinel.setAttribute('aria-live','polite');
  this.loader=new IntersectionObserver(entries=>{if(entries.some(e=>e.isIntersecting))this.append()},{rootMargin:'500px'});
  groups.add(this);queueLayout();
 }
 reset(recipes){this.observer.disconnect();this.loader.disconnect();for(const c of this.charts)c.destroy();this.charts=[];this.count=0;this.recipes=recipes;this.root.replaceChildren(this.sentinel);this.root.hidden=!recipes.length;this.append();if(this.count<recipes.length)this.loader.observe(this.sentinel)}
 append(){
  const next=this.recipes.slice(this.count,this.count+8);if(!next.length)return;
  for(const recipe of next){const chart=new CanvasChart(recipe,this.snapshot,this.onDetail,this);this.charts.push(chart);this.root.insertBefore(chart.node,this.sentinel);this.observer.observe(chart.node)}
  this.count+=next.length;
  this.sentinel.textContent='';
  if(this.count===this.recipes.length){this.loader.disconnect();this.sentinel.remove()}
 }
 update(snapshot,force=false){this.snapshot=snapshot;for(const chart of this.charts)if(refreshDue(chart.lastUpdate,Date.now(),layout.periods[chart.key],defaultRefresh,force||!snapshot.connected))chart.update(snapshot)}
 destroy(){this.loader.disconnect();this.observer.disconnect();for(const chart of this.charts)chart.destroy();groups.delete(this)}
}
class CanvasChart {
 constructor(recipe,snapshot,onDetail,grid){
  this.grid=grid;this.recipe=recipe;this.key=recipe.layoutKey||recipeKey(recipe);this.hidden=new Set();this.visible=false;
  this.node=document.createElement('article');this.node.className='board-chart';this.node.setAttribute('aria-label',`${recipe.title} 그래프`);
  this.node._pulseChart=this;
  this.node.innerHTML=`<header><button class="chart-grip" aria-label="${esc(recipe.title)} 그래프 잡기" title="그래프 합치기">⠿</button><div><h4 title="${esc(recipe.subtitle||'')}">${onDetail&&recipe.metric?`<button class="chart-title" data-detail aria-label="${esc(recipe.title)} 상세">${esc(recipe.title)}</button>`:esc(recipe.title)}</h4></div><button class="icon-button" data-options aria-label="${esc(recipe.title)} 그래프 설정" aria-expanded="false">${icon('settings',15)}</button></header><div class="board-chart-meta"></div><div class="canvas-plot"><canvas role="img" tabindex="0" aria-label="${esc(recipe.title)}. 좌우 방향키로 시각 탐색"></canvas><canvas class="cursor-canvas" aria-hidden="true"></canvas><div class="plot-empty"></div><div class="plot-tooltip" hidden></div></div><footer class="chart-tools"><div class="board-legend"></div><div class="chart-options" hidden><div class="chart-refresh"><label>갱신 <input type="number" min="1" max="3600" step="1" aria-label="${esc(recipe.title)} 갱신 주기(초)" placeholder="전체" value="${refreshPeriod(layout.periods[this.key])||''}"> 초</label><button class="plain-link" data-split>${recipe.mergeKeys?'합치기 해제':recipe.parentKey?'분리 취소':'시계열별 분리'}</button><button class="plain-link" data-merge>합치기</button></div><small class="chart-collection-note"></small></div><div class="chart-merge-picker" hidden><label>합칠 대상<select aria-label="합칠 그래프 선택"></select></label><button class="filter-button" data-confirm-merge>합치기</button><button class="plain-link" data-cancel-merge>취소</button></div></footer>`;
  this.canvas=this.node.querySelector('canvas');this.overlay=this.node.querySelector('.cursor-canvas');this.plot=this.node.querySelector('.canvas-plot');this.tooltip=this.node.querySelector('.plot-tooltip');
  this.node.querySelector('[data-detail]')?.addEventListener('click',()=>onDetail(recipe.metric,recipe.assetIds));
  this.node.querySelector('[data-options]').onclick=e=>{const options=this.node.querySelector('.chart-options');options.hidden=!options.hidden;e.currentTarget.setAttribute('aria-expanded',String(!options.hidden))};
  const grip=this.node.querySelector('.chart-grip');grip.onpointerdown=e=>startDrag(this,e);grip.onclick=()=>{if(this.suppressClick){this.suppressClick=false;return}this.chooseTarget()};this.node.querySelector('[data-merge]').onclick=()=>this.chooseTarget();this.node.querySelector('[data-split]').onclick=()=>split(this);this.node.querySelector('[data-cancel-merge]').onclick=()=>this.node.querySelector('.chart-merge-picker').hidden=true;
  this.node.querySelector('.chart-refresh input').oninput=e=>{const value=e.target.value,period=refreshPeriod(value);if(value!==''&&!period){e.target.setCustomValidity('1~3600 사이의 정수 초를 입력하세요.');e.target.reportValidity();return}e.target.setCustomValidity('');if(period)layout.periods[this.key]=period;else delete layout.periods[this.key];this.lastUpdate=0;saveLayout();window.dispatchEvent(new Event('pulse:chart-refresh-changed'));announce(period?`이 그래프를 ${period}초마다 갱신합니다. 실제 수집은 15초 간격입니다.`:'전체 화면 갱신 설정을 따릅니다.')};
  this.canvas.addEventListener('pointermove',e=>{const r=this.canvas.getBoundingClientRect(),x=e.clientX-r.left;this.pointer=true;broadcast(this.snapshot.start+(Math.max(0,Math.min(1,(x-52)/this.plotWidth)))*(this.snapshot.end-this.snapshot.start))});
  this.canvas.addEventListener('pointerleave',()=>{this.pointer=false;broadcast(null)});
  this.canvas.addEventListener('blur',()=>broadcast(null));
  this.canvas.addEventListener('keydown',e=>{if(!['ArrowLeft','ArrowRight','Escape'].includes(e.key))return;e.preventDefault();if(e.key==='Escape'){broadcast(null);return}this.pointer=true;broadcast(Math.max(this.snapshot.start,Math.min(this.snapshot.end,(cursor??this.snapshot.end)+(e.key==='ArrowLeft'?-1:1)*this.snapshot.step)))});
  this.resize=new ResizeObserver(()=>{if(this.visible)this.draw()});this.resize.observe(this.plot);this.update(snapshot);
 }
 chooseTarget(){const picker=this.node.querySelector('.chart-merge-picker'),select=picker.querySelector('select');const options=[...groups].filter(g=>scope(g)===scope(this.grid)).flatMap(g=>g.charts).filter(c=>c.key!==this.key);select.replaceChildren();const seen=new Set();for(const c of options){if(seen.has(c.key))continue;seen.add(c.key);const option=document.createElement('option');option.value=String(options.indexOf(c));option.textContent=`${c.recipe.title} · ${[...new Set(c.lines.map(l=>l.assetName||l.name))].slice(0,2).join(' / ')}`;select.append(option)}picker.hidden=false;this.node.querySelector('[data-confirm-merge]').disabled=!options.length;this.node.querySelector('[data-confirm-merge]').onclick=()=>{const target=options[Number(select.value)];if(target)merge(this,target)};select.focus()}
 update(snapshot){
  this.snapshot=snapshot;this.lastUpdate=Date.now();const all=comparisonLines(snapshot,this.recipe.assetIds,this.recipe.metricIds);
  this.lines=this.recipe.keys?all.filter(l=>this.recipe.keys.includes(l.key)):all;
  this.units=[...new Set(this.lines.map(l=>l.unit))];this.rows=chartRows(this.lines,snapshot.step);
  this.node.querySelector('[data-split]').hidden=!this.recipe.mergeKeys&&!this.recipe.parentKey&&this.lines.length<2;
  this.node.querySelector('.chart-collection-note').textContent=`갱신 ${clockFormat.format(snapshot.end*1000)} · 수집 15초`;
  this.node.querySelector('.board-chart-meta').innerHTML=`<span>${esc(this.units.length>1?`왼쪽 ${this.units[0]} · 오른쪽 ${this.units[1]}`:this.units[0]||this.recipe.unit||'관측 대기')}</span>`;
  const legend=this.node.querySelector('.board-legend');legend.replaceChildren();
  this.lines.forEach((line,index)=>{const point=line.points.at(-1),asset=snapshot.assets.find(a=>line.key.endsWith(':'+a.id));const value=asset?.enabled&&point?.value!==null&&snapshot.end-point.time<=45?point?.value:null;
   const button=document.createElement('button');button.title=line.name;button.setAttribute('aria-pressed',String(!this.hidden.has(line.key)));button.classList.toggle('muted',this.hidden.has(line.key));button.innerHTML=`<i></i><span>${esc(this.recipe.metricIds.length===1?line.assetName||line.name:line.name)}</span><strong>${format(value)} <small>${esc(line.unit==='0/1'?'(0 / 1)':line.unit)}</small></strong>`;button.querySelector('i').style.background=palette[index%8];button.onclick=()=>{if(this.hidden.has(line.key))this.hidden.delete(line.key);else this.hidden.add(line.key);button.classList.toggle('muted',this.hidden.has(line.key));button.setAttribute('aria-pressed',String(!this.hidden.has(line.key)));this.draw()};legend.append(button);
  });
  const empty=this.node.querySelector('.plot-empty');empty.hidden=!!this.lines.length;empty.textContent='관측값 없음';
  if(this.visible)this.draw();
 }
 dimensions(){
  const width=Math.max(200,this.plot.clientWidth),height=this.plot.clientHeight||220,ratio=Math.min(devicePixelRatio||1,2);
  for(const canvas of [this.canvas,this.overlay]){if(canvas.width!==Math.round(width*ratio)||canvas.height!==Math.round(height*ratio)){canvas.width=Math.round(width*ratio);canvas.height=Math.round(height*ratio)}canvas.getContext('2d').setTransform(ratio,0,0,ratio,0,0)}
  this.width=width;this.height=height;this.plotWidth=width-52-(this.units.length>1?50:15);this.plotHeight=height-40;
 }
 draw(){
  this.dimensions();const ctx=this.canvas.getContext('2d');ctx.clearRect(0,0,this.width,this.height);if(!this.lines.length)return;
  ctx.font='11px system-ui';ctx.fillStyle='#a2acc0';ctx.lineWidth=1;this.scales={};
  this.units.forEach((unit,axis)=>{const scale=axisScale(this.lines,unit,this.snapshot.start,this.snapshot.end,this.hidden),{low,high}=scale;
   this.scales[unit]=scale;ctx.textAlign=axis?'left':'right';
   for(let i=0;i<=4;i++){const y=12+this.plotHeight*(1-i/4),v=low+(high-low)*i/4;if(!axis){ctx.strokeStyle='#303746';ctx.setLineDash([3,5]);ctx.beginPath();ctx.moveTo(52,y);ctx.lineTo(52+this.plotWidth,y);ctx.stroke()}if(unit!=='0/1'||i===0||i===4){ctx.fillStyle='#98a3b8';ctx.fillText(formatAxisTick(v,scale),axis?58+this.plotWidth:45,y+4)}}
  });
  const ranges=this.units.map(unit=>axisRangeLabel(this.scales[unit]));
  const rangeLabel=ranges.length>1?`왼쪽 ${ranges[0]} · 오른쪽 ${ranges[1]}`:ranges[0];
  this.node.querySelector('.board-chart-meta').innerHTML=`<span>${esc(rangeLabel)}</span>`;
  this.canvas.setAttribute('aria-label',`${this.recipe.title}. ${rangeLabel}. 좌우 방향키로 시각 탐색`);
  ctx.setLineDash([]);ctx.textAlign='center';ctx.fillStyle='#98a3b8';const ticks=this.width<400?3:4;
  for(let i=0;i<=ticks;i++){const t=this.snapshot.start+(this.snapshot.end-this.snapshot.start)*i/ticks;ctx.fillText(shortTime(t),52+this.plotWidth*i/ticks,this.height-6)}
  ctx.save();ctx.beginPath();ctx.rect(52,8,this.plotWidth,this.plotHeight+8);ctx.clip();
  this.lines.forEach((line,index)=>{if(this.hidden.has(line.key))return;const scale=this.scales[line.unit];ctx.strokeStyle=palette[index%8];ctx.lineWidth=1.7;ctx.setLineDash(index>3?[5,3]:[]);ctx.beginPath();let drawing=false;
   for(const row of this.rows){const value=row['v'+index];if(value===null||value===undefined){drawing=false;continue}const x=52+(row.time-this.snapshot.start)/(this.snapshot.end-this.snapshot.start)*this.plotWidth,y=12+this.plotHeight*(1-(value-scale.low)/(scale.high-scale.low));if(drawing)ctx.lineTo(x,y);else ctx.moveTo(x,y);drawing=true}ctx.stroke();
  });
  if(this.recipe.warning!==undefined&&this.units.length===1){const scale=this.scales[this.units[0]],y=12+this.plotHeight*(1-(this.recipe.warning-scale.low)/(scale.high-scale.low));ctx.strokeStyle='#c9a46a';ctx.setLineDash([4,5]);ctx.beginPath();ctx.moveTo(52,y);ctx.lineTo(52+this.plotWidth,y);ctx.stroke()}
  ctx.restore();this.drawCursor();
 }
 drawCursor(){
  if(!this.visible||!this.width)return;const ctx=this.overlay.getContext('2d');ctx.clearRect(0,0,this.width,this.height);this.tooltip.hidden=cursor===null||!this.pointer;if(cursor===null)return;
  const x=52+(cursor-this.snapshot.start)/(this.snapshot.end-this.snapshot.start)*this.plotWidth;ctx.strokeStyle='#a995df';ctx.lineWidth=1;ctx.setLineDash([3,3]);ctx.beginPath();ctx.moveTo(x,8);ctx.lineTo(x,this.height-25);ctx.stroke();
  if(this.pointer){const bucket=Math.floor(cursor/this.snapshot.step)*this.snapshot.step,row=this.rows.find(r=>r.time===bucket);this.tooltip.innerHTML=`<strong>${esc(fullTime(cursor))}</strong>`+this.lines.map((l,i)=>this.hidden.has(l.key)?'':`<div><span>${esc(l.name)}</span><b>${format(row?.['v'+i])} ${esc(l.unit)}</b></div>`).join('');this.tooltip.style.left=`${Math.min(Math.max(8,x),Math.max(8,this.width-310))}px`;this.tooltip.style.top='4px'}
 }
 destroy(){if(drag?.chart===this)drag.cancel();this.resize.disconnect();delete this.node._pulseChart}
}
