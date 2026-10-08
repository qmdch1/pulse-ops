"use client";
import {useEffect,useRef,useState} from 'react';
import {Terminal as TerminalIcon} from 'lucide-react';
import {Dialog,DialogContent,DialogHeader,DialogTitle,DialogDescription} from './ui/dialog';
import {controlRequest,type Asset} from '@/lib/monitoring/assets';
import '@xterm/xterm/css/xterm.css';
export function AssetTerminal({asset,onClose}:{asset:Asset;onClose:()=>void}){
 const mount=useRef<HTMLDivElement>(null),[status,setStatus]=useState('연결 중'),[error,setError]=useState(''),[attempt,setAttempt]=useState(0);
 useEffect(()=>{
  let disposed=false,socket:WebSocket|undefined,cleanup=()=>{};
  setStatus('연결 중');setError('');
  void (async()=>{try{
   const [{Terminal},{FitAddon}]=await Promise.all([import('@xterm/xterm'),import('@xterm/addon-fit')]);if(disposed||!mount.current)return;
   const terminal=new Terminal({cursorBlink:true,fontSize:13,fontFamily:'Consolas, monospace',scrollback:3000,theme:{background:'#0b0f17',foreground:'#dae1ef',cursor:'#a99bff'}}),fit=new FitAddon();terminal.loadAddon(fit);terminal.open(mount.current);fit.fit();
   const observer=new ResizeObserver(()=>{try{fit.fit()}catch{}});observer.observe(mount.current);
   const input=terminal.onData(data=>{if(socket?.readyState===WebSocket.OPEN)socket.send(JSON.stringify({type:'input',data}))});
   const resize=terminal.onResize(({cols,rows})=>{if(socket?.readyState===WebSocket.OPEN)socket.send(JSON.stringify({type:'resize',cols,rows}))});
   cleanup=()=>{input.dispose();resize.dispose();observer.disconnect();terminal.dispose()};
   const ticket=await controlRequest<{ticket:string;path:string}>('terminals','POST',{assetId:asset.id});if(disposed)return;
   socket=new WebSocket(`${location.protocol==='https:'?'wss:':'ws:'}//${location.host}${ticket.path}?ticket=${encodeURIComponent(ticket.ticket)}`);socket.binaryType='arraybuffer';
   socket.onmessage=event=>{if(disposed)return;if(event.data instanceof ArrayBuffer){terminal.write(new Uint8Array(event.data));return}try{const message=JSON.parse(event.data);setStatus(message.message);if(message.type==='error')setError(message.message);if(message.type==='connected'){socket?.send(JSON.stringify({type:'resize',cols:terminal.cols,rows:terminal.rows}));terminal.focus()}}catch{}};
   socket.onerror=()=>{if(!disposed)setError('터미널 연결을 열 수 없습니다. 게이트웨이와 SSH 경로를 확인하세요.')};
   socket.onclose=()=>{if(!disposed)setStatus('연결 종료')};
  }catch(e){if(!disposed){setStatus('연결 실패');setError((e as Error).message)}}})();
  return()=>{disposed=true;if(socket?.readyState===WebSocket.OPEN)socket.send(JSON.stringify({type:'disconnect'}));socket?.close();cleanup()};
 },[asset.id,attempt]);
 return <Dialog open onOpenChange={open=>{if(!open)onClose()}}><DialogContent className="terminal-dialog"><DialogHeader><DialogTitle><TerminalIcon size={18}/> {asset.name}</DialogTitle><DialogDescription>{asset.ssh.username}@{asset.ssh.host||asset.address} · {asset.ssh.jumpId?'SSH jump 경유':'직접 SSH'}</DialogDescription></DialogHeader><div className="terminal-status"><span>{status}</span><button className="filter-button" onClick={()=>setAttempt(a=>a+1)}>다시 연결</button></div>{error&&<p className="form-error" role="alert">{error}</p>}<div ref={mount} className="terminal-mount" aria-label={`${asset.name} SSH 터미널`}/><p className="modal-footnote">연결된 서버의 실제 셸입니다. 창을 닫으면 세션이 종료됩니다. 입력·출력 내용은 서버 감사 이력에 저장하지 않습니다.</p></DialogContent></Dialog>
}
