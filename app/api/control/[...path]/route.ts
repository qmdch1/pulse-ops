import {authorize} from '@/lib/monitoring/auth';
import {controlFetch} from '@/lib/monitoring/control';
export const dynamic='force-dynamic';
async function proxy(request:Request,{params}:{params:Promise<{path:string[]}>}){
 const denied=await authorize(request);if(denied)return denied;
 const url=new URL(request.url),path=(await params).path.join('/'),method=request.method;
 const allowed=method==='GET'?/^(assets|audit)$/.test(path):method==='POST'?/^(assets|terminals|assets\/[a-f0-9]+\/(connect|pause|fingerprint))$/.test(path):/^(PUT|DELETE)$/.test(method)&&/^assets\/[a-f0-9]+$/.test(path);
 if(!allowed||url.search)return Response.json({error:'지원하지 않는 요청입니다.'},{status:400});
 if(method!=='GET'){
  const origin=request.headers.get('origin'),host=request.headers.get('host');
  let sameOrigin=false;try{sameOrigin=!!origin&&new URL(origin).host===host}catch{}
  if(!sameOrigin||request.headers.get('sec-fetch-site')==='cross-site')return Response.json({error:'현재 대시보드에서 요청하세요.'},{status:403});
 }
 try{
  let body:string|undefined;
  if(method==='PUT'||method==='POST'){
   if(Number(request.headers.get('content-length')||0)>262144)return Response.json({error:'입력 크기를 줄여주세요.'},{status:413});
   body=await request.text();if(new TextEncoder().encode(body).length>262144)return Response.json({error:'입력 크기를 줄여주세요.'},{status:413});
  }
  const response=await controlFetch(path,{method,body,headers:{'Content-Type':'application/json'}});
  return new Response(await response.text(),{status:response.status,headers:{'Content-Type':'application/json','Cache-Control':'no-store'}});
 }catch{return Response.json({error:'인프라 관리 서비스에 연결할 수 없습니다.'},{status:503})}
}
export const GET=proxy;export const POST=proxy;export const PUT=proxy;export const DELETE=proxy;
