import {test} from './harness.js';
import {assert} from './harness.js';
import { registeredIncidents, scopeSnapshot } from '/assets/lib/assets.js';
import { evidenceGroups } from '/assets/lib/evidence.js';
import { ruleState, eventRules } from '/assets/lib/rule-catalog.js';
const asset = (id, dependencies = []) => ({ id, name: id, kind: id === 'db' ? 'postgres' : 'application', address: '', port: 0, username: '', database: '', tlsMode: '', os: '', metricsUrl: '', environment: 'test', ssh: { host: '', port: 0, username: '', jumpId: '', fingerprint: '' }, dependencies, enabled: true, version: 1, status: 'connected', lastSeen: '', message: '', hasPassword: false, hasSshPassword: false, hasPrivateKey: false, hasPassphrase: false });
const metric = (id, assetId, value) => ({ id, state: 'ok', latest: null, series: [{ labels: { assetId, name: assetId }, points: Array.from({ length: 21 }, (_, i) => ({ time: 9700 + i * 15, value })) }] });
const snapshot = (metrics) => ({ mode: 'test', connected: true, collectedAt: '', start: 9400, end: 10000, step: 15, assets: [asset('api-a', ['db']), asset('api-b'), asset('db')], metrics, targets: [], alerts: [], grafanaUrl: null });
test('events cannot borrow low CPU or pool usage from an unrelated server', () => {
    const s = snapshot([metric('p99', 'api-a', 700), metric('cpu', 'api-b', 10), metric('pool-active', 'api-a', 95), metric('pool-wait', 'api-b', 300)]);
    const incidents = registeredIncidents(s);
    assert.ok(incidents.some(i => i.ruleId === 'latency' && i.assetId === 'api-a'));
    assert.ok(!incidents.some(i => i.ruleId === 'io' || i.ruleId === 'pool'));
    assert.equal(ruleState(eventRules.find(r => r.id === 'pool'), s, incidents), 'missing');
    assert.equal(scopeSnapshot(s, 'api-a').metrics.find(m => m.id === 'cpu')?.latest, null);
});
test('evidence combines matching units, keeps unit axes separate and excludes unrelated assets', () => {
    const s = snapshot([metric('p99', 'api-a', 500), metric('p99', 'api-b', 100), metric('cpu', 'api-a', 45), metric('db-probe', 'db', 10), metric('db-up', 'db', 1), metric('requests', 'api-a', 20)]);
    const groups = evidenceGroups(s, ['p99', 'cpu', 'requests'], 'api-a'), lines = groups.flat();
    assert.ok(lines.some(l => l.key === 'db-probe:db'));
    assert.ok(!lines.some(l => l.key.includes('api-b')));
    assert.ok(groups.every(g => new Set(g.map(l => l.unit)).size <= 2));
    assert.ok(groups.some(g => g.some(l => l.key === 'p99:api-a') && g.some(l => l.key === 'db-probe:db')));
    assert.ok(lines.every(l => l.points.length === 21));
});
test('paused or stale observations never become current incidents', () => {
    const s = snapshot([metric('errors', 'api-a', 10)]);
    s.assets[0].enabled = false;
    assert.equal(registeredIncidents(s).length, 0);
    s.assets[0].enabled = true;
    s.end += 90;
    assert.equal(registeredIncidents(s).length, 0);
});
test('changing chart resolution does not change event evaluation evidence', () => {
    const s = snapshot([metric('errors', 'api-a', 0)]);
    s.step = 720;
    s.evaluationMetrics = [metric('errors', 'api-a', 8)];
    assert.ok(registeredIncidents(s).some(i => i.ruleId === 'errors' && i.assetId === 'api-a'));
    assert.ok(registeredIncidents({ ...s, step: 15 }).some(i => i.ruleId === 'errors' && i.assetId === 'api-a'));
});
