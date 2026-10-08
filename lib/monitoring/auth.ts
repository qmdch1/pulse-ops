export async function authorize(request:Request):Promise<Response|null>{
  const mode=process.env.MONITORING_MODE;
  if(mode!=='production') return null;
  const user=process.env.DASHBOARD_USERNAME, password=process.env.DASHBOARD_PASSWORD;
  if(!user || !password || password.length<24) return Response.json({error:'운영 접근 계정과 24자 이상의 비밀번호가 필요합니다.'},{status:503});
  const header=request.headers.get('authorization')||'';
  let value='';
  try{if(header.startsWith('Basic '))value=atob(header.slice(6));}catch{}
  const encoder=new TextEncoder();
  const [a,b]=await Promise.all([crypto.subtle.digest('SHA-256',encoder.encode(value)),crypto.subtle.digest('SHA-256',encoder.encode(`${user}:${password}`))]);
  const aa=new Uint8Array(a),bb=new Uint8Array(b);let diff=0;for(let i=0;i<aa.length;i++)diff|=aa[i]^bb[i];
  if(diff===0)return null;
  return new Response('인증이 필요합니다.',{status:401,headers:{'WWW-Authenticate':'Basic realm="PULSE OPS", charset="UTF-8"','Cache-Control':'no-store'}});
}
