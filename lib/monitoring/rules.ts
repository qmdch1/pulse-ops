import type {Snapshot,Incident,MetricResult} from './types';
import {metricById} from './catalog.ts';
import {databaseEvidence} from './database-evidence.ts';
export function sustained(metric:MetricResult|undefined, predicate:(n:number)=>boolean, seconds:number,end:number,step:number){
 if(!metric||metric.state!=='ok')return false;
 return metric.series.some(series=>{
  const points=series.points.filter(p=>p.time>=end-seconds-step&&p.time<=end);
  if(points.length<2||points[0].time>end-seconds||points.at(-1)!.time<end-step)return false;
  for(let i=0;i<points.length;i++){if(points[i].value===null||!predicate(points[i].value!)||(i>0&&points[i].time-points[i-1].time>step*1.5))return false;}
  return true;
 });
}
export function slope(metric:MetricResult|undefined,minSeconds:number){
 if(!metric||metric.state!=='ok')return null;
 const points=metric.series[0]?.points.filter(p=>p.value!==null)||[];
 if(points.length<10||points.at(-1)!.time-points[0].time<minSeconds)return null;
 const start=points[0].time,n=points.length;let sx=0,sy=0,sxx=0,sxy=0;
 for(const p of points){const x=p.time-start;sx+=x;sy+=p.value!;sxx+=x*x;sxy+=x*p.value!;}
 const denominator=n*sxx-sx*sx;return denominator>0?(n*sxy-sx*sy)/denominator:null;
}
export const simpleRules:[string,string,(n:number)=>boolean,Incident['severity'],Incident['kind'],string][]=[
 ['cookie-expiry','인증 세션 만료 임박 또는 만료',n=>n<60,'warning','expiring','관리 대상 세션의 갱신 경로와 절대 만료 시각을 확인하세요.'],
 ['tls-expiry','TLS 인증서 만료 임박',n=>n<14,'warning','expiring','인증서 체인과 자동 갱신 작업을 확인하세요.'],
 ['token-expiry','서비스 토큰 만료 임박',n=>n<24,'warning','expiring','서비스 계정 토큰의 안전한 교체를 준비하세요.'],
 ['cookie-secure','Secure 쿠키 정책 미충족',n=>n<1,'warning','detected','관리 대상 쿠키의 HTTPS 전송 정책을 점검하세요.'],
 ['cookie-http','HttpOnly 쿠키 정책 미충족',n=>n<1,'warning','detected','인증 쿠키의 클라이언트 접근 필요성을 검토하세요.'],
 ['cookie-samesite','SameSite 쿠키 정책 미충족',n=>n<1,'warning','detected','서비스의 교차 사이트 정책을 확인하세요.'],
 ['loop-p99','이벤트 루프 정체',n=>n>100,'warning','detected','프로세스별 CPU와 동기 작업·직렬화·정규식 실행을 확인하세요.'],
 ['queue','작업 대기열 누적',n=>n>100,'warning','detected','유입·처리율, 소비자 상태와 배압 정책을 확인하세요.'],
 ['queue-age','작업 최대 대기 시간 초과',n=>n>60,'warning','detected','장기 대기 작업과 소비자 병목을 확인하세요.'],
 ['disk','디스크 용량 압박',n=>n>85,'warning','detected','마운트별 여유 공간과 파일 증가량을 확인하세요.'],
 ['inode','inode 소진 위험',n=>n>90,'warning','detected','작은 파일 증가와 보존 정책을 확인하세요.'],
 ['file-descriptors','파일·소켓 한계 접근',n=>n>80,'warning','detected','연결 해제 누락과 파일 디스크립터 한계를 점검하세요.'],
 ['db-up','DB 연결 실패',n=>n<1,'critical','detected','DB 서비스 상태와 등록한 계정 접근을 확인하세요.'],
 ['db-probe','DB 읽기 응답 지연',n=>n>200,'warning','detected','DB 왕복 시간과 연결 한도·활성 세션·잠금 대기를 함께 확인하세요. 업무 쿼리 P99와는 다른 측정입니다.'],
 ['db-connection-usage','DB 연결·세션 한도 접근',n=>n>80,'warning','detected','최대 연결 설정과 활성 세션·연결 반환 여부를 확인하세요.'],
 ['db-lock-waiters','DB 잠금 대기 지속',n=>n>0,'warning','detected','차단 트랜잭션과 처리 중 세션, 애플리케이션 응답 지연을 확인하세요.'],
 ['db-slow-queries','느린 DB 명령 증가',n=>n>1,'warning','detected','DB의 느린 쿼리 기준과 인덱스·실행 계획을 확인하세요.'],
 ['db-monitoring-ready','DB 통계 일부 수집 실패',n=>n<1,'warning','collection','DB 연결은 가능하지만 일부 통계를 읽지 못했습니다. 모니터링 권한·DB 버전·쿼리 제한을 확인하세요.'],
 ['redis-up','Redis 연결 실패',n=>n<1,'critical','detected','Redis 서비스 상태와 등록한 계정 접근을 확인하세요.'],
 ['db-replication','DB 복제 지연',n=>n>30,'warning','detected','복제 네트워크와 WAL 적용 지연을 점검하세요.'],
 ['db-deadlocks','DB 데드락 발생',n=>n>0,'warning','detected','잠금 순서와 트랜잭션 경계를 확인하세요.'],
 ['db-rollback','DB 트랜잭션 롤백 증가',n=>n>5,'warning','detected','애플리케이션 예외와 트랜잭션 실패 사유를 확인하세요.'],
 ['redis-memory','Redis 메모리 한계 접근',n=>n>85,'warning','detected','키 크기·축출 정책·maxmemory 설정을 확인하세요.'],
 ['redis-evictions','캐시 축출 증가',n=>n>1,'warning','detected','메모리 사용과 DB 요청 증가를 확인하세요.'],
 ['gc-pause','GC 정지 시간 증가',n=>n>100,'warning','detected','힙 크기와 할당률, 런타임별 GC 원인을 확인하세요.'],
 ['oom','OOM 종료 컨테이너 관측',n=>n>0,'critical','detected','종료된 컨테이너의 메모리 한계와 요청 크기를 확인하세요.'],
 ['clock-health','시간 동기화 중단',n=>n<1,'warning','detected','시간 동기화 서비스와 기준 시계 연결을 확인하세요.'],
 ['restarts','컨테이너 반복 재시작',n=>n>2,'warning','detected','종료 사유와 배포·자원 한계를 확인하세요.'],
 ['probe','외부 HTTP Probe 실패',n=>n<100,'critical','detected','서비스 경로와 DNS·TLS·로드밸런서를 확인하세요.'],
 ['auth-errors','인증 실패율 상승',n=>n>5,'warning','detected','401, 자격 증명 만료와 변경된 인증 계약을 확인하세요.'],
 ['clock-skew','시스템 시계 편차',n=>n>1,'warning','detected','시간 동기화 서비스 상태를 확인하세요.'],
 ['network-drop','네트워크 패킷 드롭',n=>n>1,'warning','detected','NIC 큐·버퍼와 네트워크 혼잡을 확인하세요.'],
 ['tcp-retry','TCP 재전송 증가',n=>n>1,'warning','detected','연결 손실과 혼잡을 점검하세요.'],
 ['targets-down','지표 수집 대상 중단',n=>n>0,'critical','collection','등록 대상 연결과 수집 상태를 점검하세요.'],
 ['scrape-age','수집 데이터 신선도 저하',n=>n>45,'warning','collection','수집 주기와 대상 응답 시간을 확인하세요.'],
 ];
export function detectIncidents(snapshot:Snapshot):Incident[]{
 const incidents:Incident[]=[];const map=new Map(snapshot.metrics.map(m=>[m.id,m]));
 const value=(id:string)=>map.get(id)?.latest??null;
 const holds=(id:string,p:(n:number)=>boolean,sec=120)=>sustained(map.get(id),p,sec,snapshot.end,snapshot.step);
 const add=(id:string,title:string,kind:Incident['kind'],severity:Incident['severity'],ids:string[],summary:string,steps:string[])=>{const m=metricById.get(ids[0]);const n=value(ids[0]);incidents.push({id,title,kind,severity,metricIds:ids,summary,steps,status:'firing',scope:'선택 범위',value:n===null?'—':formatNumber(n),unit:m?.unit||'',evidence:ids.map(k=>`${metricById.get(k)?.title||k}: ${value(k)===null?'미관측':formatNumber(value(k)!)} ${metricById.get(k)?.unit||''}`)});};
 if(!snapshot.connected){if(snapshot.mode!=='unconfigured')add('collector','수집기 연결 중단','collection','critical',[],'운영 상태를 판단할 수 없습니다. 마지막 값을 정상으로 간주하지 않습니다.',['Go 인프라 관리 서비스 연결을 확인하세요.','수집기 네트워크와 접근 허용 목록을 확인하세요.']);return incidents;}
 if(holds('errors',n=>n>2))add('errors','서버 오류율 증가','detected','critical',['errors','p99','requests'],'5xx 비율이 2분 이상 2%를 초과했습니다. 실제 실패 범위를 먼저 확인하세요.',['실패 API와 최근 변경 버전을 확인하세요.','성공·실패 지연을 분리해 타임아웃과 빠른 실패를 구분하세요.']);
 if(holds('p99',n=>n>500))add('latency','꼬리 응답 지연 지속','detected','critical',['p99','cpu','pool-wait'],'P99가 2분 이상 500ms를 초과했습니다. 원인 후보는 추가 지표로 검증해야 합니다.',['CPU가 낮으면 DB·외부 호출·락 대기를 확인하세요.','CPU가 높으면 트래픽, 핫 프로세스, 스로틀링을 확인하세요.']);
 if(holds('pool-active',n=>n>80)&&holds('pool-wait',n=>n>100))add('pool','DB 커넥션 풀 병목 후보','detected','critical',['pool-active','pool-wait','db-latency'],'풀 사용률과 연결 대기가 함께 높습니다. 풀 증설보다 반환 지연의 원인을 확인하세요.',['슬로 쿼리와 잠금 대기를 점검하세요.','타임아웃, 요청 제한, 재시도 예산을 확인하세요.']);
 if(holds('p99',n=>n>500)&&value('cpu')!==null&&value('cpu')!<35)add('io','CPU 여유 상태의 지연 증가','detected','warning',['p99','cpu','db-latency'],'I/O, 잠금 또는 의존 서비스 대기가 원인 후보입니다.',['DB 쿼리 및 연결 대기를 확인하세요.','외부 호출 추적에서 대기 구간을 확인하세요.']);
 if(holds('errors',n=>n>2)&&value('p99')!==null&&value('p99')!<200)add('fast-fail','빠른 실패로 가려진 장애 후보','detected','critical',['errors','p99','failed-latency'],'오류는 증가했으나 지연은 낮습니다. 낮아진 P99를 회복으로 단정할 수 없습니다.',['연결 거부, 예외 로그와 배포 이력을 확인하세요.']);
 if(holds('throttling',n=>n>20)&&holds('p99',n=>n>500))add('throttle','CPU 제한과 꼬리 지연 동반','detected','warning',['throttling','p99','cpu'],'CPU 제한 주기와 응답 지연이 동시에 증가했습니다.',['컨테이너 CPU limit과 프로세스 사용량을 비교하세요.']);
 const leak=slope(map.get('gc-floor'),1800);
 if(leak!==null&&leak>1/60)add('leak','GC 이후 메모리 상승 추세','predicted','warning',['gc-floor','gc-pause','memory'],'30분 이상 관측에서 GC 후 메모리가 분당 1MiB 이상 증가합니다. RSS만으로 힙 누수를 확정할 수 없습니다.',['힙·네이티브 메모리 프로파일을 확보하세요.','배포 변경과 큰 요청의 시점을 비교하세요.']);
 if(holds('disk-forecast',n=>n<0,900))add('disk-risk','24시간 내 디스크 소진 가능','predicted','warning',['disk-forecast','disk-free','disk'],'6시간 선형 추세의 예측 결과가 15분간 음수입니다. 증가율 변화에 따라 결과가 달라집니다.',['로그·백업·임시 파일의 증가 원인을 확인하세요.','보존 정책 및 용량 확장을 계획하세요.']);
 const baseline=value('requests-week');if(baseline!==null&&baseline>1&&holds('requests',n=>n<baseline*.5))add('traffic-drop','평소 대비 트래픽 감소','detected','warning',['requests','requests-week','probe'],'지난주 동일 시각 대비 요청량이 절반 이하입니다. 계절성과 캠페인 영향을 함께 확인하세요.',['DNS, 게이트웨이, 로드밸런서 도달 여부를 점검하세요.']);
 if(baseline!==null&&baseline>1&&holds('requests',n=>n>baseline*2))add('traffic-spike','평소 대비 트래픽 급증','detected','warning',['requests','requests-week','retries'],'지난주 동일 시각 대비 2배를 초과했습니다.',['사용자 증가와 크롤러·재시도·정기 배치를 구분하세요.']);
 const old=value('p99-week');if(old!==null&&old>0&&holds('p99',n=>n>old*1.5))add('regression','주간 성능 저하 후보','predicted','warning',['p99','p99-week','requests'],'지난주 같은 시각 P99보다 50% 이상 높습니다.',['비슷한 부하에서 버전·쿼리·캐시 변화를 비교하세요.']);
 if(holds('cpu',n=>n>85)&&holds('p99',n=>n>500))add('cpu-pressure','CPU 포화와 응답 지연 동반','detected','critical',['cpu','p99','requests','requests-week'],'CPU 사용과 사용자 지연이 함께 높습니다.',['요청량이 증가했으면 처리 용량을 확인하세요.','동일 부하라면 배포된 코드와 프로파일을 확인하세요.']);
 const memory=map.get('memory')?.series[0]?.points.filter(p=>p.value!==null)||[];
 if(memory.length>=10&&memory.at(-1)!.time-memory[0].time>=300){const baselineMemory=memory.slice(0,Math.floor(memory.length/2)).reduce((s,p)=>s+p.value!,0)/Math.floor(memory.length/2);if(value('memory')!==null&&value('memory')!>baselineMemory+50&&value('memory')!>baselineMemory*1.5)add('memory-step','메모리 급상승 후보','detected','warning',['memory','inflight','gc-floor'],'최소 5분 관측의 앞 구간 평균보다 50MiB 및 50% 이상 상승했습니다.',['같은 시각의 대량 조회·파일 처리·다운로드 요청을 확인하세요.','페이지 처리 또는 스트리밍 가능 여부를 검토하세요.']);}
 const queueSlope=slope(map.get('queue'),300);if(queueSlope!==null&&queueSlope>.1&&holds('queue',n=>n>20))add('queue-growth','대기열 증가 지속','predicted','warning',['queue','queue-age','rejections'],'최소 5분간 초당 0.1개 이상 큐가 증가했습니다.',['유입률과 처리율의 차이, 소비자 상태를 확인하세요.','버퍼 상한과 배압을 점검하세요.']);
 const apiSeries=map.get('api-latency')?.series||[];const slowApis=apiSeries.filter(s=>{const last=s.points.at(-1);return last?.value!=null&&last.time>=snapshot.end-snapshot.step&&last.value>500});
 if(holds('api-latency',n=>n>500)&&slowApis.length){add('api-scope',slowApis.length===apiSeries.length?'관측 API 전반 지연':'특정 API 지연','detected','warning',['api-latency','db-latency','pool-wait'],`${apiSeries.length}개 관측 API 중 ${slowApis.length}개에서 현재 P99가 500ms를 초과합니다.`,[slowApis.length===apiSeries.length?'공유 DB·캐시·풀의 상태를 확인하세요.':'해당 API의 쿼리와 외부 호출을 확인하세요.']);}
 if(holds('targets-down',n=>n>0)&&holds('saturation',n=>n>80))add('cascade','서버 이탈과 처리 포화 동반','detected','critical',['targets-down','saturation','pool-active'],'수집 대상 이탈과 남은 처리 범위의 포화가 동반됩니다. 헬스체크 연쇄 실패는 추가 검증이 필요합니다.',['로드밸런서 헬스체크와 실제 요청 도달 여부를 확인하세요.','공유 의존 서비스와 재시도 증폭을 점검하세요.']);
 if(holds('cache-hit',n=>n<70)&&holds('db-latency',n=>n>100))add('stampede','캐시 저하와 DB 지연 동반','detected','warning',['cache-hit','db-latency','redis-expired'],'낮은 적중률과 쿼리 지연이 동반됩니다. 캐시 스탬피드는 원인 후보입니다.',['동시 TTL 만료와 축출량을 확인하세요.','키별 재생성 잠금·TTL 분산 정책을 검토하세요.']);
 if(holds('retries',n=>n>1)&&holds('errors',n=>n>2))add('retry-storm','실패·재시도 증폭 후보','detected','critical',['retries','errors','requests'],'재시도와 실패율이 함께 증가했습니다.',['재시도 총량, 지수 백오프와 지터를 확인하세요.']);
 if(value('timeout-db')!==null&&value('timeout-gateway')!==null&&value('timeout-db')!>=value('timeout-gateway')!)add('timeout','계층 타임아웃 예산 역전','detected','warning',['timeout-db','timeout-gateway','gateway-errors'],'DB 제한 시간이 게이트웨이보다 짧지 않습니다.',['애플리케이션·DB 제한을 바깥 계층보다 짧게 맞추세요.']);
 if(holds('slo-burn',n=>n>14.4)&&holds('slo-burn-hour',n=>n>14.4))add('slo','빠른 오류 예산 소진','predicted','critical',['slo-burn','slo-burn-hour','errors'],'99.9% 목표 기준 5분·1시간 창 모두 14.4배를 초과합니다.',['서비스별 승인 SLO 목표를 확인하세요.','변경 중단과 복구 우선순위를 검토하세요.']);
 if(value('deployment')!==null&&value('deployment')!<30&&holds('p99',n=>n>500,600))add('post-deploy','배포 후 지연 회복 지연','detected','warning',['deployment','p99','canary'],'최근 배포 이후 10분간 지연 기준을 초과했습니다.',['버전별·동일 부하로 카나리와 이전 버전을 비교하세요.','승인된 롤백 기준과 대조하세요.']);
 for(const [id,title,test,severity,kind,step]of simpleRules)if(holds(id,test,kind==='expiring'?30:60)){
  add(id,title,kind,severity,databaseEvidence[id]||[id],'선택 범위에서 기준 초과가 지속 관측되었습니다.',[step]);
  if(kind==='expiring'&&value(id)!==null){const factor=id==='cookie-expiry'?60:id==='tls-expiry'?86400:3600;incidents.at(-1)!.evidence.push(`만료 시각: ${new Date((snapshot.end+value(id)!*factor)*1000).toISOString()} (UTC)`);}
 }
 const known:Record<string,string>={HighServerErrorRatio:'errors',HighTailLatency:'latency',SessionExpiryApproaching:'cookie-expiry',TargetUnavailable:'targets-down',DatabaseExporterUnavailable:'db-up',CacheExporterUnavailable:'redis-up',DiskFullIn24Hours:'disk-risk'};
 for(const alert of snapshot.alerts){
   if(incidents.some(i=>i.id===known[alert.name]))continue;
   incidents.push({id:`prom:${alert.name}:${alert.instance}`,title:alert.summary,severity:alert.state==='pending'?'notice':alert.severity==='critical'?'critical':'warning',kind:'detected',status:alert.state==='pending'?'pending':'firing',scope:alert.instance,summary:alert.state==='pending'?'Prometheus가 경보 지속 조건을 평가하고 있습니다.':'Prometheus의 지속 조건을 충족한 수집기 경보입니다.',metricIds:known[alert.name]&&metricById.has(known[alert.name])?[known[alert.name]]:[],evidence:[`경보: ${alert.name}`,`상태: ${alert.state}`,`관측 시작: ${alert.activeAt}`],steps:['경보의 대상과 지표 근거를 확인하세요.','Grafana에서 발생 전후 시계열을 비교하세요.'],value:alert.state==='pending'?'평가 중':'발생 중',unit:'',firstSeen:alert.activeAt});
 }
 return incidents.sort((a,b)=>({critical:0,warning:1,notice:2}[a.severity]-{critical:0,warning:1,notice:2}[b.severity]));
}
export function formatNumber(n:number){return new Intl.NumberFormat('en-US',{maximumFractionDigits:Math.abs(n)<10?2:1}).format(n);}
