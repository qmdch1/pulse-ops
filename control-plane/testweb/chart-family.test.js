import {test,assert} from './harness.js';
import {ChartGrid} from '/assets/charts.js';
import {metricRecipes} from '/assets/lib/board.js';
import {metrics} from '/assets/lib/catalog.js';

const frames=(n=3)=>new Promise(resolve=>{const next=()=>n--?requestAnimationFrame(next):resolve();next()});
const asset=id=>({id,name:id,kind:'application',dependencies:[],enabled:true,status:'connected'});
const metric=(id,values)=>({id,state:'ok',latest:null,series:Object.entries(values).map(([assetId,value])=>({labels:{assetId,name:assetId},points:Array.from({length:21},(_,i)=>({time:9700+i*15,value}))}))});
const snapshot=()=>({assets:[asset('api-a'),asset('api-b')],mode:'test',connected:true,collectedAt:'',start:9700,end:10000,step:15,targets:[],alerts:[],metrics:[metric('requests',{'api-a':5,'api-b':6}),metric('p50',{'api-a':20,'api-b':30}),metric('p95',{'api-a':80,'api-b':90}),metric('p97',{'api-a':100,'api-b':120}),metric('p99',{'api-a':300,'api-b':1750}),metric('p99.9',{'api-b':1900}),metric('latency-mean',{'api-a':25,'api-b':35})]});
const charts0=grid=>grid.charts.filter(c=>c.row);
async function mount(ids=['api-a','api-b']){const root=document.createElement('div');root.className='board-chart-grid';root.style.width='1100px';document.body.append(root);const s=snapshot();const grid=new ChartGrid(root,metricRecipes(s,ids,metrics.filter(m=>s.metrics.some(x=>x.id===m.id))),s,()=>{});await frames();return {root,grid,done:()=>{grid.destroy();root.remove()}}}

test('percentile family renders as one compact row on one shared axis',async()=>{const {root,grid,done}=await mount();try{
 const row=root.querySelector(':scope>.chart-family');assert.ok(row,'family row');
 assert.deepEqual([...row.querySelectorAll('.board-chart.compact .chart-title')].map(b=>b.textContent),['P50','P95','P97','P99','P99.9','평균']);
 assert.deepEqual([...root.querySelectorAll(':scope>.board-chart h4')].map(h=>h.textContent),['요청 처리량']);
 const p99=charts0(grid).find(c=>c.recipe.metricIds[0]==='p99').node;p99.querySelector('[data-options]').click();assert.ok(!p99.querySelector('.chart-options').hidden,'settings stay reachable on a compact card');assert.ok(p99.querySelector('.chart-refresh input'));
 assert.equal(row.querySelector('.chart-family-grid').style.gridTemplateColumns,'repeat(6, minmax(0px, 1fr))');
 const charts=grid.charts.filter(c=>c.row),scale=charts[0].row.scale('ms');
 assert.equal(scale.high,1900);assert.ok(charts.every(c=>c.row.scale('ms')===scale));
 assert.match(row.querySelector('header p').textContent,/같은 축 0–1\.9 초 · 점선 경고 0\.5 초/);
 assert.equal(charts.find(c=>c.recipe.metricIds[0]==='p50').node.querySelector('.chart-latest').textContent,'2030');assert.equal(charts[0].node.querySelector('.chart-unit').textContent,'ms');
}finally{done()}});

test('family colors follow infrastructure and the row legend hides it on every card',async()=>{const {root,grid,done}=await mount();try{
 const charts=grid.charts.filter(c=>c.row),tail=charts.find(c=>c.recipe.metricIds[0]==='p99.9');
 assert.equal(tail.lines.length,1);assert.equal(tail.paint(tail.lines[0],0),1,'api-b keeps its color where api-a is missing');
 const legend=[...root.querySelectorAll('.chart-family-legend button')];assert.deepEqual(legend.map(b=>b.textContent),['api-a','api-b']);
 legend[1].click();await frames();
 assert.ok(charts.every(c=>c.lines.every(l=>c.hidden.has(l.key)===(l.assetId==='api-b'))));
 assert.equal(charts[0].row.scale('ms').high,1000,'hidden spikes leave the shared axis');
 assert.equal(tail.node.querySelector('.chart-latest').textContent,'모두 숨김');
 assert.equal(root.querySelector('.chart-family-legend button:last-child').getAttribute('aria-pressed'),'false');
}finally{done()}});

test('a single infrastructure shows each family value as a headline with its unit',async()=>{const {root,done}=await mount(['api-a']);try{
 const cards=[...root.querySelectorAll('.chart-family .board-chart.compact')];
 assert.deepEqual(cards.map(c=>c.querySelector('.chart-latest strong')?.textContent),['20ms','80ms','100ms','300ms',undefined,'25ms']);
 assert.equal(cards[0].querySelector('.chart-latest span'),null);assert.equal(cards[0].querySelector('.chart-unit').textContent,'');
}finally{done()}});
