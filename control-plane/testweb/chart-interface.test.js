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
        line.click();
        assert.equal(line.getAttribute('aria-pressed'), 'true');
        chart.querySelector('[data-detail]').click();
        assert.deepEqual(opened, {metric: 'node-cpu', ids: ['ui-a', 'ui-b']});
    } finally {grid.destroy(); root.remove()}
});
