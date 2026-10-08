// One catalog shared with the Go snapshot endpoint. No bundler or generated client.
const response = await fetch(new URL('../data/metrics.json', import.meta.url));
if (!response.ok) throw new Error('지표 정의를 불러오지 못했습니다.');
export const metrics = await response.json();
export const metricById = new Map(metrics.map(metric => [metric.id, metric]));
export const groupLabels = {"golden":"트래픽 & 응답","resources":"서버 리소스","runtime":"런타임 & 프로세스","database":"데이터베이스 & 캐시","network":"네트워크 & 큐","security":"인증 & 만료","changes":"배포 & 신뢰성"};
export function scopedQuery(query, instance='all') {
 if(instance.length>200) throw new Error('Invalid instance');
 return query.replaceAll('__SCOPE__', instance==='all'?'instance=~".+"':'instance='+JSON.stringify(instance));
}
