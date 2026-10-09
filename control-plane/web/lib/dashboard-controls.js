import {esc, icon} from '../ui.js';

import {assetSearchText} from '../operations.js';
const matches = (asset, query) => assetSearchText(asset).includes(query.toLowerCase());
export function infrastructurePicker(assets, selected, query = '') {
    query = assets.length > 12 ? query : '';
    const visible = assets.filter(asset => matches(asset, query));
    const all = visible.length > 0 && visible.every(asset => selected.includes(asset.id));
    return `<div class="quick-scopes" role="group" aria-label="인프라 선택">
        ${assets.length > 12 ? `<label class="search-field">${icon('search')}<input id="asset-search" aria-label="인프라 검색" placeholder="인프라 검색" value="${esc(query)}"></label>` : ''}
        <div class="scope-buttons"><button id="scope-all" class="scope-chip scope-all" data-scope-all aria-pressed="${all}" aria-label="${all ? '전체 선택 해제' : '전체 선택'}" ${visible.length ? '' : 'disabled'}>전체</button>
        ${assets.map(asset => `<button id="scope-${esc(asset.id)}" class="scope-chip" data-scope="${esc(asset.id)}" data-search="${esc(assetSearchText(asset))}" aria-pressed="${selected.includes(asset.id)}" title="${esc(asset.name)}" ${matches(asset, query) ? '' : 'hidden'}><i class="${esc(asset.status)}" aria-hidden="true"></i><span>${esc(asset.name)}</span></button>`).join('')}</div></div>`;
}
export function bindInfrastructurePicker(root, assets, read, change, searchChanged = () => {}) {
    const commit = (ids, focusId) => {
        change(ids);
        root.ownerDocument.getElementById(focusId)?.focus({preventScroll: true});
    };
    for (const button of root.querySelectorAll('[data-scope]')) button.onclick = () => {
        const ids = read(), id = button.dataset.scope;
        commit(ids.includes(id) ? ids.filter(item => item !== id) : [...ids, id], button.id);
    };
    const all = root.querySelector('[data-scope-all]'), input = root.querySelector('#asset-search');
    const update = () => {
        const query = input?.value || '';
        const candidates = assets.filter(asset => matches(asset, query));
        for (const button of root.querySelectorAll('[data-scope]')) button.hidden = !button.dataset.search.includes(query.toLowerCase());
        const selected = candidates.length > 0 && candidates.every(asset => read().includes(asset.id));
        all.disabled = !candidates.length;
        all.setAttribute('aria-pressed', String(selected));
        all.setAttribute('aria-label', selected ? '전체 선택 해제' : '전체 선택');
        searchChanged(query);
    };
    if (input) input.oninput = update;
    all.onclick = () => {
        const candidates = assets.filter(asset => matches(asset, input?.value || '')).map(asset => asset.id), ids = read();
        commit(candidates.every(id => ids.includes(id)) ? ids.filter(id => !candidates.includes(id)) : [...new Set([...ids, ...candidates])], all.id);
    };
}
