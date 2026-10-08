import {readSnapshot} from '@/lib/monitoring/prometheus';
import {authorize} from '@/lib/monitoring/auth';
export const dynamic='force-dynamic';
export async function GET(request:Request){
 const denied=await authorize(request);if(denied)return denied;
 const url=new URL(request.url);const range=Number(url.searchParams.get('range')||3600),instance=url.searchParams.get('instance')||'all';
 if(![900,3600,21600,86400].includes(range)||instance.length>200||[...url.searchParams.keys()].some(k=>!['range','instance'].includes(k)))return Response.json({error:'허용되지 않은 조회 범위입니다.'},{status:400});
 try{return Response.json(await readSnapshot(range,instance),{headers:{'Cache-Control':'no-store'}});}catch{return Response.json({error:'조회 요청을 완료하지 못했습니다. 잠시 후 다시 시도하세요.'},{status:503});}
}
