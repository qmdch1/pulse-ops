// An open page keeps running the code it loaded. When the server's UI build
// changes, reload — but wait while a dialog, chart drag or input is in use.
export function buildAction(known, current, busy) {
    if (!current || current === known) return 'none';
    if (!known) return 'remember';
    return busy ? 'wait' : 'reload';
}
