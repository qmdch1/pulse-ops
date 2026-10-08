import { detectIncidents } from './rules.js';
export const assetLabels = { server: '서버', application: '애플리케이션', postgres: 'PostgreSQL', mysql: 'MySQL', mariadb: 'MariaDB', oracle: 'Oracle', redis: 'Redis · 캐시', http: '웹 · HTTP' };
export const sqlDatabaseKinds = ['postgres', 'mysql', 'mariadb', 'oracle'];
export const isSQLDatabase = (kind) => sqlDatabaseKinds.includes(kind);
export const defaultPorts = { server: 22, postgres: 5432, mysql: 3306, mariadb: 3306, oracle: 1521, redis: 6379 };
export const databaseProfiles = {
    postgres: ['db-up', 'db-probe', 'db-connections', 'db-transactions', 'db-buffer', 'db-rollback', 'db-locks', 'db-deadlocks', 'db-replication'],
    mysql: ['db-up', 'db-probe', 'db-connections', 'db-connection-usage', 'db-active', 'db-connection-limit', 'db-statements', 'mysql-buffer-hit', 'db-slow-queries', 'db-lock-waiters', 'db-lock-waits', 'db-network-in', 'db-network-out', 'db-uptime', 'db-monitoring-ready'],
    mariadb: ['db-up', 'db-probe', 'db-connections', 'db-connection-usage', 'db-active', 'db-connection-limit', 'db-statements', 'mysql-buffer-hit', 'db-slow-queries', 'db-lock-waiters', 'db-lock-waits', 'db-network-in', 'db-network-out', 'db-uptime', 'db-monitoring-ready'],
    oracle: ['db-up', 'db-probe', 'db-connections', 'db-active', 'db-connection-usage', 'db-connection-limit', 'db-statements', 'db-transactions', 'db-rollback', 'oracle-buffer-hit', 'db-lock-waiters', 'db-network-in', 'db-network-out', 'db-monitoring-ready'],
};
export const statusLabels = { draft: '등록 초안', connecting: '연결 확인 중', connected: '수집 중', paused: '일시정지', error: '연결 확인 필요' };
export function scopeSnapshot(snapshot, id) {
    const asset = snapshot.assets?.find(a => a.id === id);
    return { ...snapshot, evaluationMetrics: snapshot.evaluationMetrics?.map(m => ({ ...m, series: m.series.filter(s => s.labels.assetId === id) })), assets: asset ? [asset] : [], targets: snapshot.targets.filter(t => t.instance === id), metrics: snapshot.metrics.map(m => {
            const series = m.series.filter(s => s.labels.assetId === id), point = series[0]?.points.at(-1), fresh = !!point && point.value != null && snapshot.end - point.time <= 45 && asset?.enabled;
            return { ...m, series, latest: fresh ? point.value : null, state: (!series.length ? 'missing' : fresh ? 'ok' : 'stale') };
        }), alerts: [] };
}
export function registeredIncidents(snapshot) {
    if (!snapshot.assets)
        return detectIncidents(snapshot);
    if (!snapshot.connected)
        return detectIncidents(snapshot);
    const evaluated = snapshot.evaluationMetrics ? { ...snapshot, metrics: snapshot.evaluationMetrics, step: 15, start: snapshot.end - 3600 } : snapshot;
    return snapshot.assets.filter(a => a.enabled).flatMap(a => {
        const found = detectIncidents(scopeSnapshot(evaluated, a.id)).map(i => ({ ...i, id: `${i.id}:${a.id}`, ruleId: i.id, assetId: a.id, scope: a.name }));
        if (a.status === 'error')
            found.unshift({ id: `connection:${a.id}`, ruleId: 'connection', assetId: a.id, title: '등록 인프라 연결 실패', severity: 'critical', kind: 'collection', summary: a.message, metricIds: ['targets-down'], evidence: [a.message], steps: ['등록 주소와 인증 정보를 확인하세요.', '점프 서버와 대상의 접근 경로를 확인하세요.'], value: '실패', unit: '', status: 'firing', scope: a.name });
        return found;
    });
}
export async function controlRequest(path, method = 'GET', body) {
    const response = await fetch(`/api/control/${path}`, { method, headers: body ? { 'Content-Type': 'application/json' } : undefined, body: body ? JSON.stringify(body) : undefined });
    const data = await response.json();
    if (!response.ok)
        throw new Error(data.error || '요청을 처리하지 못했습니다.');
    return data;
}
