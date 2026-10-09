const running = new WeakMap();
export function motion(node, frames, options = {}) {
    if (!node?.animate || matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    running.get(node)?.cancel();
    const animation = node.animate(frames, {duration: 240, easing: 'cubic-bezier(.22,1,.36,1)', ...options});
    running.set(node, animation);
    return animation;
}
export function enter(node) {
    return motion(node, [{opacity: .5, transform: 'translateY(8px)'}, {opacity: 1, transform: 'translateY(0)'}]);
}
export function valueChanged(node) {
    return motion(node, [{filter: 'brightness(1.8)'}, {filter: 'brightness(1)'}], {duration: 320});
}
