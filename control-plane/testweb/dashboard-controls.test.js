import {test, assert} from './harness.js';
import {infrastructurePicker, bindInfrastructurePicker} from '/assets/lib/dashboard-controls.js';

test('infrastructure selection takes one click and retains keyboard focus after updating the board', () => {
    const assets = [{id: 'pick-a', name: 'A', status: 'connected'}, {id: 'pick-b', name: 'B', status: 'connected'}];
    let selected = ['pick-a'], changes = 0;
    const root = document.createElement('div');
    document.querySelector('main').append(root);
    const paint = () => {
        root.innerHTML = infrastructurePicker(assets, selected);
        bindInfrastructurePicker(root, assets, () => selected, ids => {selected = ids; changes++; paint()});
    };
    try {
        paint();
        assert.equal(root.querySelector('details'), null);
        const button = root.querySelector('[data-scope="pick-b"]');
        button.focus(); button.click();
        assert.equal(changes, 1);
        assert.deepEqual(selected, ['pick-a', 'pick-b']);
        assert.equal(document.activeElement.id, 'scope-pick-b');
        assert.equal(document.activeElement.getAttribute('aria-pressed'), 'true');
        root.querySelector('[data-scope="pick-a"]').click();
        assert.deepEqual(selected, ['pick-b']);
        root.querySelector('[data-scope-all]').click();
        assert.deepEqual(selected, ['pick-b', 'pick-a']);
        root.querySelector('[data-scope-all]').click();
        assert.deepEqual(selected, []);
    } finally {root.remove()}
});

test('filtered selection changes only matching infrastructure and preserves the rest of the selection', () => {
    const assets = Array.from({length: 13}, (_, i) => ({id: `filter-${i}`, name: `Node ${i}`, address: '', status: 'connected'}));
    let selected = ['filter-2'], query = '';
    const root = document.createElement('div');
    document.querySelector('main').append(root);
    const paint = () => {
        root.innerHTML = infrastructurePicker(assets, selected, query);
        bindInfrastructurePicker(root, assets, () => selected, ids => {selected = ids; paint()}, value => query = value);
    };
    try {
        paint();
        const input = root.querySelector('input');
        input.value = 'Node 1'; input.dispatchEvent(new Event('input'));
        assert.equal(root.querySelector('[data-scope="filter-2"]').hidden, true);
        root.querySelector('[data-scope-all]').click();
        assert.deepEqual(selected, ['filter-2', 'filter-1', 'filter-10', 'filter-11', 'filter-12']);
        assert.equal(root.querySelector('input').value, 'Node 1');
        root.querySelector('[data-scope-all]').click();
        assert.deepEqual(selected, ['filter-2']);
    } finally {root.remove()}
});
