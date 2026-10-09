import {test,assert} from './harness.js';
import {mountAISettings,aiSettingsInput,mountAITasks,openAIJob,openAIProject} from '/assets/ai.js';
const tick=()=>new Promise(r=>setTimeout(r,0));
const root=()=>{const e=document.createElement('section');document.body.append(e);return e};
const settings={version:2,model:'jev-latest',threshold:.75,concurrency:2,timeoutMinutes:30,hasKey:true,profiles:{codex:{enabled:true,model:'',strengths:'Implementation'},claude:{enabled:true,model:'',strengths:'Review'}}};
const decision={provider:'claude',category:'review',confidence:.6,needsReview:true,probabilities:{codex:.2,claude:.7,review:.1},basis:'operator criteria; no user ratings',model:'fixture',inputTokens:23};
test('AI masked settings preserve the key, selected models and editable routing criteria',async()=>{
 const el=root();let saved;
 const api=async(path,method,input)=>{if(method==='PUT'){saved=input;return input}return {settings,runtime:{codex:true,claude:false},root:'/projects'}};
 try{await mountAISettings(el,api);const f=el.querySelector('form');f.elements.codexModel.value='configured-model';f.elements.claudeStrengths.value='Project-specific preference';const data=aiSettingsInput(f,settings);assert.ok(!Object.hasOwn(data,'apiKey'));assert.equal(data.profiles.codex.model,'configured-model');assert.equal(data.profiles.claude.strengths,'Project-specific preference');assert.equal(data.threshold,.75);f.requestSubmit();await tick();assert.equal(saved.version,2);assert.ok(!Object.hasOwn(saved,'apiKey'));assert.ok(el.textContent.includes('실행 도구 있음'));assert.ok(el.textContent.includes('설치 확인 필요'))}finally{el.remove()}
});
test('AI key clearing is explicit and does not expose the saved credential',async()=>{
 const el=root();try{await mountAISettings(el,async()=>({settings,runtime:{},root:''}));const f=el.querySelector('form');el.querySelector('[data-clear-key]').click();assert.equal(aiSettingsInput(f,settings).apiKey,'');assert.equal(f.elements.apiKey.value,'')}finally{el.remove()}
});
test('AI preview never submits an execution job and escapes untrusted decision metadata',async()=>{
 const el=root();let calls=[];let stop;
 const api=async(path,method,input)=>{calls.push([path,method,input]);if(path==='ai/projects')return [{id:'p',name:'<img src=x>'}];if(path==='ai/jobs')return [];return {...decision,basis:'<script>unsafe</script>'}};
 try{stop=await mountAITasks(el,api);const f=el.querySelector('form');f.elements.prompt.value='review the code';el.querySelector('[data-preview]').click();await tick();assert.equal(calls.filter(c=>c[1]==='POST').length,1);assert.equal(calls.find(c=>c[1]==='POST')[0],'ai/preview');assert.ok(el.textContent.includes('자동 실행을 보류'));assert.equal(el.querySelectorAll('script,img').length,0);assert.equal(f.elements.prompt.value,'review the code')}finally{stop?.();el.remove()}
});
test('AI job submission sends the selected project and prompt once and keeps polling separate from the editor',async()=>{
 const el=root();let submitted;let stop;
 const api=async(path,method,input)=>{if(path==='ai/projects')return [{id:'p',name:'Project'}];if(method==='POST'){submitted=input;return {id:'j',status:'queued'}}return [{id:'j',projectId:'p',prompt:'<img src=x>',provider:'codex',status:'running',createdAt:100}]};
 try{stop=await mountAITasks(el,api);const f=el.querySelector('form');f.elements.prompt.value='fix $(literal)';f.requestSubmit();await tick();assert.deepEqual(submitted,{projectId:'p',prompt:'fix $(literal)'});assert.equal(f.elements.prompt.value,'');assert.ok(el.textContent.includes('작업 중'));assert.equal(el.querySelectorAll('img').length,0);f.elements.prompt.value='draft retained';el.querySelector('[data-reload-jobs]').click();await tick();assert.equal(f.elements.prompt.value,'draft retained')}finally{stop?.();el.remove()}
});
test('AI low-confidence manual assignment preserves the current version and result evaluation is separate',async()=>{
 let j={id:'j',version:3,status:'review',provider:'claude',prompt:'review',createdAt:100,decision};let sent;
 const api=async(path,method,input)=>{if(method==='POST'){sent=input;j={...j,version:4,status:'ready',provider:input.provider};return j}return j};
 const d=await openAIJob('j',()=>{},api);try{d.querySelector('[data-assign="codex"]').click();await tick();assert.deepEqual(sent,{version:3,action:'assign',provider:'codex'});assert.ok(d.textContent.includes('실행 대기'));assert.equal(d.querySelectorAll('[data-rate]').length,0)}finally{d.close()}
});
test('AI completed results are escaped and human feedback uses the recorded execution version',async()=>{
 let j={id:'j',version:7,status:'completed',provider:'codex',prompt:'fix',output:'<img src=x> no tests run',createdAt:100,rating:''};let sent;
 const api=async(path,method,input)=>{if(method==='POST'){sent=input;j={...j,version:8,rating:input.rating};return j}return j};
 const d=await openAIJob('j',()=>{},api);try{assert.equal(d.querySelectorAll('img').length,0);assert.ok(d.textContent.includes('테스트 통과나 변경 승인'));d.querySelector('[data-rate="needs_work"]').click();await tick();assert.deepEqual(sent,{version:7,action:'rate',rating:'needs_work'});assert.equal(d.querySelector('[data-rate="needs_work"]').getAttribute('aria-pressed'),'true')}finally{d.close()}
});
test('AI project registration sends server-side paths with explicit project context',async()=>{
 let sent;const d=openAIProject(()=>{},async(path,method,input)=>sent={path,method,input});try{const f=d.querySelector('form');f.elements.name.value='Project';f.elements.directory.value='/projects/repository';f.elements.context.value='Go and REST API';f.requestSubmit();await tick();assert.deepEqual(sent,{path:'ai/projects',method:'POST',input:{name:'Project',directory:'/projects/repository',context:'Go and REST API'}})}finally{d.close()}
});
test('AI execution usage distinguishes an absent report from a measured zero',async()=>{
 let j={id:'j',version:1,status:'completed',provider:'codex',prompt:'fix',createdAt:100,workspace:'/projects/work',inputTokens:0,outputTokens:0,usageReported:false};
 let d=await openAIJob('j',()=>{},async()=>j);
 try{assert.ok(d.textContent.includes('토큰 사용량 미제공'));assert.ok(!d.textContent.includes('입력 0 · 출력 0'))}finally{d.close()}
 j.usageReported=true;d=await openAIJob('j',()=>{},async()=>j);
 try{assert.ok(d.textContent.includes('입력 0 · 출력 0 토큰'))}finally{d.close()}
});
