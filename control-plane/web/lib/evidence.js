import { metricById } from './catalog.js';
import { isSQLDatabase } from './assets.js';
export function evidenceGroups(snapshot, ids, assetId) {
    const asset = snapshot.assets?.find(a => a.id === assetId), related = new Set([assetId, ...asset?.dependencies || []]);
    const requested = new Set(ids);
    // Direct dependencies expose actual round-trip and health evidence alongside application latency.
    for (const a of snapshot.assets || []) {
        if (a.id === assetId || !related.has(a.id))
            continue;
        if (isSQLDatabase(a.kind)) {
            requested.add('db-probe');
            requested.add('db-up');
        }
        else if (a.kind === 'redis') {
            requested.add('redis-probe');
            requested.add('redis-up');
        }
        else if (a.kind === 'server') {
            requested.add('node-cpu');
            requested.add('memory-host');
        }
        else if (a.kind === 'http') {
            requested.add('probe-latency');
            requested.add('probe');
        }
    }
    const byUnit = new Map();
    for (const id of requested) {
        const metric = metricById.get(id);
        if (!metric)
            continue;
        const result = snapshot.metrics.find(m => m.id === id);
        for (const series of result?.series || []) {
            if (!related.has(series.labels.assetId) || !series.points.some(p => p.value !== null))
                continue;
            const line = { key: `${id}:${series.labels.assetId}`, metricId: id, name: `${series.labels.name} · ${metric.title}`, unit: metric.unit, points: series.points };
            byUnit.set(metric.unit, [...byUnit.get(metric.unit) || [], line]);
        }
    }
    const units = [...byUnit.keys()], groups = [];
    for (let i = 0; i < units.length; i += 2)
        groups.push([...(byUnit.get(units[i]) || []), ...(byUnit.get(units[i + 1]) || [])]);
    return groups;
}
