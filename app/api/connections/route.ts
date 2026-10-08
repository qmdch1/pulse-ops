import {authorize} from '@/lib/monitoring/auth';
export const dynamic='force-dynamic';
export async function GET(request:Request){
 const denied=await authorize(request);if(denied)return denied;
 let hosts:unknown[]=[];
 if(process.env.SSH_INVENTORY_FILE){try{const {readFile}=await import('node:fs/promises');const content=await readFile(process.env.SSH_INVENTORY_FILE,'utf8');if(content.length<100000){const data=JSON.parse(content);hosts=(Array.isArray(data.hosts)?data.hosts:[]).slice(0,100).map((h:any)=>({alias:String(h.alias).slice(0,100),hostname:String(h.hostname).slice(0,200),user:String(h.user).slice(0,80),port:String(h.port).slice(0,5),proxyJump:h.proxyJump?String(h.proxyJump).slice(0,200):null,platform:String(h.platform).slice(0,30),status:'not_connected'}));}}catch{}}
 return Response.json({hosts,mode:process.env.MONITORING_MODE||'unconfigured',configured:!!process.env.CONTROL_PLANE_URL,notice:'SSH 설정의 연결 후보입니다. 아직 접속하거나 exporter를 설치하지 않았습니다.'},{headers:{'Cache-Control':'no-store'}});
}
