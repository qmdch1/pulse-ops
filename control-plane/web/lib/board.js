import { metrics, metricById } from './catalog.js';
import { databaseProfiles, isSQLDatabase, scopeSnapshot } from './assets.js';
import { eventRules } from './rule-catalog.js';
const severityRank = { critical: 0, warning: 1, notice: 2 };
export function groupEvents(incidents) {
    const groups = new Map();
    for (const incident of incidents) {
        const id = incident.ruleId || incident.id, rule = eventRules.find(r => r.id === id);
        const group = groups.get(id) || { id, title: rule?.title || incident.title, severity: incident.severity, kind: incident.kind, incidents: [], assetIds: [], metricIds: [] };
        group.incidents.push(incident);
        if (incident.assetId && !group.assetIds.includes(incident.assetId))
            group.assetIds.push(incident.assetId);
        group.metricIds = [...new Set([...group.metricIds, ...incident.metricIds, ...(rule?.metricIds || [])])];
        if (severityRank[incident.severity] < severityRank[group.severity])
            group.severity = incident.severity;
        groups.set(id, group);
    }
    return [...groups.values()].sort((a, b) => severityRank[a.severity] - severityRank[b.severity] || b.assetIds.length - a.assetIds.length || a.id.localeCompare(b.id));
}
export const contextMetrics = {
    server: ['node-cpu', 'memory-host', 'load', 'disk', 'network-in', 'network-out'],
    application: ['requests', 'p99', 'p97', 'errors', 'cpu', 'memory', 'pool-wait', 'db-latency', 'cache-hit'],
    postgres: ['db-up', 'db-probe', 'db-connections', 'db-transactions', 'db-locks', 'db-rollback', 'db-buffer'],
    mysql: ['db-up', 'db-probe', 'db-connections', 'db-active', 'db-connection-usage', 'db-lock-waiters', 'mysql-buffer-hit'],
    mariadb: ['db-up', 'db-probe', 'db-connections', 'db-active', 'db-connection-usage', 'db-lock-waiters', 'mysql-buffer-hit'],
    oracle: ['db-up', 'db-probe', 'db-connections', 'db-active', 'db-connection-usage', 'db-lock-waiters', 'oracle-buffer-hit'],
    redis: ['redis-up', 'redis-probe', 'redis-hit', 'redis-memory', 'redis-blocked', 'redis-expired'],
    http: ['probe', 'probe-latency', 'http-status', 'tls-expiry'],
};
const sources = {
    server: ['Node exporter', 'cAdvisor', 'kube-state-metrics'],
    application: ['Application', 'Process collector', 'Session exporter', 'Credential exporter', 'Node.js exporter'],
    redis: ['Redis exporter'], http: ['Blackbox exporter'],
};
export function metricsForAsset(snapshot, asset) {
    const directProfiles = { server: ['uptime', 'cpu', 'disk-total', 'disk-used', 'disk-free'], redis: ['redis-used', 'redis-clients', 'redis-commands'] };
    const ids = new Set([...(databaseProfiles[asset.kind] || contextMetrics[asset.kind] || []), ...(directProfiles[asset.kind] || []), 'targets', 'targets-down', 'scrape-duration', 'scrape-age']);
    for (const m of snapshot.metrics)
        if (m.series.some(s => s.labels.assetId === asset.id && s.points.some(p => p.value !== null)))
            ids.add(m.id);
    return metrics.filter(m => ids.has(m.id) || (!isSQLDatabase(asset.kind) && (sources[asset.kind] || []).includes(m.source)));
}
export function observedForAsset(snapshot, metricId, assetId) {
    return !!snapshot.metrics.find(m => m.id === metricId)?.series.some(s => s.labels.assetId === assetId && s.points.some(p => p.value !== null));
}
export function metricRecipes(snapshot, ids, definitions) {
    return arrangeFamilies(definitions.flatMap(metric => {
        const parts = chartGroups(comparisonLines(snapshot, ids, [metric.id]));
        return (parts.length ? parts : [[]]).map((lines, index) => ({
            title: metric.title + (index ? ' · ' + (index + 1) : ''),
            subtitle: metric.description, assetIds: ids, metricIds: [metric.id],
            keys: lines.length ? lines.map(line => line.key) : undefined,
            metric, unit: metric.unit, warning: metric.warning, part: index,
        }));
    }));
}
// One quantity measured at several percentiles, windows, directions or policies.
// Members share a unit and read best side by side on one axis.
export const metricFamilies = [
    { id: 'latency', title: '응답 시간 분포', members: [['p50', 'P50'], ['p95', 'P95'], ['p97', 'P97'], ['p99', 'P99'], ['p99.9', 'P99.9'], ['latency-mean', '평균']] },
    { id: 'outcome-latency', title: '성공·실패 요청 P99', members: [['success-latency', '성공'], ['failed-latency', '실패']] },
    { id: 'event-loop', title: '이벤트 루프 지연', members: [['loop-p99', 'P99'], ['loop-max', '최대']] },
    { id: 'slo-burn', title: '오류 예산 소진 속도', members: [['slo-burn', '5분'], ['slo-burn-hour', '1시간']] },
    { id: 'disk-capacity', title: '디스크 용량', members: [['disk-total', '전체'], ['disk-used', '사용'], ['disk-free', '남음']] },
    { id: 'network', title: '네트워크 송수신', members: [['network-in', '수신'], ['network-out', '송신']] },
    { id: 'db-network', title: 'DB 송수신', members: [['db-network-in', '수신'], ['db-network-out', '송신']] },
    { id: 'cookie-policy', title: '세션 쿠키 정책', members: [['cookie-secure', 'Secure'], ['cookie-http', 'HttpOnly'], ['cookie-samesite', 'SameSite']] },
];
const familyMembers = new Map(metricFamilies.flatMap(family => family.members.map(([id, label], index) => [id, { family, label, index }])));
const memberOf = recipe => recipe.metricIds.length === 1 ? familyMembers.get(recipe.metricIds[0]) : undefined;
// A family row sits where its first member would, one row per split part, in
// family order. A family with one present member stays an ordinary chart.
// Titles and keys are untouched so saved merges and refresh periods still match.
export function arrangeFamilies(recipes) {
    const present = new Map();
    for (const recipe of recipes) {
        const member = memberOf(recipe);
        if (member)
            present.set(member.family.id, new Set([...(present.get(member.family.id) || []), recipe.metricIds[0]]));
    }
    const rows = new Map(), result = [];
    for (const recipe of recipes) {
        const member = memberOf(recipe);
        if (!member || present.get(member.family.id).size < 2) {
            result.push(recipe);
            continue;
        }
        const part = recipe.part || 0, row = `${member.family.id}:${part}`;
        if (!rows.has(row)) {
            rows.set(row, []);
            result.push(rows.get(row));
        }
        rows.get(row).push({ ...recipe, family: { id: member.family.id, row, title: member.family.title + (part ? ' · ' + (part + 1) : ''), label: member.label, index: member.index } });
    }
    return result.flatMap(item => Array.isArray(item) ? item.sort((a, b) => a.family.index - b.family.index) : [item]);
}
// The overview is deliberately small. Full collection and event evidence use
// metricsForAsset/eventMetricIds and never inherit this display filter.
export const dashboardMetrics = {
    application: ['requests', 'p99', 'errors', 'cpu', 'memory'],
    server: ['node-cpu', 'cpu', 'memory-host', 'disk', 'disk-total', 'disk-used', 'disk-free', 'network-in', 'network-out'],
    postgres: ['db-probe', 'db-transactions', 'db-connections', 'db-locks'],
    mysql: ['db-probe', 'db-statements', 'db-connection-usage', 'db-lock-waiters'],
    mariadb: ['db-probe', 'db-statements', 'db-connection-usage', 'db-lock-waiters'],
    oracle: ['db-probe', 'db-statements', 'db-connection-usage', 'db-lock-waiters'],
    redis: ['redis-probe', 'redis-commands', 'redis-used', 'redis-hit'],
    http: ['probe-latency'],
};
// CPU and memory read together when applications and servers are selected at once.
const dashboardOrder = [...new Set(['requests', 'p99', 'errors', 'node-cpu', 'cpu', 'memory-host', 'memory', ...Object.values(dashboardMetrics).flat()])];
// A dashboard chart belongs to a metric; each eligible asset keeps its own line.
export function dashboardRecipes(snapshot, ids) {
    const selected = snapshot.assets.filter(asset => ids.includes(asset.id));
    return arrangeFamilies(dashboardOrder.flatMap(id => {
        const owners = selected.filter(asset => (dashboardMetrics[asset.kind] || []).includes(id) && observedForAsset(snapshot, id, asset.id));
        return owners.length ? metricRecipes(snapshot, owners.map(asset => asset.id), [metricById.get(id)]).map(recipe => ({...recipe, subtitle: undefined})) : [];
    }));
}
export function relatedAssets(snapshot, rootIds) {
    const byId = new Map(snapshot.assets?.map(a => [a.id, a]) || []), seen = new Set(), pending = [...rootIds];
    while (pending.length) {
        const id = pending.shift();
        if (seen.has(id))
            continue;
        const asset = byId.get(id);
        if (!asset)
            continue;
        seen.add(id);
        pending.push(...asset.dependencies);
    }
    return [...seen].map(id => byId.get(id));
}
export function eventMetricIds(group, assets) { return [...new Set([...group.metricIds, ...assets.flatMap(a => contextMetrics[a.kind] || [])])]; }
// Comparison is a presentation scope only. No averaging of percentiles and no
// sharing measurements between assets during incident evaluation.
export function comparisonSnapshot(snapshot, ids) {
    const allowed = new Set(ids), assets = snapshot.assets?.filter(a => allowed.has(a.id)) || [];
    if (assets.length === 1)
        return scopeSnapshot(snapshot, assets[0].id);
    const restrict = (rows) => rows.map(m => {
        const series = m.series.filter(s => allowed.has(s.labels.assetId));
        const live = series.some(s => assets.find(a => a.id === s.labels.assetId)?.enabled && s.points.at(-1)?.value != null && snapshot.end - s.points.at(-1).time <= 45);
        return { ...m, series, latest: null, state: (!series.length ? 'missing' : live ? 'ok' : 'stale') };
    });
    return { ...snapshot, assets, metrics: restrict(snapshot.metrics), evaluationMetrics: snapshot.evaluationMetrics ? restrict(snapshot.evaluationMetrics) : undefined, targets: snapshot.targets.filter(t => allowed.has(t.instance)), alerts: [] };
}
export function comparisonLines(snapshot, ids, metricIds) {
    const allowed = new Set(ids), lines = [];
    for (const id of [...new Set(metricIds)]) {
        const metric = metricById.get(id);
        if (!metric)
            continue;
        for (const series of snapshot.metrics.find(m => m.id === id)?.series || []) {
            if (!allowed.has(series.labels.assetId) || !series.points.some(p => p.value !== null))
                continue;
            const assetName = series.labels.name || series.labels.assetId;
            lines.push({ key: `${id}:${series.labels.assetId}`, metricId: id, assetId: series.labels.assetId, assetName, name: `${assetName} · ${metric.title}`, unit: metric.unit, points: series.points });
        }
    }
    return lines;
}
// Preserve every selected line, split crowded charts rather than truncating.
export function chartGroups(lines, maxLines = 8) {
    const byUnit = new Map();
    for (const line of lines)
        byUnit.set(line.unit, [...(byUnit.get(line.unit) || []), line]);
    const groups = [];
    for (const values of byUnit.values())
        for (let i = 0; i < values.length; i += maxLines) {
            const chunk = values.slice(i, i + maxLines), last = groups.at(-1);
            if (last && last.length + chunk.length <= maxLines && new Set([...last, ...chunk].map(l => l.unit)).size <= 2)
                last.push(...chunk);
            else
                groups.push(chunk);
        }
    return groups;
}
export function chartRows(lines, step) {
    const rows = new Map(), bucket = Math.max(1, step);
    lines.forEach((line, index) => line.points.forEach(point => {
        const time = Math.floor(point.time / bucket) * bucket;
        const row = rows.get(time) || { time };
        row[`v${index}`] = point.value;
        rows.set(time, row);
    }));
    const ordered = [...rows.values()].sort((a, b) => Number(a.time) - Number(b.time)), result = [];
    for (const row of ordered) {
        const previous = result.at(-1);
        // An absent collection interval must break the line, including when every
        // selected asset has a gap and there is no other series to supply a row.
        if (previous && Number(row.time) - Number(previous.time) > bucket) {
            const gap = { time: Number(previous.time) + bucket };
            lines.forEach((_, i) => gap[`v${i}`] = null);
            result.push(gap);
        }
        result.push(row);
    }
    return result;
}
export function boardSelection(raw, assets) {
    if (!Array.isArray(raw))
        return assets.map(a => a.id);
    const allowed = new Set(assets.map(a => a.id));
    return [...new Set(raw.filter((id) => typeof id === 'string' && allowed.has(id)))];
}
