import {test} from './harness.js';
import {assert} from './harness.js';
import { metrics, scopedQuery } from '/assets/lib/catalog.js';
import { sustained, slope, detectIncidents } from '/assets/lib/rules.js';
const end = 10000, step = 10;
const metric = (id, value, n = 20) => ({ id, state: value === null ? 'missing' : 'ok', latest: value, series: [{ labels: {}, points: Array.from({ length: n }, (_, i) => ({ time: end - (n - 1 - i) * step, value })) }] });
const snapshot = (values) => ({ mode: 'test', connected: true, collectedAt: new Date().toISOString(), start: 9000, end, step, metrics: values, targets: [], alerts: [], grafanaUrl: null });
test('metric identities are unique and queries use a fixed scoped contract', () => { assert.equal(new Set(metrics.map(m => m.id)).size, metrics.length); for (const m of metrics) {
    assert.ok(m.query.includes('__SCOPE__') || m.source === '직접 수집');
    assert.ok(m.title && m.source && m.description && m.unit);
} });
test('P97 and P99 aggregate buckets before quantile', () => { for (const id of ['p97', 'p99'])
    assert.match(metrics.find(m => m.id === id).query, /histogram_quantile.*sum by \(le\)/); });
test('label input is escaped rather than interpolated as PromQL', () => { const query = scopedQuery('up{__SCOPE__}', 'x"} or vector(1) #'); assert.equal(query, 'up{instance="x\\\"} or vector(1) #"}'); assert.throws(() => scopedQuery('up{__SCOPE__}', 'x'.repeat(201))); });
test('sustained threshold rejects short spikes, gaps, nulls and stale evidence', () => { assert.equal(sustained(metric('p99', 800), n => n > 500, 120, end, step), true); assert.equal(sustained(metric('p99', 800, 5), n => n > 500, 120, end, step), false); const gap = metric('p99', 800); gap.series[0].points.splice(12, 4); assert.equal(sustained(gap, n => n > 500, 120, end, step), false); const unknown = metric('p99', 800); unknown.series[0].points[10].value = null; assert.equal(sustained(unknown, n => n > 500, 120, end, step), false); assert.equal(sustained({ ...metric('p99', 800), state: 'stale' }, n => n > 500, 120, end, step), false); });
test('missing data never fires numerical threshold events', () => assert.deepEqual(detectIncidents(snapshot([metric('errors', null), metric('cookie-expiry', null)])), []));
test('cookie expiry and expired cookies both alert with evidence', () => { for (const n of [20, -5]) {
    const events = detectIncidents(snapshot([metric('cookie-expiry', n)]));
    assert.equal(events[0].kind, 'expiring');
    assert.ok(events[0].evidence.length);
} });
test('correlated I/O delay requires available CPU evidence', () => { assert.ok(!detectIncidents(snapshot([metric('p99', 800)])).some(e => e.id === 'io')); assert.ok(detectIncidents(snapshot([metric('p99', 800), metric('cpu', 20)])).some(e => e.id === 'io')); });
test('prediction rejects insufficient history', () => { assert.equal(slope(metric('gc-floor', 50), 1800), null); const m = metric('gc-floor', 50, 200); m.series[0].points.forEach((p, i) => p.value = 50 + i); assert.ok(slope(m, 1800) > 0); });
test('two-window SLO burn must agree', () => { assert.ok(!detectIncidents(snapshot([metric('slo-burn', 20), metric('slo-burn-hour', 2)])).some(e => e.id === 'slo')); assert.ok(detectIncidents(snapshot([metric('slo-burn', 20), metric('slo-burn-hour', 20)])).some(e => e.id === 'slo')); });
test('disconnected collector never reuses business events', () => { const s = snapshot([metric('p99', 900)]); s.connected = false; assert.deepEqual(detectIncidents(s).map(e => e.id), ['collector']); });
test('custom Prometheus alerts are visible and pending is distinct', () => { const s = snapshot([]); s.alerts = [{ name: 'CustomContractFailure', state: 'pending', severity: 'critical', instance: 'test', activeAt: '2026-10-08T00:00:00Z', summary: 'Custom failure' }]; const e = detectIncidents(s)[0]; assert.equal(e.status, 'pending'); assert.equal(e.severity, 'notice'); assert.equal(e.scope, 'test'); });
