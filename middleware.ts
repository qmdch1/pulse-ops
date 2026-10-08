import { NextRequest, NextResponse } from 'next/server';
import { authorize } from './lib/monitoring/auth';
export async function middleware(request:NextRequest){
  const denied=request.nextUrl.pathname==='/api/health'?null:await authorize(request);
  if(denied)return denied;
  const response=NextResponse.next();
  response.headers.set('X-Content-Type-Options','nosniff');
  response.headers.set('Referrer-Policy','same-origin');
  response.headers.set('X-Frame-Options','DENY');
  response.headers.set('Permissions-Policy','camera=(), microphone=(), geolocation=()');
  return response;
}
export const config={matcher:['/((?!_next/static|_next/image|favicon.svg).*)']};
