import {test, assert} from './harness.js';
import {metrics} from '/assets/lib/catalog.js';
import {axisScale, axisRangeLabel, formatAxisTick} from '/assets/lib/chart-scale.js';

const line = (metricId, unit, values, key = metricId) => ({metricId, unit, key, points: values.map((value, time) => ({time, value}))});

test('every catalog metric has an explicit positive display range containing its warning marker', () => {
    for (const metric of metrics) {
        assert.ok(Number.isFinite(metric.axisMax) && metric.axisMax > 0, metric.id);
        if (Number.isFinite(metric.warning)) assert.ok(metric.axisMax >= metric.warning, metric.id);
        const scale = axisScale([line(metric.id, metric.unit, [null])], metric.unit, 0, 0);
        assert.equal(scale.low, 0);
        assert.equal(scale.high, metric.axisMax);
    }
});

test('latency keeps a one-second baseline and expands exactly to a visible outlier', () => {
    const normal = line('p99', 'ms', [12, 35, 500]);
    const scale = axisScale([normal], 'ms', 0, 2);
    assert.equal(scale.high, 1000);
    assert.equal(axisRangeLabel(scale), '0–1 초');
    assert.equal(formatAxisTick(500, scale), '0.5');
    const spike = line('p99', 'ms', [500, 1750]);
    assert.equal(axisScale([spike], 'ms', 0, 1).high, 1750);
    assert.equal(axisScale([line('db-probe', 'ms', [2])], 'ms', 0, 0).high, 500);
    assert.equal(axisScale([line('redis-probe', 'ms', [0.5])], 'ms', 0, 0).high, 100);
});

test('utilization stays at 100 percent and preserves multi-core CPU values above 100', () => {
    for (const id of ['node-cpu', 'memory-host', 'disk', 'db-buffer'])
        assert.equal(axisScale([line(id, '%', [0.1, 8])], '%', 0, 1).high, 100);
    assert.equal(axisScale([line('process-cpu', '%', [350])], '%', 0, 0).high, 350);
    assert.equal(axisScale([line('errors', '%', [0.1])], '%', 0, 0).high, 5);
});

test('mixed charts keep independent unit ranges and use the widest matching metric baseline', () => {
    const lines = [line('p99', 'ms', [25]), line('db-probe', 'ms', [3]), line('node-cpu', '%', [4])];
    assert.equal(axisScale(lines, 'ms', 0, 0).high, 1000);
    assert.equal(axisScale(lines, '%', 0, 0).high, 100);
    assert.equal(axisScale(lines, 'ms', 0, 0, new Set(['p99'])).high, 500);
});

test('hidden or out-of-window spikes and non-finite samples cannot distort an axis', () => {
    const lines = [line('p99', 'ms', [90000, 10, null, NaN, Infinity]), line('p99', 'ms', [20000], 'hidden')];
    const before = lines[0].points.map(point => point.value);
    const scale = axisScale(lines, 'ms', 1, 4, new Set(['hidden']));
    assert.equal(scale.high, 1000);
    assert.equal(scale.low, 0);
    assert.deepEqual(lines[0].points.map(point => point.value), before);
});

test('expired and forecast values remain negative while binary and HTTP ranges stay meaningful', () => {
    const expiry = axisScale([line('cookie-expiry', '분', [-15, 20])], '분', 0, 1);
    assert.equal(expiry.low, -15);
    assert.equal(expiry.high, 60);
    assert.equal(axisScale([line('disk-forecast', 'GiB', [-10])], 'GiB', 0, 0).low, -10);
    assert.equal(axisScale([line('db-up', '0/1', [0, 1])], '0/1', 0, 1).high, 1);
    assert.equal(axisRangeLabel(axisScale([line('db-up', '0/1', [0, 1])], '0/1', 0, 1)), '0–1');
    assert.equal(axisScale([line('http-status', 'code', [200, 503])], 'code', 0, 1).high, 600);
});

test('capacity and duration axes use explicit binary and time units without changing sample values', () => {
    const memory = line('memory', 'MiB', [512]);
    assert.equal(axisRangeLabel(axisScale([memory], 'MiB', 0, 0)), '0–1 GiB');
    assert.equal(memory.points[0].value, 512);
    assert.equal(axisRangeLabel(axisScale([line('db-network-in', 'B/s', [2048])], 'B/s', 0, 0)), '0–1 MiB/s');
    assert.equal(axisRangeLabel(axisScale([line('uptime', '초', [3600])], '초', 0, 0)), '0–1 일');
});
