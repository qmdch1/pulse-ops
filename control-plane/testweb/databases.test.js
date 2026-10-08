import {test} from './harness.js';
import {assert} from './harness.js';
import { assetLabels, databaseProfiles, defaultPorts, isSQLDatabase, registeredIncidents } from '/assets/lib/assets.js';
import { metricById } from '/assets/lib/catalog.js';
import { eventRules } from '/assets/lib/rule-catalog.js';
import { evidenceGroups } from '/assets/lib/evidence.js';
function asset(id, kind, dependencies = []) { return { id, kind, name: id, address: '', port: 0, username: '', database: '', tlsMode: '', os: '', metricsUrl: '', environment: 'test', ssh: { host: '', port: 0, username: '', jumpId: '', fingerprint: '' }, dependencies, enabled: true, version: 1, status: 'connected', lastSeen: '', message: '', hasPassword: false, hasSshPassword: false, hasPrivateKey: false, hasPassphrase: false }; }
function metric(id, assetId, value) { return { id, state: 'ok', latest: null, series: [{ labels: { assetId, name: assetId }, points: Array.from({ length: 21 }, (_, i) => ({ time: 9700 + i * 15, value })) }] }; }
function snapshot(kind) { return { mode: 'test', connected: true, collectedAt: '', start: 9400, end: 10000, step: 15, assets: [asset('app', 'application', ['db']), asset('db', kind)], metrics: [metric('p99', 'app', 600), metric('db-probe', 'db', 250), metric('db-up', 'db', 1)], targets: [], alerts: [], grafanaUrl: null }; }
test('all SQL engines expose valid dedicated dashboard metrics', () => {
    for (const kind of ['postgres', 'mysql', 'mariadb', 'oracle']) {
        assert.ok(isSQLDatabase(kind));
        assert.ok(assetLabels[kind]);
        assert.ok(defaultPorts[kind]);
        assert.ok(databaseProfiles[kind].every(id => metricById.has(id)));
    }
    assert.ok(!databaseProfiles.mysql.includes('db-transactions'));
    assert.ok(!databaseProfiles.oracle.includes('mysql-buffer-hit'));
    assert.equal(isSQLDatabase('redis'), false);
});
test('application evidence includes each registered database engine without borrowing its values', () => {
    for (const kind of ['mysql', 'mariadb', 'oracle']) {
        const s = snapshot(kind), lines = evidenceGroups(s, ['p99'], 'app').flat();
        assert.ok(lines.some(l => l.key === 'db-probe:db'));
        assert.ok(lines.some(l => l.key === 'db-up:db'));
        const events = registeredIncidents(s);
        assert.ok(events.some(i => i.ruleId === 'db-probe' && i.assetId === 'db'));
        assert.ok(events.find(i => i.ruleId === 'db-probe').metricIds.includes('db-lock-waiters'));
        assert.ok(!events.some(i => i.ruleId === 'db-probe' && i.assetId === 'app'));
    }
});
test('missing Oracle privileges raise a collection event without declaring database outage', () => {
    const s = snapshot('oracle');
    s.metrics.push(metric('db-monitoring-ready', 'db', 0));
    const events = registeredIncidents(s);
    assert.ok(events.some(i => i.ruleId === 'db-monitoring-ready' && i.kind === 'collection'));
    assert.ok(!events.some(i => i.ruleId === 'db-up' || i.ruleId === 'connection'));
    const rule = eventRules.find(r => r.id === 'db-probe');
    assert.ok(rule.metricIds.includes('db-lock-waiters'));
    assert.deepEqual(rule.requiredIds, ['db-probe']);
});
