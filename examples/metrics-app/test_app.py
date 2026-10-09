"""Exercise the documented endpoint over HTTP, including failing requests."""
import math
import threading
import unittest
from urllib.error import HTTPError
from urllib.request import urlopen
from wsgiref.simple_server import WSGIRequestHandler, make_server

from prometheus_client.parser import text_string_to_metric_families
from app import MetricsMiddleware, application


class QuietHandler(WSGIRequestHandler):
    def log_message(self, *args):
        pass


def samples(text):
    return {(sample.name, tuple(sorted(sample.labels.items()))): sample.value
            for family in text_string_to_metric_families(text) for sample in family.samples}


class EndpointTests(unittest.TestCase):
    def setUp(self):
        self.middleware = MetricsMiddleware(application)
        self.server = make_server('127.0.0.1', 0, self.middleware, handler_class=QuietHandler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.base = f'http://127.0.0.1:{self.server.server_port}'

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()

    def request(self, path):
        try:
            response = urlopen(self.base + path, timeout=5)
        except HTTPError as error:
            response = error
        with response:
            return response.status, response.headers, response.read().decode()

    def test_real_responses_are_counted_and_scrapes_and_health_are_excluded(self):
        self.assertEqual(self.request('/api/items')[0], 200)
        self.assertEqual(self.request('/api/fail')[0], 500)
        self.assertEqual(self.request('/unknown')[0], 404)
        self.assertEqual(self.request('/health')[0], 200)
        status, headers, first = self.request('/metrics')
        self.assertEqual(status, 200)
        self.assertIn('text/plain', headers['Content-Type'])
        _, _, second = self.request('/metrics')
        values = samples(second)
        for code in ('200', '404', '500'):
            self.assertEqual(values[('http_requests_total', (('status', code),))], 1)
        self.assertEqual(values[('http_request_duration_seconds_count', ())], 3)
        self.assertEqual(values[('app_active_requests', ())], 0)
        self.assertGreater(values[('http_request_duration_seconds_sum', ())], 0)
        self.assertEqual(samples(first)[('http_request_duration_seconds_count', ())], 3)
        self.assertTrue(math.isfinite(values[('app_process_cpu_percent', ())]))
        self.assertGreaterEqual(values[('app_process_cpu_percent', ())], 0)

    def test_zero_errors_exist_before_any_failure_and_buckets_are_cumulative(self):
        self.request('/api/items')
        _, _, body = self.request('/metrics')
        values = samples(body)
        self.assertEqual(values[('http_requests_total', (('status', '500'),))], 0)
        buckets = sorted((float(dict(labels)['le']), value) for (name, labels), value in values.items()
                         if name == 'http_request_duration_seconds_bucket')
        self.assertEqual([value for _, value in buckets], sorted(value for _, value in buckets))
        self.assertTrue(math.isinf(buckets[-1][0]))
        self.assertEqual(buckets[-1][1], values[('http_request_duration_seconds_count', ())])

    def test_each_instance_keeps_its_own_registry(self):
        self.request('/api/fail')
        other = MetricsMiddleware(application)
        self.assertEqual(other.registry.get_sample_value('http_requests_total', {'status': '500'}), 0)
        self.assertEqual(self.middleware.registry.get_sample_value('http_requests_total', {'status': '500'}), 1)


if __name__ == '__main__':
    unittest.main()
