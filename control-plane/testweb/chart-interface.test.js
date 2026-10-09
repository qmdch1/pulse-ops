import {test, assert} from './harness.js';
import {ChartGrid} from '/assets/charts.js';
import {dashboardRecipes} from '/assets/lib/board.js';

test('compact charts retain values, legend visibility and opt-in settings without repeated metric names', async () => {
    const snapshot = {
        start: 100, end: 115, step: 15,
        assets: [{id: 'ui-a', name: 'Server A', kind: 'server', enabled: true}, {id: 'ui-b', name: 'Server B', kind: 'server', enabled: true}],
        metrics: [{id: 'node-cpu', series: ['ui-a', 'ui-b'].map((assetId, index) => ({labels: {assetId, name: `Server ${index ? 'B' : 'A'}`}, points: [{time: 115, value: index ? 37 : 12}]}))}],
    };
    const root = document.createElement('div');
    document.querySelector('main').append(root);
    let opened;
    const grid = new ChartGrid(root, dashboardRecipes(snapshot, ['ui-a', 'ui-b']), snapshot, (metric, ids) => opened = {metric: metric.id, ids});
    try {
        await new Promise(resolve => requestAnimationFrame(resolve));
        const chart = root.querySelector('article');
        assert.ok(chart);
        assert.equal(chart.querySelector('header p'), null);
        assert.deepEqual([...chart.querySelectorAll('.board-legend button>span')].map(node => node.textContent), ['Server A', 'Server B']);
        assert.deepEqual([...chart.querySelectorAll('.board-legend strong')].map(node => node.textContent.trim()), ['12 %', '37 %']);
        const options = chart.querySelector('.chart-options'), toggle = chart.querySelector('[data-options]');
        assert.ok(options.hidden);
        toggle.click();
        assert.ok(!options.hidden);
        assert.equal(toggle.getAttribute('aria-expanded'), 'true');
        assert.ok(chart.querySelector('[data-split]') && chart.querySelector('.chart-refresh input'));
        toggle.click();
        assert.ok(options.hidden);
        const line = chart.querySelector('.board-legend button');
        line.click();
        assert.equal(line.getAttribute('aria-pressed'), 'false');
        line.focus();
        const updated = structuredClone(snapshot);
        updated.metrics[0].series[0].points[0].value = 21;
        grid.update(updated, true);
        assert.equal(chart.querySelector('.board-legend button'), line);
        assert.equal(document.activeElement, line);
        assert.equal(line.getAttribute('aria-pressed'), 'false');
        assert.equal(line.querySelector('strong').textContent.trim(), '21 %');
        line.click();
        assert.equal(line.getAttribute('aria-pressed'), 'true');
        chart.querySelector('[data-detail]').click();
        assert.deepEqual(opened, {metric: 'node-cpu', ids: ['ui-a', 'ui-b']});
    } finally {grid.destroy(); root.remove()}
});

test('selecting a merge target combines immediately and the header undo restores every series', async () => {
    const snapshot = {
        start: 100, end: 115, step: 15,
        assets: [{id: 'merge-ui-a', name: 'Server A', kind: 'server', enabled: true}],
        metrics: ['node-cpu', 'memory-host'].map(id => ({id, series: [{labels: {assetId: 'merge-ui-a', name: 'Server A'}, points: [{time: 115, value: id === 'node-cpu' ? 12 : 37}]}]})),
    };
    const root = document.createElement('div');
    document.querySelector('main').append(root);
    const grid = new ChartGrid(root, dashboardRecipes(snapshot, ['merge-ui-a']), snapshot, () => {});
    try {
        await new Promise(resolve => requestAnimationFrame(resolve));
        const source = root.querySelector('article');
        source.querySelector('.chart-grip').click();
        const picker = source.querySelector('.chart-merge-picker');
        assert.ok(!picker.hidden);
        assert.equal(picker.querySelector('select'), null);
        picker.querySelector('.chart-target').click();
        await new Promise(resolve => requestAnimationFrame(resolve));
        assert.equal(root.querySelectorAll('article').length, 1);
        const combined = root.querySelector('article');
        assert.deepEqual([...combined.querySelectorAll('.board-legend strong')].map(node => node.textContent.trim()), ['37 %', '12 %']);
        const undo = combined.querySelector('[data-undo]');
        assert.ok(!undo.hidden);
        assert.equal(document.activeElement, undo);
        undo.click();
        await new Promise(resolve => requestAnimationFrame(resolve));
        assert.equal(root.querySelectorAll('article').length, 2);
        assert.deepEqual([...root.querySelectorAll('.board-legend strong')].map(node => node.textContent.trim()), ['12 %', '37 %']);
        assert.ok([...root.querySelectorAll('[data-undo]')].every(button => button.hidden));
    } finally {grid.destroy(); root.remove()}
});
