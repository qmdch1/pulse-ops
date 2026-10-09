"""Single-process WSGI example of the metrics consumed by Pulse Ops."""
import argparse
import threading
import time
from wsgiref.simple_server import make_server

from prometheus_client import CollectorRegistry, Counter, Gauge, Histogram, ProcessCollector, make_wsgi_app


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
