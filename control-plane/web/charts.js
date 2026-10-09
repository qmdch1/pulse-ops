import {comparisonLines,chartRows,chartGroups} from './lib/board.js';
import {recipeKey,readLayout,mergeLayout,activeGroups,removeGroup,refreshPeriod,refreshDue,familyColumns} from './lib/chart-layout.js';
import {formatNumber} from './lib/rules.js';
import {axisScale,sharedScale,formatAxisTick,axisRangeLabel} from './lib/chart-scale.js';
import {metricById} from './lib/catalog.js';
import {enter,valueChanged} from './lib/motion.js';
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
function merge(source,target){const area=scope(target.grid);if(scope(source.grid)!==area||source.key===target.key)return;layout.groups=mergeLayout(layout.groups,source.key,target.key);changed();announce('그래프를 합쳤습니다. 시간과 단위는 각각 유지됩니다.');requestAnimationFrame(()=>{const chart=[...groups].filter(g=>scope(g)===area).flatMap(g=>g.charts).find(c=>c.key===target.key);if(chart){chart.node.classList.add('chart-joined');chart.node.querySelector('[data-undo]')?.focus({preventScroll:true})}})}
function split(chart){if(chart.recipe.mergeKeys){layout.groups=removeGroup(layout.groups,chart.key);announce('합치기를 해제하고 원래 그래프로 분리했습니다.')}else if(chart.recipe.parentKey){layout.split=layout.split.filter(k=>k!==chart.recipe.parentKey);layout.groups=layout.groups.filter(g=>!g.some(k=>chart.grid.expanded.some(r=>r.parentKey===chart.recipe.parentKey&&r.layoutKey===k)));announce('분리 전 그래프로 되돌렸습니다.')}else{layout.split=[...new Set([...layout.split,chart.key])];announce('각 시계열을 개별 그래프로 분리했습니다.')}const restoreKey=chart.recipe.parentKey||chart.key;changed();requestAnimationFrame(()=>{const restored=[...groups].filter(g=>scope(g)===scope(chart.grid)).flatMap(g=>g.charts).find(c=>c.key===restoreKey||c.recipe.parentKey===restoreKey);(restored?.node.querySelector('.chart-title')||restored?.node.querySelector('[data-options]'))?.focus({preventScroll:true})})}
function startDrag(chart,event){if(event.button!==0||drag)return;const handle=event.currentTarget,startX=event.clientX,startY=event.clientY;let preview,target,moved=false,raf=0,x=startX,y=startY;const area=scope(chart.grid);handle.setPointerCapture(event.pointerId);drag={chart};
 const paint=()=>{if(!drag)return;if(preview){preview.style.left=`${x+16}px`;preview.style.top=`${y+16}px`;const node=document.elementFromPoint(x,y)?.closest('.board-chart');const candidate=node?._pulseChart;target?.node.classList.remove('chart-drop-target');target=candidate&&candidate!==chart&&candidate.key!==chart.key&&scope(candidate.grid)===area?candidate:null;target?.node.classList.add('chart-drop-target');const edge=65,speed=y<edge?-13:y>innerHeight-edge?13:0;if(speed){if(area?.tagName==='DIALOG')area.scrollTop+=speed;else window.scrollBy(0,speed)}}raf=requestAnimationFrame(paint)};
 const move=e=>{x=e.clientX;y=e.clientY;if(!moved&&Math.hypot(x-startX,y-startY)>5){moved=true;chart.suppressClick=true;preview=chart.node.cloneNode(true);preview.className='board-chart chart-drag-preview';preview.setAttribute('aria-hidden','true');preview.inert=true;preview.style.width=Math.min(chart.node.clientWidth,420)+'px';const original=chart.node.querySelectorAll('canvas');preview.querySelectorAll('canvas').forEach((c,i)=>{c.width=original[i].width;c.height=original[i].height;c.getContext('2d').drawImage(original[i],0,0)});area?.tagName==='DIALOG'?area.append(preview):document.body.append(preview);chart.node.classList.add('chart-lifted');document.body.classList.add('chart-dragging');raf=requestAnimationFrame(paint)}};
 const end=(e,cancel=false)=>{cancelAnimationFrame(raf);if(moved&&!cancel){target?.node.classList.remove('chart-drop-target');const candidate=document.elementFromPoint(e.clientX,e.clientY)?.closest('.board-chart')?._pulseChart;target=candidate&&candidate!==chart&&candidate.key!==chart.key&&scope(candidate.grid)===area?candidate:null;}handle.removeEventListener('pointermove',move);handle.removeEventListener('pointerup',up);handle.removeEventListener('pointercancel',cancelled);window.removeEventListener('keydown',key);window.removeEventListener('blur',cancelled);try{handle.releasePointerCapture(event.pointerId)}catch{}preview?.remove();target?.node.classList.remove('chart-drop-target');chart.node.classList.remove('chart-lifted');document.body.classList.remove('chart-dragging');drag=null;if(moved&&!cancel&&target)merge(chart,target);else if(moved)announce('합치기를 취소했습니다. 그래프가 원래 위치로 돌아왔습니다.')};
 const up=e=>end(e),cancelled=e=>end(e,true),key=e=>{if(e.key==='Escape'){e.preventDefault();end(e,true)}};drag.cancel=cancelled;handle.addEventListener('pointermove',move);handle.addEventListener('pointerup',up);handle.addEventListener('pointercancel',cancelled);window.addEventListener('keydown',key);window.addEventListener('blur',cancelled);
}
let cursor=null,frame=0;
function broadcast(time){cursor=time;if(frame)return;frame=requestAnimationFrame(()=>{frame=0;for(const grid of groups)for(const chart of grid.charts)if(chart.visible)chart.drawCursor()})}
// Merged or split charts leave the family and render at full size.
const familyRow=recipe=>recipe.family&&!recipe.mergeKeys&&!recipe.parentKey?recipe.family.row:null;

// P50…P99.9, in/out pairs and policy flags: one axis, one legend and one color
// per infrastructure across the row, so card heights compare directly.
class FamilyRow {
 constructor(recipe,size){
  this.charts=[];this.size=size;this.hiddenAssets=new Set();this.order=[];this.scales=new Map();this.frame=0;
  this.node=document.createElement('section');this.node.className='chart-family';this.node.setAttribute('aria-label',`${recipe.family.title} · ${size}개 그래프 같은 축 비교`);
  this.node.innerHTML=`<header><div><h4>${esc(recipe.family.title)}</h4><p></p></div><div class="chart-family-legend" role="group" aria-label="${esc(recipe.family.title)} 인프라 표시 전환"></div></header><div class="chart-family-grid"></div>`;
  this.body=this.node.querySelector('.chart-family-grid');this.body.style.gridTemplateColumns=`repeat(${size},minmax(0,1fr))`;
  this.resize=new ResizeObserver(([entry])=>{this.body.style.gridTemplateColumns=`repeat(${familyColumns(entry.contentRect.width,this.size)},minmax(0,1fr))`});this.resize.observe(this.body);
 }
 color(assetId){return Math.max(0,this.order.indexOf(assetId))}
 scale(unit){if(!this.scales.has(unit))this.scales.set(unit,sharedScale(this.charts.filter(c=>c.lines.length).map(c=>axisScale(c.lines,unit,c.snapshot.start,c.snapshot.end,c.hidden)),unit));return this.scales.get(unit)}
 // Colors follow the infrastructure, not the line position, and hiding one keeps the rest unchanged.
 changed(){this.scales.clear();const seen=new Set(this.charts.flatMap(c=>c.lines.map(l=>l.assetId)));this.order=[...new Set(this.charts.flatMap(c=>c.recipe.assetIds))].filter(id=>seen.has(id));if(!this.frame)this.frame=requestAnimationFrame(()=>{this.frame=0;this.render()})}
 render(){
  const names=new Map(this.charts.flatMap(c=>c.lines.map(l=>[l.assetId,l.assetName]))),legend=this.node.querySelector('.chart-family-legend');
  legend.replaceChildren(...this.order.map(id=>{const button=document.createElement('button'),off=this.hiddenAssets.has(id);button.classList.toggle('muted',off);button.setAttribute('aria-pressed',String(!off));button.title=`${names.get(id)} · 이 행 전체에서 ${off?'다시 표시':'숨기기'}`;button.innerHTML=`<i></i><span>${esc(names.get(id))}</span>`;button.querySelector('i').style.background=palette[this.color(id)%8];button.onclick=()=>{if(this.hiddenAssets.has(id))this.hiddenAssets.delete(id);else this.hiddenAssets.add(id);this.charts.forEach(c=>c.follow());this.changed()};return button}));
  const unit=this.charts.find(c=>c.lines.length)?.lines[0].unit,scale=unit&&this.scale(unit),warnings=[...new Set(this.charts.map(c=>c.recipe.warning).filter(Number.isFinite))];
  this.node.querySelector('header p').textContent=[this.charts.map(c=>c.recipe.family.label).join(' · '),scale?`같은 축 ${axisRangeLabel(scale)}`:'관측 대기',scale&&unit!=='0/1'&&warnings.length===1?`점선 경고 ${formatAxisTick(warnings[0],scale)}${scale.displayUnit?' '+scale.displayUnit:''}`:''].filter(Boolean).join(' · ');
  for(const chart of this.charts){chart.showLatest();if(chart.visible)chart.draw()}
 }
 destroy(){cancelAnimationFrame(this.frame);this.resize.disconnect()}
}

// Canvas and tooltip work only near the viewport. Lazy batches keep long
// multi-infrastructure boards from constructing thousands of charts at once.
export class ChartGrid {
 constructor(root,recipes,snapshot,onDetail,animated=false){
  this.root=root;this.baseRecipes=recipes;this.recipes=recipes;this.snapshot=snapshot;this.onDetail=onDetail;this.animated=animated;this.charts=[];this.count=0;this.rows=new Map();this.familySizes=new Map();
  this.observer=new IntersectionObserver(entries=>{for(const entry of entries){const chart=this.charts.find(c=>c.node===entry.target);if(chart){const becameVisible=!chart.visible&&entry.isIntersecting;chart.visible=entry.isIntersecting;if(chart.visible)chart.draw();if(becameVisible&&refreshPeriod(layout.periods[chart.key]))window.dispatchEvent(new Event('pulse:chart-refresh-changed'))}}},{rootMargin:'250px'});
  this.sentinel=document.createElement('div');this.sentinel.className='chart-sentinel';this.sentinel.setAttribute('aria-live','polite');
  this.loader=new IntersectionObserver(entries=>{if(entries.some(e=>e.isIntersecting))this.append()},{rootMargin:'500px'});
  groups.add(this);queueLayout();
 }
 reset(recipes){this.observer.disconnect();this.loader.disconnect();for(const c of this.charts)c.destroy();for(const r of this.rows.values())r.destroy();this.charts=[];this.rows=new Map();this.count=0;this.recipes=recipes;this.familySizes=new Map();for(const r of recipes){const row=familyRow(r);if(row)this.familySizes.set(row,(this.familySizes.get(row)||0)+1)}this.root.replaceChildren(this.sentinel);this.root.hidden=!recipes.length;this.append();if(this.count<recipes.length)this.loader.observe(this.sentinel)}
 append(){
  const next=this.recipes.slice(this.count,this.count+8);if(!next.length)return;
  for(const recipe of next){const key=familyRow(recipe),size=key?this.familySizes.get(key):0;let row=null;if(size>1){row=this.rows.get(key);if(!row){row=new FamilyRow(recipe,size);this.rows.set(key,row);this.root.insertBefore(row.node,this.sentinel)}}
   const chart=new CanvasChart(recipe,this.snapshot,this.onDetail,this,row);this.charts.push(chart);if(row)row.body.append(chart.node);else this.root.insertBefore(chart.node,this.sentinel);this.observer.observe(chart.node)}
  this.count+=next.length;
  this.sentinel.textContent='';
  if(this.count===this.recipes.length){this.loader.disconnect();this.sentinel.remove()}
 }
 update(snapshot,force=false){this.snapshot=snapshot;for(const chart of this.charts)if(refreshDue(chart.lastUpdate,Date.now(),layout.periods[chart.key],defaultRefresh,force||!snapshot.connected))chart.update(snapshot)}
 destroy(){this.loader.disconnect();this.observer.disconnect();for(const chart of this.charts)chart.destroy();for(const row of this.rows.values())row.destroy();groups.delete(this)}
}
class CanvasChart {
 constructor(recipe,snapshot,onDetail,grid,row=null){
  this.grid=grid;this.recipe=recipe;this.row=row;this.compact=!!row;this.key=recipe.layoutKey||recipeKey(recipe);this.hidden=new Set();this.visible=false;this.lines=[];
  this.node=document.createElement('article');this.node.className=this.compact?'board-chart compact':'board-chart';this.node.setAttribute('aria-label',`${recipe.title} 그래프`);
  if(grid.animated){this.node.classList.add('chart-enter');this.node.style.setProperty('--enter-delay',`${Math.min(grid.charts.length,5)*30}ms`)}
  this.node._pulseChart=this;
  const label=this.compact?recipe.family.label:recipe.title,name=onDetail&&recipe.metric?`<button class="chart-title" data-detail aria-label="${esc(recipe.title)} 상세">${esc(label)}</button>`:esc(label);
  // Family cards show the short label and latest values; the row header carries the axis and legend.
  const heading=this.compact?`<h4 title="${esc(recipe.title)}">${name}<small class="chart-unit"></small><small class="chart-period" hidden></small></h4><div class="chart-latest"></div>`:`<h4 title="${esc(recipe.subtitle||'')}">${name}</h4>`;
  this.node.innerHTML=`<header><button class="chart-grip" aria-label="${esc(recipe.title)} 그래프 잡기" title="그래프 합치기">⠿</button><div>${heading}</div><button class="icon-button chart-undo" data-undo aria-label="${esc(recipe.title)} ${recipe.mergeKeys?'합치기 해제':'분리 취소'}" title="${recipe.mergeKeys?'합치기 해제':'분리 취소'}" ${recipe.mergeKeys||recipe.parentKey?'':'hidden'}>${icon('undo',15)}</button><button class="icon-button" data-options aria-label="${esc(recipe.title)} 그래프 설정" aria-expanded="false">${icon('settings',15)}</button></header><div class="board-chart-meta"></div><div class="canvas-plot"><canvas role="img" tabindex="0" aria-label="${esc(recipe.title)}. 좌우 방향키로 시각 탐색"></canvas><canvas class="cursor-canvas" aria-hidden="true"></canvas><div class="plot-empty"></div><div class="plot-tooltip" hidden></div></div><footer class="chart-tools"><div class="board-legend"></div><div class="chart-options" hidden><div class="chart-refresh"><label>갱신 <input type="number" min="1" max="3600" step="1" aria-label="${esc(recipe.title)} 갱신 주기(초)" placeholder="전체" value="${refreshPeriod(layout.periods[this.key])||''}"> 초</label><button class="plain-link" data-split>${recipe.mergeKeys?'합치기 해제':recipe.parentKey?'분리 취소':'시계열별 분리'}</button><button class="plain-link" data-merge>합치기</button></div><small class="chart-collection-note"></small></div><div class="chart-merge-picker" hidden></div></footer>`;
  this.canvas=this.node.querySelector('canvas');this.overlay=this.node.querySelector('.cursor-canvas');this.plot=this.node.querySelector('.canvas-plot');this.tooltip=this.node.querySelector('.plot-tooltip');
  this.node.querySelector('[data-detail]')?.addEventListener('click',()=>onDetail(recipe.metric,recipe.assetIds));
  this.node.querySelector('[data-undo]').onclick=()=>split(this);
  this.node.querySelector('[data-options]').onclick=e=>{const options=this.node.querySelector('.chart-options');options.hidden=!options.hidden;e.currentTarget.setAttribute('aria-expanded',String(!options.hidden));if(!options.hidden)enter(options)};
  const grip=this.node.querySelector('.chart-grip');grip.onpointerdown=e=>startDrag(this,e);grip.onclick=()=>{if(this.suppressClick){this.suppressClick=false;return}this.chooseTarget()};this.node.querySelector('[data-merge]').onclick=()=>this.chooseTarget();this.node.querySelector('[data-split]').onclick=()=>split(this);
  this.node.querySelector('.chart-refresh input').oninput=e=>{const value=e.target.value,period=refreshPeriod(value);if(value!==''&&!period){e.target.setCustomValidity('1~3600 사이의 정수 초를 입력하세요.');e.target.reportValidity();return}e.target.setCustomValidity('');if(period)layout.periods[this.key]=period;else delete layout.periods[this.key];this.lastUpdate=0;saveLayout();window.dispatchEvent(new Event('pulse:chart-refresh-changed'));announce(period?`이 그래프를 ${period}초마다 갱신합니다. 실제 수집은 15초 간격입니다.`:'전체 화면 갱신 설정을 따릅니다.')};
  this.canvas.addEventListener('pointermove',e=>{const r=this.canvas.getBoundingClientRect(),x=e.clientX-r.left;this.pointer=true;broadcast(this.snapshot.start+(Math.max(0,Math.min(1,(x-(this.left??52))/this.plotWidth)))*(this.snapshot.end-this.snapshot.start))});
  this.canvas.addEventListener('pointerleave',()=>{this.pointer=false;broadcast(null)});
  this.canvas.addEventListener('blur',()=>broadcast(null));
  this.canvas.addEventListener('keydown',e=>{if(!['ArrowLeft','ArrowRight','Escape'].includes(e.key))return;e.preventDefault();if(e.key==='Escape'){broadcast(null);return}this.pointer=true;broadcast(Math.max(this.snapshot.start,Math.min(this.snapshot.end,(cursor??this.snapshot.end)+(e.key==='ArrowLeft'?-1:1)*this.snapshot.step)))});
  row?.charts.push(this);this.resize=new ResizeObserver(()=>{if(this.visible)this.draw()});this.resize.observe(this.plot);this.update(snapshot);
 }
 chooseTarget(){
  const picker=this.node.querySelector('.chart-merge-picker');if(!picker.hidden){picker.hidden=true;return}
  picker.innerHTML=`<header><span>합치기</span><button class="icon-button" aria-label="합치기 취소">${icon('close',13)}</button></header><div class="chart-targets"></div>`;
  picker.querySelector('header button').onclick=()=>{picker.hidden=true;this.node.querySelector('.chart-grip').focus()};
  const seen=new Set(),root=picker.querySelector('.chart-targets');
  for(const target of [...groups].filter(g=>scope(g)===scope(this.grid)).flatMap(g=>g.charts)){
   if(target.key===this.key||seen.has(target.key))continue;seen.add(target.key);
   const button=document.createElement('button');button.className='chart-target';button.textContent=target.recipe.title;button.onclick=()=>merge(this,target);root.append(button);
  }
  if(!root.children.length)root.textContent='합칠 그래프 없음';picker.hidden=false;enter(picker);root.querySelector('button')?.focus();
 }
 update(snapshot){
  this.snapshot=snapshot;this.lastUpdate=Date.now();const all=comparisonLines(snapshot,this.recipe.assetIds,this.recipe.metricIds);
  this.lines=this.recipe.keys?all.filter(l=>this.recipe.keys.includes(l.key)):all;
  this.units=[...new Set(this.lines.map(l=>l.unit))];this.rows=chartRows(this.lines,snapshot.step);
  this.node.querySelector('[data-split]').hidden=!!this.recipe.mergeKeys||!!this.recipe.parentKey||this.lines.length<2;
  this.node.querySelector('.chart-collection-note').textContent=`갱신 ${clockFormat.format(snapshot.end*1000)} · 수집 15초`;
  this.node.querySelector('.board-chart-meta').innerHTML=`<span>${esc(this.units.length>1?`왼쪽 ${this.units[0]} · 오른쪽 ${this.units[1]}`:this.units[0]||this.recipe.unit||'관측 대기')}</span>`;
  const legend=this.node.querySelector('.board-legend'),existing=new Map([...legend.children].map(button=>[button.dataset.series,button]));
  if(this.row){this.follow();this.row.changed()}else this.lines.forEach((line,index)=>{const value=this.latest(line);
   let button=existing.get(line.key);
   if(!button){button=document.createElement('button');button.dataset.series=line.key;button.innerHTML='<i></i><span></span><strong></strong>';button.onclick=()=>{if(this.hidden.has(line.key))this.hidden.delete(line.key);else this.hidden.add(line.key);button.classList.toggle('muted',this.hidden.has(line.key));button.setAttribute('aria-pressed',String(!this.hidden.has(line.key)));this.draw()};legend.append(button)}
   existing.delete(line.key);button.title=line.name;button.setAttribute('aria-pressed',String(!this.hidden.has(line.key)));button.classList.toggle('muted',this.hidden.has(line.key));
   button.querySelector('span').textContent=this.recipe.metricIds.length===1?line.assetName||line.name:line.name;button.querySelector('i').style.background=palette[index%8];
   const number=button.querySelector('strong'),previous=number.textContent;number.innerHTML=`${format(value)} <small>${esc(line.unit==='0/1'?'(0 / 1)':line.unit)}</small>`;
   if(previous&&previous!==number.textContent)valueChanged(number);
  });
  for(const button of existing.values())button.remove();
  const empty=this.node.querySelector('.plot-empty');empty.hidden=!!this.lines.length;empty.textContent='관측값 없음';
  if(this.visible&&!this.row)this.draw();
 }
 latest(line){const point=line.points.at(-1),asset=this.snapshot.assets.find(a=>a.id===line.assetId);return asset?.enabled&&point&&point.value!==null&&this.snapshot.end-point.time<=45?point.value:null}
 paint(line,index){return this.row?this.row.color(line.assetId):index}
 follow(){this.hidden=new Set(this.lines.filter(l=>this.row.hiddenAssets.has(l.assetId)).map(l=>l.key))}
 showLatest(){
  const shown=this.lines.filter(l=>!this.hidden.has(l.key)),unit=this.units[0]==='0/1'?'':this.units[0]||'',box=this.node.querySelector('.chart-latest');
  const before=box.textContent;
  if(shown.length===1)box.innerHTML=`<strong>${format(this.latest(shown[0]))}${unit?`<small>${esc(unit)}</small>`:''}</strong>`;
  else if(!shown.length)box.textContent=this.lines.length?'모두 숨김':'';
  else box.replaceChildren(...shown.map(line=>{const value=document.createElement('span');value.title=line.assetName;value.innerHTML=`<i></i>${format(this.latest(line))}`;value.querySelector('i').style.background=palette[this.paint(line)%8];return value}));
  if(shown.length===1&&before&&before!==box.textContent)valueChanged(box.querySelector('strong'));
  this.node.querySelector('.chart-unit').textContent=shown.length>1?unit:'';
  const period=refreshPeriod(layout.periods[this.key]),badge=this.node.querySelector('.chart-period');badge.hidden=!period;badge.textContent=period?`↻ ${period}초`:'';badge.title=period?`개별 갱신 ${period}초 · 그래프 설정에서 변경`:'';
 }
 dimensions(){
  const width=Math.max(this.compact?120:200,this.plot.clientWidth),height=this.plot.clientHeight||(this.compact?128:220),ratio=Math.min(devicePixelRatio||1,2);
  for(const canvas of [this.canvas,this.overlay]){if(canvas.width!==Math.round(width*ratio)||canvas.height!==Math.round(height*ratio)){canvas.width=Math.round(width*ratio);canvas.height=Math.round(height*ratio)}canvas.getContext('2d').setTransform(ratio,0,0,ratio,0,0)}
  this.left=this.compact?34:52;this.width=width;this.height=height;this.plotWidth=width-this.left-(this.units.length>1?50:this.compact?12:15);this.plotHeight=height-40;
 }
 draw(){
  this.dimensions();const ctx=this.canvas.getContext('2d');ctx.clearRect(0,0,this.width,this.height);if(!this.lines.length)return;
  const left=this.left,steps=this.compact?2:4;ctx.font=this.compact?'10px system-ui':'11px system-ui';ctx.fillStyle='#a2acc0';ctx.lineWidth=1;this.scales={};
  this.units.forEach((unit,axis)=>{const scale=this.row?.scale(unit)||axisScale(this.lines,unit,this.snapshot.start,this.snapshot.end,this.hidden),{low,high}=scale;
   this.scales[unit]=scale;ctx.textAlign=axis?'left':'right';
   for(let i=0;i<=steps;i++){const y=12+this.plotHeight*(1-i/steps),v=low+(high-low)*i/steps;if(!axis){ctx.strokeStyle='#303746';ctx.setLineDash([3,5]);ctx.beginPath();ctx.moveTo(left,y);ctx.lineTo(left+this.plotWidth,y);ctx.stroke()}if(unit!=='0/1'||i===0||i===steps){ctx.fillStyle='#98a3b8';ctx.fillText(formatAxisTick(v,scale),axis?left+6+this.plotWidth:left-7,y+4)}}
  });
  const ranges=this.units.map(unit=>axisRangeLabel(this.scales[unit]));
  const rangeLabel=ranges.length>1?`왼쪽 ${ranges[0]} · 오른쪽 ${ranges[1]}`:ranges[0];
  this.node.querySelector('.board-chart-meta').innerHTML=`<span>${esc(rangeLabel)}</span>`;
  this.canvas.setAttribute('aria-label',`${this.recipe.title}. ${rangeLabel}. 좌우 방향키로 시각 탐색`);
  ctx.setLineDash([]);ctx.textAlign='center';ctx.fillStyle='#98a3b8';const ticks=this.compact?2:this.width<400?3:4;
  for(let i=0;i<=ticks;i++){const t=this.snapshot.start+(this.snapshot.end-this.snapshot.start)*i/ticks,label=shortTime(t),half=ctx.measureText(label).width/2;ctx.fillText(label,Math.min(Math.max(left+this.plotWidth*i/ticks,half+2),this.width-half-2),this.height-6)}
  ctx.save();ctx.beginPath();ctx.rect(left,8,this.plotWidth,this.plotHeight+8);ctx.clip();
  this.lines.forEach((line,index)=>{if(this.hidden.has(line.key))return;const scale=this.scales[line.unit],color=this.paint(line,index);ctx.strokeStyle=palette[color%8];ctx.lineWidth=this.compact?1.5:1.7;ctx.setLineDash(color>3?[5,3]:[]);ctx.beginPath();let drawing=false;
   for(const row of this.rows){const value=row['v'+index];if(value===null||value===undefined){drawing=false;continue}const x=left+(row.time-this.snapshot.start)/(this.snapshot.end-this.snapshot.start)*this.plotWidth,y=12+this.plotHeight*(1-(value-scale.low)/(scale.high-scale.low));if(drawing)ctx.lineTo(x,y);else ctx.moveTo(x,y);drawing=true}ctx.stroke();
  });
  if(this.recipe.warning!==undefined&&this.units.length===1){const scale=this.scales[this.units[0]],y=12+this.plotHeight*(1-(this.recipe.warning-scale.low)/(scale.high-scale.low));ctx.strokeStyle='#c9a46a';ctx.setLineDash([4,5]);ctx.beginPath();ctx.moveTo(left,y);ctx.lineTo(left+this.plotWidth,y);ctx.stroke()}
  ctx.restore();this.drawCursor();
 }
 drawCursor(){
  if(!this.visible||!this.width)return;const ctx=this.overlay.getContext('2d');ctx.clearRect(0,0,this.width,this.height);this.tooltip.hidden=cursor===null||!this.pointer;if(cursor===null)return;
  const x=this.left+(cursor-this.snapshot.start)/(this.snapshot.end-this.snapshot.start)*this.plotWidth;ctx.strokeStyle='#a995df';ctx.lineWidth=1;ctx.setLineDash([3,3]);ctx.beginPath();ctx.moveTo(x,8);ctx.lineTo(x,this.height-25);ctx.stroke();
  if(this.pointer){const bucket=Math.floor(cursor/this.snapshot.step)*this.snapshot.step,row=this.rows.find(r=>r.time===bucket);this.tooltip.innerHTML=`<strong>${esc(fullTime(cursor))}</strong>`+this.lines.map((l,i)=>this.hidden.has(l.key)?'':`<div><span>${esc(this.compact?l.assetName:l.name)}</span><b>${format(row?.['v'+i])} ${esc(l.unit)}</b></div>`).join('');this.tooltip.style.left='0px';this.tooltip.style.left=`${Math.min(Math.max(8,x),Math.max(8,this.width-this.tooltip.offsetWidth-8))}px`;this.tooltip.style.top='4px'}
 }
 destroy(){if(drag?.chart===this)drag.cancel();this.resize.disconnect();delete this.node._pulseChart}
}
