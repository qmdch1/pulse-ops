import {metricById} from './catalog.js';

// Display ranges are soft limits. Keep raw values, thresholds and gaps intact.
export function axisScale(lines, unit, start, end, hidden = new Set()) {
    const matching = lines.filter(line => line.unit === unit);
    const visible = matching.filter(line => !hidden.has(line.key));
    const defaults = visible.length ? visible : matching;
    let low = 0, high = 0;
    // Usage measured against a capacity tops out at the largest capacity shown.
    for (const line of defaults) {
        const metric = metricById.get(line.metricId);
        high = Math.max(high, line.capacity || metric?.axisMax || 1);
    }
    for (const line of visible)
        for (const point of line.points)
            if (point.time >= start && point.time <= end && Number.isFinite(point.value)) {
                low = Math.min(low, point.value);
                high = Math.max(high, point.value);
            }
    if (high <= low) high = low + 1;
    return {low, high, ...axisUnit(unit, Math.max(Math.abs(low), Math.abs(high)))};
}

// Small multiples of one family use the widest member range so equal heights
// mean equal values across the row.
export function sharedScale(scales, unit) {
    if (!scales.length) return null;
    const low = Math.min(...scales.map(scale => scale.low)), high = Math.max(...scales.map(scale => scale.high));
    return {low, high, ...axisUnit(unit, Math.max(Math.abs(low), Math.abs(high)))};
}

function axisUnit(unit, extent) {
    const families = {
        ms: [[86400000, '일'], [3600000, '시간'], [60000, '분'], [1000, '초']],
        '초': [[86400, '일'], [3600, '시간'], [60, '분']],
        '분': [[1440, '일'], [60, '시간']],
        '시간': [[24, '일']],
        MiB: [[1048576, 'TiB'], [1024, 'GiB']],
        GiB: [[1024, 'TiB']],
        'B/s': [[1073741824, 'GiB/s'], [1048576, 'MiB/s'], [1024, 'KiB/s']],
        'MiB/s': [[1024, 'GiB/s']],
    };
    for (const [divisor, displayUnit] of families[unit] || [])
        if (extent >= divisor) return {divisor, displayUnit};
    return {divisor: 1, displayUnit: unit === '0/1' ? '' : unit};
}

export function formatAxisTick(value, scale) {
    return new Intl.NumberFormat('en-US', {maximumFractionDigits: 2, notation: Math.abs(value / scale.divisor) >= 10000 ? 'compact' : 'standard'}).format(value / scale.divisor);
}

export function axisRangeLabel(scale) {
    return `${formatAxisTick(scale.low, scale)}–${formatAxisTick(scale.high, scale)}${scale.displayUnit ? ` ${scale.displayUnit}` : ''}`;
}
