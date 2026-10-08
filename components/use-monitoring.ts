"use client";
import {useCallback,useEffect,useRef,useState} from 'react';
import type {Snapshot} from '@/lib/monitoring/types';

export function useMonitoring(range:string,instance='all',enabled=true,refreshSeconds=30){
 const [snapshot,setSnapshot]=useState<Snapshot|null>(null),[loading,setLoading]=useState(false),[error,setError]=useState<string|null>(null);
 const controller=useRef<AbortController|null>(null),generation=useRef(0),busy=useRef(false);
 const load=useCallback(async()=>{
  if(!enabled||busy.current)return;
  busy.current=true;const version=generation.current,request=new AbortController();controller.current=request;setLoading(true);
  try{const response=await fetch(`/api/monitoring?range=${range}&instance=${encodeURIComponent(instance)}`,{signal:request.signal});if(!response.ok)throw new Error('데이터를 불러오지 못했습니다. 연결 상태를 확인해 주세요.');const data:Snapshot=await response.json();if(version===generation.current){setSnapshot(data);setError(null)}}
  catch(e){if(!request.signal.aborted&&version===generation.current){setError(e instanceof Error?e.message:'수집 실패');setSnapshot(previous=>previous?{...previous,connected:false,metrics:previous.metrics.map(m=>({...m,state:'stale',latest:null}))}:null)}}
  finally{if(version===generation.current){busy.current=false;setLoading(false)}}
 },[range,instance,enabled]);
 useEffect(()=>{generation.current++;controller.current?.abort();busy.current=false;setSnapshot(null);setError(null);void load();return()=>{generation.current++;controller.current?.abort();busy.current=false}},[load]);
 useEffect(()=>{if(!enabled||!refreshSeconds)return;const timer=setInterval(()=>{if(!document.hidden)void load()},refreshSeconds*1000);return()=>clearInterval(timer)},[load,enabled,refreshSeconds]);
 return {snapshot,loading,error,reload:load};
}
