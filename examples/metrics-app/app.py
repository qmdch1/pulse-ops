"""Single-process WSGI example of the metrics consumed by Pulse Ops."""
import argparse
import os
import threading
import time
from wsgiref.simple_server import make_server

from prometheus_client import CollectorRegistry, Counter, Gauge, Histogram, ProcessCollector, make_wsgi_app
from prometheus_client.core import CounterMetricFamily, GaugeMetricFamily

CGROUP = '/sys/fs/cgroup'


def _read(root, *names):
    """The first readable file among names below root, stripped, or None."""
    for name in names:
        try:
            with open(os.path.join(root, name)) as handle:
                return handle.read().strip()
        except OSError:
            continue
    return None


def _number(text):
    try:
        return float(text)
    except (TypeError, ValueError):  # a missing file, or "max" for no limit
        return None


def _stat(text, key):
    """One value of a "key value" per line file such as cpu.stat or memory.stat."""
    for line in (text or '').splitlines():
        name, _, value = line.partition(' ')
        if name == key:
            return _number(value)
    return None


def usable_cpus():
    try:
        return len(os.sched_getaffinity(0))
    except AttributeError:  # macOS
        return os.cpu_count() or 1


def host_memory_bytes(meminfo='/proc/meminfo'):
    try:
        with open(meminfo) as handle:
            for line in handle:
                if line.startswith('MemTotal:'):
                    return float(line.split()[1]) * 1024
    except OSError:
        pass
    try:
        return float(os.sysconf('SC_PAGE_SIZE') * os.sysconf('SC_PHYS_PAGES'))
    except (AttributeError, ValueError, OSError):
        return None


def cpu_limit_cores(root=CGROUP, cpus=None):
    """CPU cores allocated: the CPU quota (v2 cpu.max, v1 CFS quota), capped by the CPUs it may run on."""
    cpus = cpus or usable_cpus()
    quota = period = None
    cpu_max = _read(root, 'cpu.max')
    if cpu_max is not None:
        parts = cpu_max.split()
        if len(parts) == 2 and parts[0] != 'max':
            quota, period = _number(parts[0]), _number(parts[1])
    else:
        quota = _number(_read(root, 'cpu/cpu.cfs_quota_us', 'cpu,cpuacct/cpu.cfs_quota_us'))
        period = _number(_read(root, 'cpu/cpu.cfs_period_us', 'cpu,cpuacct/cpu.cfs_period_us'))
    if quota and quota > 0 and period and period > 0:
        return min(quota / period, float(cpus))
    return float(cpus)


def cpu_usage_seconds(root=CGROUP):
    """CPU time of every process in the container, or None outside a container cgroup.

    The host's root cgroup also has cpu.stat (v2) and cpuacct.usage (v1), but they
    count the whole machine. Only a v2 cgroup with cpu.max (never the root) or a v1
    cgroup with a CPU quota is taken as the container's own.
    """
    if _read(root, 'cpu.max') is not None:
        usec = _stat(_read(root, 'cpu.stat'), 'usage_usec')
        return None if usec is None else usec / 1e6
    quota = _number(_read(root, 'cpu/cpu.cfs_quota_us', 'cpu,cpuacct/cpu.cfs_quota_us'))
    usage = _number(_read(root, 'cpuacct/cpuacct.usage', 'cpu,cpuacct/cpuacct.usage'))
    return usage / 1e9 if quota and quota > 0 and usage is not None else None


def memory_limit_bytes(root=CGROUP, host_memory=None):
    """The memory limit when one is set (v2 memory.max, v1 limit_in_bytes), otherwise the host's RAM."""
    host_memory = host_memory or host_memory_bytes()
    limit = _number(_read(root, 'memory.max', 'memory/memory.limit_in_bytes'))
    if limit and limit > 0 and (not host_memory or limit < host_memory):
        return limit
    return host_memory


def memory_usage_bytes(root=CGROUP, host_memory=None):
    """Container memory like docker stats (usage minus inactive page cache), or None outside a container cgroup."""
    current = _number(_read(root, 'memory.current'))  # v2, never present on the root cgroup
    if current is not None:
        return max(0.0, current - (_stat(_read(root, 'memory.stat'), 'inactive_file') or 0))
    host_memory = host_memory or host_memory_bytes()
    limit = _number(_read(root, 'memory/memory.limit_in_bytes'))
    usage = _number(_read(root, 'memory/memory.usage_in_bytes'))
    if usage is None or not limit or (host_memory and limit >= host_memory):
        return None  # v1 without a limit cannot be told apart from the whole host
    return max(0.0, usage - (_stat(_read(root, 'memory/memory.stat'), 'total_inactive_file') or 0))


def process_rss_bytes():
    try:
        with open('/proc/self/statm') as handle:
            return float(handle.read().split()[1]) * os.sysconf('SC_PAGE_SIZE')
    except (OSError, IndexError, ValueError):
        return None


class ContainerCollector:
    """Optional allocation metrics, read on every scrape so the CPU counter matches the collector's clock.

    Outside a container the usage falls back to this process and the allocation to the whole host.
    """

    def __init__(self, root=CGROUP):
        self.root = root

    def collect(self):
        host_memory = host_memory_bytes()
        yield GaugeMetricFamily('app_cpu_limit_cores', 'CPU cores allocated to the container', value=cpu_limit_cores(self.root))
        seconds = cpu_usage_seconds(self.root)
        yield CounterMetricFamily('app_cpu_usage_seconds', 'CPU time used by the whole container',
                                  value=time.process_time() if seconds is None else seconds)
        limit, used = memory_limit_bytes(self.root, host_memory), memory_usage_bytes(self.root, host_memory)
        used = process_rss_bytes() if used is None else used
        if limit:
            yield GaugeMetricFamily('app_memory_limit_bytes', 'Memory allocated to the container', value=limit)
            if used is not None:
                yield GaugeMetricFamily('app_memory_usage_bytes', 'Container memory without inactive page cache', value=used)


class MetricsMiddleware:
    def __init__(self, application):
        self.application = application
        self.registry = CollectorRegistry()
        self.requests = Counter('http_requests_total', 'Completed business HTTP requests', ['status'], registry=self.registry)
        self.duration = Histogram('http_request_duration_seconds', 'Business HTTP response duration in seconds',
                                  buckets=(.005, .01, .025, .05, .1, .2, .3, .5, 1, 2, 5, 10), registry=self.registry)
        self.active = Gauge('app_active_requests', 'Business HTTP requests in flight', registry=self.registry)
        self.cpu = Gauge('app_process_cpu_percent', 'Process CPU percentage; one busy core is 100 percent', registry=self.registry)
        ProcessCollector(registry=self.registry)  # process_resident_memory_bytes on Linux
        self.registry.register(ContainerCollector())  # optional: CPU and RAM against the container's allocation
        self.metrics = make_wsgi_app(self.registry)
        self.cpu_lock = threading.Lock()
        self.last_wall, self.last_cpu = time.monotonic(), time.process_time()
        for status in ('200', '400', '401', '403', '404', '409', '429', '500', '502', '503', '504'):
            self.requests.labels(status).inc(0)

    def __call__(self, environ, start_response):
        path = environ.get('PATH_INFO', '/')
        if path == '/metrics':
            with self.cpu_lock:
                wall, cpu = time.monotonic(), time.process_time()
                if wall > self.last_wall:
                    self.cpu.set(100 * (cpu - self.last_cpu) / (wall - self.last_wall))
                self.last_wall, self.last_cpu = wall, cpu
            return self.metrics(environ, start_response)
        if path == '/health':
            start_response('200 OK', [('Content-Type', 'text/plain')])
            return [b'ok\n']

        began = time.monotonic()
        status = '500'
        self.active.inc()

        def capture_status(response_status, headers, exc_info=None):
            nonlocal status
            status = response_status.split(' ', 1)[0]
            return start_response(response_status, headers, exc_info)

        try:
            response = self.application(environ, capture_status)
        except Exception:
            capture_status('500 Internal Server Error', [('Content-Type', 'text/plain')])
            response = [b'internal server error\n']

        def completed():
            try:
                yield from response
            finally:
                try:
                    if hasattr(response, 'close'):
                        response.close()
                finally:
                    self.requests.labels(status).inc()
                    self.duration.observe(time.monotonic() - began)
                    self.active.dec()

        return completed()


def application(environ, start_response):
    path = environ.get('PATH_INFO', '/')
    if path == '/api/items':
        status, payload = '200 OK', b'{"items":[]}\n'
    elif path == '/api/fail':
        raise RuntimeError('Demonstrate an application failure')
    else:
        status, payload = '404 Not Found', b'{"error":"not found"}\n'
    start_response(status, [('Content-Type', 'application/json')])
    return [payload]


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--host', default='127.0.0.1')
    parser.add_argument('--port', type=int, default=18080)
    args = parser.parse_args()
    with make_server(args.host, args.port, MetricsMiddleware(application)) as server:
        print(f'Example: http://{args.host}:{args.port}/metrics', flush=True)
        server.serve_forever()
