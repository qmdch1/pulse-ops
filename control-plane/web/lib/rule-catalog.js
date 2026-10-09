import { metricById } from './catalog.js';
const response = await fetch('/assets/data/event-rules.json');
if (!response.ok) throw new Error('이벤트 규칙을 읽을 수 없습니다.');
export const eventRules = await response.json();
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
