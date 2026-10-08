import { metricById } from './catalog.js';
import { simpleRules } from './rules.js';
import { databaseEvidence } from './database-evidence.js';
const rule = (id, title, ids, condition, duration, severity = 'warning', kind = 'detected', requiredIds = ids) => ({ id, title, metricIds: ids, condition, duration, severity, kind, requiredIds });
export const eventRules = [
    rule('collector', '수집기 연결 중단', [], 'Go 인프라 관리 서비스에 연결할 수 없음', '즉시', 'critical', 'collection'),
    rule('connection', '등록 인프라 연결 실패', ['targets-down'], '등록된 대상의 최근 연결 시도가 실패함', '최근 실패 즉시 · 재시도 최대 5분', 'critical', 'collection'),
    rule('errors', '서버 오류율 증가', ['errors', 'p99', 'requests'], '5xx 비율 > 2%', '2분 지속', 'critical', 'detected', ['errors']),
    rule('latency', '꼬리 응답 지연 지속', ['p99', 'cpu', 'pool-wait'], 'P99 > 500ms', '2분 지속', 'critical', 'detected', ['p99']),
    rule('pool', 'DB 커넥션 풀 병목 후보', ['pool-active', 'pool-wait', 'db-latency'], '풀 사용률 > 80% AND 연결 대기 > 100ms', '각 2분 지속', 'critical', 'detected', ['pool-active', 'pool-wait']),
    rule('io', 'CPU 여유 상태의 지연 증가', ['p99', 'cpu', 'db-latency'], 'P99 > 500ms AND 현재 CPU < 35%', 'P99 2분 지속', 'warning', 'detected', ['p99', 'cpu']),
    rule('fast-fail', '빠른 실패로 가려진 장애 후보', ['errors', 'p99', 'failed-latency'], '5xx > 2% AND 현재 P99 < 200ms', '오류율 2분 지속', 'critical', 'detected', ['errors', 'p99']),
    rule('throttle', 'CPU 제한과 꼬리 지연 동반', ['throttling', 'p99', 'cpu'], 'CPU 제한 비율 > 20% AND P99 > 500ms', '각 2분 지속', 'warning', 'detected', ['throttling', 'p99']),
    rule('leak', 'GC 이후 메모리 상승 추세', ['gc-floor', 'gc-pause', 'memory'], 'GC 후 메모리 선형 증가율 > 1MiB/분', '30분 이상 · 10개 이상 표본', 'warning', 'predicted', ['gc-floor']),
    rule('disk-risk', '24시간 내 디스크 소진 가능', ['disk-forecast', 'disk-free', 'disk'], '6시간 추세로 예측한 24시간 뒤 여유 공간 < 0', '15분 지속 · 6시간 전 표본 필요', 'warning', 'predicted', ['disk-forecast']),
    rule('traffic-drop', '평소 대비 트래픽 감소', ['requests', 'requests-week', 'probe'], '지난주 동일 시각 > 1req/s AND 현재 요청량 < 지난주의 50%', '요청량 2분 지속', 'warning', 'detected', ['requests', 'requests-week']),
    rule('traffic-spike', '평소 대비 트래픽 급증', ['requests', 'requests-week', 'retries'], '지난주 동일 시각 > 1req/s AND 현재 요청량 > 지난주의 2배', '요청량 2분 지속', 'warning', 'detected', ['requests', 'requests-week']),
    rule('regression', '주간 성능 저하 후보', ['p99', 'p99-week', 'requests'], '지난주 P99 > 0 AND 현재 P99 > 지난주의 1.5배', 'P99 2분 지속', 'warning', 'predicted', ['p99', 'p99-week']),
    rule('cpu-pressure', 'CPU 포화와 응답 지연 동반', ['cpu', 'p99', 'requests', 'requests-week'], 'CPU > 85% AND P99 > 500ms', '각 2분 지속', 'critical', 'detected', ['cpu', 'p99']),
    rule('memory-step', '메모리 급상승 후보', ['memory', 'inflight', 'gc-floor'], '현재 RSS > 앞 절반 평균 + 50MiB AND > 평균 × 1.5', '5분 이상 · 10개 이상 표본', 'warning', 'detected', ['memory']),
    rule('queue-growth', '대기열 증가 지속', ['queue', 'queue-age', 'rejections'], '큐 선형 증가율 > 0.1개/초 AND 큐 > 20개', '추세 5분 이상 · 깊이 2분 지속', 'warning', 'predicted', ['queue']),
    rule('api-scope', '특정 API 또는 API 전반 지연', ['api-latency', 'db-latency', 'pool-wait'], '관측 API 중 하나 이상 P99 > 500ms; 현재 느린 API 수로 범위 구분', '2분 지속', 'warning', 'detected', ['api-latency']),
    rule('cascade', '서버 이탈과 처리 포화 동반', ['targets-down', 'saturation', 'pool-active'], '수집 중단 대상 > 0 AND 처리 용량 사용률 > 80%', '각 2분 지속', 'critical', 'detected', ['targets-down', 'saturation']),
    rule('stampede', '캐시 저하와 DB 지연 동반', ['cache-hit', 'db-latency', 'redis-expired'], '캐시 적중률 < 70% AND DB 지연 > 100ms', '각 2분 지속', 'warning', 'detected', ['cache-hit', 'db-latency']),
    rule('retry-storm', '실패·재시도 증폭 후보', ['retries', 'errors', 'requests'], '재시도 > 1회/초 AND 5xx 비율 > 2%', '각 2분 지속', 'critical', 'detected', ['retries', 'errors']),
    rule('timeout', '계층 타임아웃 예산 역전', ['timeout-db', 'timeout-gateway', 'gateway-errors'], 'DB 타임아웃 ≥ 게이트웨이 타임아웃', '현재 설정 비교', 'warning', 'detected', ['timeout-db', 'timeout-gateway']),
    rule('slo', '빠른 오류 예산 소진', ['slo-burn', 'slo-burn-hour', 'errors'], '5분 AND 1시간 소진율 > 14.4배 · SLO 99.9% 기준', '두 창 각 2분 지속', 'critical', 'predicted', ['slo-burn', 'slo-burn-hour']),
    rule('post-deploy', '배포 후 지연 회복 지연', ['deployment', 'p99', 'canary'], '배포 후 30분 미만 AND P99 > 500ms', 'P99 10분 지속', 'warning', 'detected', ['deployment', 'p99']),
    ...simpleRules.map(([id, title, , severity, kind]) => {
        const metric = metricById.get(id);
        return rule(id, title, databaseEvidence[id] || [id], `${metric.title} ${metric.direction === 'below' ? '<' : '>'} ${metric.warning} ${metric.unit}`, kind === 'expiring' ? '30초 지속' : '1분 지속', severity, kind, [id]);
    }),
];
export const ruleById = new Map(eventRules.map(rule => [rule.id, rule]));
export function ruleState(rule, snapshot, incidents) {
    if (incidents.some(i => (i.ruleId || i.id) === rule.id && i.status === 'firing'))
        return 'firing';
    if (incidents.some(i => (i.ruleId || i.id) === rule.id && i.status === 'pending'))
        return 'pending';
    if (!snapshot || snapshot.mode === 'unconfigured' || !snapshot.connected)
        return 'missing';
    if (snapshot.assets && snapshot.assets.length > 1)
        return snapshot.assets.some(a => a.enabled && rule.requiredIds.every(id => snapshot.metrics.find(m => m.id === id)?.series.some(s => s.labels.assetId === a.id && s.points.at(-1)?.value != null && snapshot.end - s.points.at(-1).time <= 45))) ? 'waiting' : 'missing';
    const observed = new Set(snapshot.metrics.filter(m => m.state === 'ok').map(m => m.id));
    return rule.requiredIds.every(id => observed.has(id)) ? 'waiting' : 'missing';
}
