export const $ = (selector, root=document) => root.querySelector(selector);
export const $$ = (selector, root=document) => [...root.querySelectorAll(selector)];
export const esc = value => String(value??'').replace(/[&<>"']/g, c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
export const shortTime = v => new Intl.DateTimeFormat('ko-KR',{hour:'2-digit',minute:'2-digit',hour12:false,timeZone:'Asia/Seoul'}).format(v*1000);
export const fullTime = v => new Date(typeof v==='number'?v*1000:v).toLocaleString('ko-KR',{timeZone:'Asia/Seoul',hour12:false});
const paths={activity:'M3 12h4l3-8 4 16 3-8h4',server:'M4 3h16v7H4z M4 14h16v7H4z M7 6h1 M7 17h1',grid:'M3 3h7v7H3z M14 3h7v7h-7z M3 14h7v7H3z M14 14h7v7h-7z',event:'M12 3v2 M12 19v2 M3 12h2 M19 12h2 M8 12a4 4 0 1 0 8 0a4 4 0 1 0-8 0',settings:'M4 7h16 M4 17h16 M9 4v6 M15 14v6',database:'M4 6c0-5 16-5 16 0s-16 5-16 0 M4 6v12c0 5 16 5 16 0V6 M4 12c0 5 16 5 16 0',terminal:'M4 6l6 6-6 6 M12 18h8',plus:'M12 4v16 M4 12h16',close:'M5 5l14 14 M19 5L5 19',search:'M16 16l5 5 M3 10a7 7 0 1 0 14 0a7 7 0 1 0-14 0',arrow:'M4 12h16 M14 6l6 6-6 6',down:'M6 9l6 6 6-6',edit:'M4 20l4-1 12-12-4-4L4 15z M14 5l5 5',refresh:'M20 6v5h-5 M4 18v-5h5 M5 8a8 8 0 0 1 14-2 M19 16a8 8 0 0 1-14 2',check:'M4 12l5 5L20 6',menu:'M4 6h16 M4 12h16 M4 18h16',compare:'M4 4v16 M12 4v16 M20 4v16',shield:'M12 2l8 3v7c0 5-8 10-8 10S4 17 4 12V5z M8 12l3 3 5-6'};
export function icon(name,size=17){return `<svg width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="${paths[name]||paths.activity}"/></svg>`}
export function infraIcon(kind){return `<span class="infra-icon ${['postgres','mysql','mariadb','oracle'].includes(kind)?'database':kind==='redis'?'cache':''}">${icon(kind==='server'?'server':['postgres','mysql','mariadb','oracle','redis'].includes(kind)?'database':'activity')}</span>`}
export function download(name,data,type='application/json'){const blob=new Blob([typeof data==='string'?data:JSON.stringify(data,null,2)],{type});const url=URL.createObjectURL(blob),a=document.createElement('a');a.href=url;a.download=name;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}
let activeDialog;
export function dialog(title,description,body,{className='',onClose}={}){
 activeDialog?.close();
 const previous=document.activeElement,d=document.createElement('dialog');d.className=`pulse-dialog ${className}`;
 d.innerHTML=`<header class="dialog-header"><div><div class="eyebrow">PULSE / OPS</div><h2 id="dialog-title">${esc(title)}</h2><p>${esc(description)}</p></div><button class="icon-button" data-close aria-label="닫기">${icon('close')}</button></header><div class="dialog-body">${body}</div>`;
 d.setAttribute('aria-labelledby','dialog-title');document.body.append(d);document.body.classList.add('dialog-open');
 d.querySelector('[data-close]').onclick=()=>d.close();
 d.addEventListener('click',e=>{if(e.target===d){const r=d.getBoundingClientRect();if(e.clientX<r.left||e.clientX>r.right||e.clientY<r.top||e.clientY>r.bottom)d.close()}});
 d.addEventListener('close',()=>{onClose?.();d.remove();if(activeDialog===d)activeDialog=null;document.body.classList.remove('dialog-open');if(previous?.isConnected)previous.focus()},{once:true});
 activeDialog=d;d.showModal();return d;
}
export function bind(root,selector,event,fn){for(const element of $$(selector,root))element.addEventListener(event,e=>fn(e,element))}
export function preference(key,fallback){try{return JSON.parse(localStorage.getItem(key))??fallback}catch{return fallback}}
export function savePreference(key,value){try{localStorage.setItem(key,JSON.stringify(value))}catch{}}
export function debounce(fn,delay=120){let timer;return(...args)=>{clearTimeout(timer);timer=setTimeout(()=>fn(...args),delay)}}
export async function request(path,method='GET',body){
 const controller=new AbortController(),timer=setTimeout(()=>controller.abort(),40000);
 try{const response=await fetch('/api/control/'+path,{method,headers:body?{'Content-Type':'application/json'}:undefined,body:body?JSON.stringify(body):undefined,signal:controller.signal});const data=await response.json();if(!response.ok)throw new Error(data.error||'요청을 처리하지 못했습니다.');return data}finally{clearTimeout(timer)}
}
