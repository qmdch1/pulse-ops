import {$,dialog,request,esc} from './ui.js';

export async function openTerminal(asset){
 let cleanup=()=>{},generation=0;
 const d=dialog(asset.name,`${asset.ssh.username}@${asset.ssh.host||asset.address} · ${asset.ssh.jumpId?'SSH jump 경유':'직접 SSH'}`,`<div class="terminal-status"><span>연결 중</span><button class="filter-button">다시 연결</button></div><p class="form-error" role="alert"></p><div class="terminal-mount" aria-label="${esc(asset.name)} SSH 터미널"></div><p class="modal-footnote">연결된 서버의 실제 셸입니다. 창을 닫으면 세션이 종료됩니다. 입력·출력 본문은 감사 이력에 저장하지 않습니다.</p>`,{className:'terminal-dialog',onClose:()=>{generation++;cleanup()}});
 if(!document.querySelector('[data-terminal-css]')){const link=document.createElement('link');link.rel='stylesheet';link.href='/assets/vendor/xterm.css';link.dataset.terminalCss='';document.head.append(link)}
 async function connect(){
  cleanup();const current=++generation;let socket;
  $('.terminal-status span',d).textContent='연결 중';$('[role=alert]',d).textContent='';$('.terminal-mount',d).replaceChildren();
  try{
   const [{Terminal},{FitAddon}]=await Promise.all([import('./vendor/xterm.mjs'),import('./vendor/addon-fit.mjs')]);if(!d.isConnected||current!==generation)return;
   const terminal=new Terminal({cursorBlink:true,fontSize:13,fontFamily:'Consolas, monospace',scrollback:3000,theme:{background:'#0b0f17',foreground:'#dae1ef',cursor:'#a99bff'}}),fit=new FitAddon();terminal.loadAddon(fit);terminal.open($('.terminal-mount',d));fit.fit();
   const send=value=>{if(socket?.readyState===WebSocket.OPEN)socket.send(JSON.stringify(value))};
   const input=terminal.onData(data=>send({type:'input',data})),resize=terminal.onResize(({cols,rows})=>send({type:'resize',cols,rows}));
   const observer=new ResizeObserver(()=>{try{fit.fit()}catch{}});observer.observe($('.terminal-mount',d));
   let disposed=false;const dispose=()=>{if(disposed)return;disposed=true;send({type:'disconnect'});socket?.close();observer.disconnect();input.dispose();resize.dispose();terminal.dispose()};cleanup=dispose;
   const ticket=await request('terminals','POST',{assetId:asset.id});if(!d.isConnected||current!==generation){dispose();return}
   socket=new WebSocket(`${location.protocol==='https:'?'wss:':'ws:'}//${location.host}${ticket.path}?ticket=${encodeURIComponent(ticket.ticket)}`);socket.binaryType='arraybuffer';
   socket.onmessage=e=>{if(current!==generation||!d.isConnected)return;if(e.data instanceof ArrayBuffer){terminal.write(new Uint8Array(e.data));return}const message=JSON.parse(e.data);$('.terminal-status span',d).textContent=message.message;if(message.type==='error')$('[role=alert]',d).textContent=message.message;if(message.type==='connected'){fit.fit();send({type:'resize',cols:terminal.cols,rows:terminal.rows});terminal.focus()}};
   socket.onerror=()=>{if(d.isConnected&&current===generation)$('[role=alert]',d).textContent='터미널 연결을 열 수 없습니다. SSH 경로와 접속 정보를 확인하세요.'};
   socket.onclose=()=>{if(d.isConnected&&current===generation)$('.terminal-status span',d).textContent='연결 종료'};
  }catch(e){if(d.isConnected&&current===generation)$('[role=alert]',d).textContent=e.message}
 }
 $('.terminal-status button',d).onclick=connect;
 const unload=()=>cleanup();window.addEventListener('pagehide',unload,{once:true});d.addEventListener('close',()=>window.removeEventListener('pagehide',unload),{once:true});
 await connect();
}
