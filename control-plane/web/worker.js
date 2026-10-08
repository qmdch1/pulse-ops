import {registeredIncidents} from './lib/assets.js';
let generation=0,active;
self.onmessage=async ({data})=>{
 if(data.type==='stop'){generation++;active?.abort();return}
 const sequence=++generation;active?.abort();active=new AbortController();const controller=active,timer=setTimeout(()=>controller.abort(),35000);
 try{const response=await fetch(`/api/monitoring?range=${data.range}`,{signal:controller.signal});if(!response.ok)throw new Error(response.status===401?'로그인 세션을 확인하세요.':'관측 데이터를 읽을 수 없습니다.');const snapshot=await response.json();const incidents=registeredIncidents(snapshot);if(sequence===generation)postMessage({type:'snapshot',snapshot,incidents})}
 catch(e){if(sequence===generation)postMessage({type:'error',message:e.name==='AbortError'?'관측 요청 시간이 초과됐습니다.':e.message})}
 finally{clearTimeout(timer)}
};
// Catalog loading is asynchronous. Do not let the first request arrive before
// the module worker has installed its message handler.
postMessage({type:'ready'});
