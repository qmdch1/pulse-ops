import {test} from './harness.js';
import {assert} from './harness.js';
import { boardSelection, chartGroups, chartRows, comparisonLines, comparisonSnapshot, dashboardRecipes, eventMetricIds, groupEvents, metricsForAsset, observedForAsset, relatedAssets } from '/assets/lib/board.js';
import { registeredIncidents } from '/assets/lib/assets.js';
const asset = (id, kind = 'application', dependencies = []) => ({ id, name: id, kind, address: '', port: 0, username: '', database: '', tlsMode: '', os: '', metricsUrl: '', environment: 'test', ssh: { host: '', port: 0, username: '', jumpId: '', fingerprint: '' }, dependencies, enabled: true, version: 1, status: 'connected', lastSeen: '', message: '', hasPassword: false, hasSshPassword: false, hasPrivateKey: false, hasPassphrase: false });
const metric = (id, values) => ({ id, state: 'ok', latest: null, series: Object.entries(values).map(([assetId, value]) => ({ labels: { assetId, name: assetId }, points: Array.from({ length: 21 }, (_, i) => ({ time: 9700 + i * 15, value })) })) });
const snapshot = (rows = []) => ({ assets: [asset('api-a', 'application', ['db']), asset('api-b'), asset('db', 'mysql', ['host']), asset('host', 'server', ['api-a'])], mode: 'test', connected: true, collectedAt: '', start: 9700, end: 10000, step: 15, metrics: rows, targets: [], alerts: [], grafanaUrl: null });
const incident = (id, severity = 'warning') => ({ id: `latency:${id}`, ruleId: 'latency', assetId: id, title: id, severity, kind: 'detected', summary: '', metricIds: ['p99'], evidence: [id], steps: ['inspect'], value: '700', unit: 'ms', status: 'firing', scope: id });
test('event groups retain every affected asset and evidence, with the strongest severity', () => {
    const groups = groupEvents([incident('api-a'), incident('api-b', 'critical'), { ...incident('db'), id: 'connection:db', ruleId: 'connection' }]);
    assert.equal(groups.length, 2);
    const latency = groups.find(g => g.id === 'latency');
    assert.equal(groups[0], latency);
    assert.equal(latency.severity, 'critical');
    assert.deepEqual(latency.assetIds, ['api-a', 'api-b']);
    assert.deepEqual(latency.incidents.flatMap(i => i.evidence), ['api-a', 'api-b']);
    assert.ok(latency.metricIds.includes('p99'));
});
test('event investigation follows registered dependencies transitively, with cycle and missing reference protection', () => {
    const s = snapshot();
    s.assets[2].dependencies.push('deleted');
    const related = relatedAssets(s, ['api-a']);
    assert.deepEqual(related.map(a => a.id), ['api-a', 'db', 'host']);
    assert.deepEqual(relatedAssets(s, ['deleted']), []);
    const ids = eventMetricIds(groupEvents([incident('api-a')])[0], related);
    assert.ok(ids.includes('p99') && ids.includes('db-probe') && ids.includes('node-cpu'));
    assert.equal(ids.length, new Set(ids).size);
});
test('comparison retains independent P99 values and excludes unselected assets', () => {
    const s = snapshot([metric('p99', { 'api-a': 700, 'api-b': 10 }), metric('db-probe', { db: 5 })]);
    const lines = comparisonLines(s, ['api-a', 'db'], ['p99', 'db-probe', 'p99']);
    assert.deepEqual(lines.map(l => l.key), ['p99:api-a', 'db-probe:db']);
    assert.equal(lines[0].points.at(-1).value, 700);
    const multi = comparisonSnapshot(s, ['api-a', 'api-b']);
    assert.equal(multi.metrics[0].latest, null);
    assert.equal(multi.metrics[0].series.length, 2);
    assert.equal(comparisonSnapshot(s, ['api-a']).metrics[0].latest, 700);
    assert.equal(comparisonSnapshot(s, []).metrics[0].state, 'missing');
});
test('dashboard automatically shares matching server metrics and retains each selected server value', () => {
    const s = snapshot([metric('node-cpu', { 'host-a': 12, 'host-b': 37, 'host-c': 99 }), metric('memory-host', { 'host-a': 40, 'host-b': 65 })]);
    s.assets = ['host-a', 'host-b', 'host-c'].map(id => asset(id, 'server'));
    const recipes = dashboardRecipes(s, ['host-a', 'host-b']);
    assert.equal(recipes.length, 2);
    for (const id of ['node-cpu', 'memory-host']) {
        const matches = recipes.filter(recipe => recipe.metricIds.includes(id));
        assert.equal(matches.length, 1);
        assert.deepEqual(matches[0].keys, [`${id}:host-a`, `${id}:host-b`]);
        assert.deepEqual(matches[0].assetIds, ['host-a', 'host-b']);
    }
    const cpu = recipes.find(recipe => recipe.metricIds[0] === 'node-cpu');
    assert.deepEqual(comparisonLines(s, cpu.assetIds, cpu.metricIds).map(line => line.points.at(-1).value), [12, 37]);
    const single = dashboardRecipes(s, ['host-b']);
    assert.deepEqual(single.find(recipe => recipe.metricIds[0] === 'node-cpu').keys, ['node-cpu:host-b']);
    assert.deepEqual(dashboardRecipes(s, []), []);
});
test('dashboard missing metrics stay scoped to the selected infrastructure without inventing values', () => {
    const s = snapshot([metric('node-cpu', { host: 0 }), metric('memory-host', { host: null }), metric('db-probe', { db: 5 })]);
    const observed = dashboardRecipes(s, ['host', 'deleted']);
    assert.deepEqual(observed.map(recipe => recipe.metricIds[0]), ['node-cpu']);
    const missing = dashboardRecipes(s, ['host'], true);
    const memory = missing.find(recipe => recipe.metricIds[0] === 'memory-host');
    assert.ok(memory);
    assert.equal(comparisonLines(s, memory.assetIds, memory.metricIds).length, 0);
    assert.ok(!missing.some(recipe => recipe.metricIds.includes('db-probe')));
});
test('display comparison does not change asset-local incident evaluation', () => {
    const s = snapshot([metric('p99', { 'api-a': 700 }), metric('cpu', { 'api-b': 10 })]);
    const before = registeredIncidents(s);
    comparisonLines(s, ['api-a', 'api-b'], ['p99', 'cpu']);
    comparisonSnapshot(s, ['api-a', 'api-b']);
    assert.deepEqual(registeredIncidents(s), before);
    assert.ok(!before.some(i => i.ruleId === 'io'));
});
test('paused and stale history stays inspectable without being reported as current', () => {
    const s = snapshot([metric('p99', { 'api-a': 700, 'api-b': 10 })]);
    s.assets[0].enabled = false;
    s.assets[1].enabled = false;
    const paused = comparisonSnapshot(s, ['api-a', 'api-b']);
    assert.equal(paused.metrics[0].state, 'stale');
    assert.equal(paused.metrics[0].latest, null);
    assert.equal(comparisonLines(s, ['api-a', 'api-b'], ['p99']).length, 2);
    s.assets[0].enabled = true;
    s.end += 90;
    assert.equal(comparisonSnapshot(s, ['api-a', 'api-b']).metrics[0].state, 'stale');
});
test('dense mixed-unit charts split without dropping any selected line', () => {
    const lines = Array.from({ length: 37 }, (_, i) => ({ key: String(i), metricId: 'p99', name: String(i), unit: ['ms', '%', '개', 'MiB'][i % 4], points: [{ time: 10000, value: i }] }));
    const groups = chartGroups(lines);
    assert.equal(groups.flat().length, 37);
    assert.deepEqual(groups.flat().map(l => l.key).sort(), lines.map(l => l.key).sort());
    assert.ok(groups.every(g => g.length <= 8 && new Set(g.map(l => l.unit)).size <= 2));
});
test('shared time buckets preserve gaps, zeros, signed values and dotted metric IDs', () => {
    const lines = [{ key: 'p99.9:api-a', metricId: 'p99.9', name: 'tail', unit: 'ms', points: [{ time: 10003, value: 0 }, { time: 10018, value: null }, { time: 10033, value: 98 }] }, { key: 'expiry:api-b', metricId: 'cookie-expiry', name: 'expiry', unit: '분', points: [{ time: 10004, value: -15 }, { time: 10034, value: -16 }] }];
    const rows = chartRows(lines, 15);
    assert.equal(rows.length, 3);
    assert.equal(rows[0].v0, 0);
    assert.equal(rows[0].v1, -15);
    assert.equal(rows[1].v0, null);
    assert.equal(rows[1].v1, undefined);
    assert.equal(rows[2].v0, 98);
    assert.deepEqual(lines[0].points.map(p => p.time), [10003, 10018, 10033]);
});
test('asset metric lists distinguish engines and missing observations', () => {
    const s = snapshot([metric('p99', { 'api-a': 700 }), metric('db-probe', { db: null }), metric('mysql-buffer-hit', { db: 98 })]);
    const dbMetrics = metricsForAsset(s, s.assets[2]).map(m => m.id);
    assert.ok(dbMetrics.includes('mysql-buffer-hit') && dbMetrics.includes('db-probe'));
    assert.ok(!dbMetrics.includes('oracle-buffer-hit') && !dbMetrics.includes('db-buffer') && !dbMetrics.includes('p99'));
    assert.equal(observedForAsset(s, 'db-probe', 'db'), false);
    assert.equal(observedForAsset(s, 'mysql-buffer-hit', 'db'), true);
    assert.equal(comparisonLines(s, ['db'], ['db-probe']).length, 0);
});
test('an absent collection interval breaks a line instead of interpolating an outage', () => {
    const line = { key: 'p99:api-a', metricId: 'p99', name: 'latency', unit: 'ms', points: [{ time: 9000, value: 10 }, { time: 9120, value: 20 }] };
    assert.deepEqual(chartRows([line], 15), [{ time: 9000, v0: 10 }, { time: 9015, v0: null }, { time: 9120, v0: 20 }]);
});
test('saved selection excludes deleted and duplicate assets while preserving explicit empty selection', () => {
    const assets = snapshot().assets;
    assert.deepEqual(boardSelection(null, assets), assets.map(a => a.id));
    assert.deepEqual(boardSelection([], assets), []);
    assert.deepEqual(boardSelection(['api-a', 'deleted', 'api-a', 7, 'db'], assets), ['api-a', 'db']);
});
