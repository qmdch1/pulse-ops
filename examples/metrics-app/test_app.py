"""Exercise the documented endpoint over HTTP, including failing requests."""
import math
import os
import tempfile
import threading
import time
import unittest
from urllib.error import HTTPError
from urllib.request import urlopen
from wsgiref.simple_server import WSGIRequestHandler, make_server

from prometheus_client import CollectorRegistry
from prometheus_client.parser import text_string_to_metric_families
from app import (ContainerCollector, MetricsMiddleware, application, cpu_limit_cores, cpu_usage_seconds,
                 memory_limit_bytes, memory_usage_bytes, process_rss_bytes)

MIB, HOST = 1024 * 1024, 16 * 1024 ** 3


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
        for name in ('app_cpu_limit_cores', 'app_cpu_usage_seconds_total', 'app_memory_limit_bytes'):
            self.assertGreater(values[(name, ())], 0)
        if process_rss_bytes() is not None:  # Linux, with or without a container cgroup
            self.assertGreater(values[('app_memory_usage_bytes', ())], 0)

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


class CgroupTests(unittest.TestCase):
    """Allocation metrics read from a fake /sys/fs/cgroup."""

    def cgroup(self, files):
        root = tempfile.TemporaryDirectory()
        self.addCleanup(root.cleanup)
        for name, text in files.items():
            path = os.path.join(root.name, name)
            os.makedirs(os.path.dirname(path), exist_ok=True)
            with open(path, 'w') as handle:
                handle.write(text + '\n')
        return root.name

    def test_v2_limited_container_reports_its_quota_and_memory_without_page_cache(self):
        root = self.cgroup({'cgroup.controllers': 'cpuset cpu io memory pids', 'cpu.max': '50000 100000',
                            'cpu.stat': 'usage_usec 373551624\nuser_usec 300000000', 'memory.max': '201326592',
                            'memory.current': '61337600', 'memory.stat': 'anon 40000000\ninactive_file 11354112'})
        self.assertEqual(cpu_limit_cores(root, cpus=12), 0.5)
        self.assertAlmostEqual(cpu_usage_seconds(root), 373.551624)
        self.assertEqual(memory_limit_bytes(root, HOST), 192 * MIB)
        self.assertEqual(memory_usage_bytes(root, HOST), 61337600 - 11354112)
        # A quota above the CPUs it may run on is capped by them.
        self.assertEqual(cpu_limit_cores(self.cgroup({'cpu.max': '400000 100000'}), cpus=2), 2)

    def test_v2_unlimited_container_uses_its_cpus_and_the_host_memory(self):
        root = self.cgroup({'cgroup.controllers': 'cpu memory', 'cpu.max': 'max 100000', 'cpu.stat': 'usage_usec 6000000',
                            'memory.max': 'max', 'memory.current': str(1024 * MIB), 'memory.stat': 'inactive_file 0'})
        self.assertEqual(cpu_limit_cores(root, cpus=12), 12)
        self.assertEqual(cpu_usage_seconds(root), 6)
        self.assertEqual(memory_limit_bytes(root, HOST), HOST)
        self.assertEqual(memory_usage_bytes(root, HOST), 1024 * MIB)

    def test_v1_container_reads_cfs_quota_cpuacct_and_memory_controller(self):
        root = self.cgroup({'cpu/cpu.cfs_quota_us': '200000', 'cpu/cpu.cfs_period_us': '100000',
                            'cpuacct/cpuacct.usage': '3000000000', 'memory/memory.limit_in_bytes': str(128 * MIB),
                            'memory/memory.usage_in_bytes': str(64 * MIB), 'memory/memory.stat': f'cache 0\ntotal_inactive_file {16 * MIB}'})
        self.assertEqual(cpu_limit_cores(root, cpus=4), 2)
        self.assertEqual(cpu_usage_seconds(root), 3)
        self.assertEqual(memory_limit_bytes(root, HOST), 128 * MIB)
        self.assertEqual(memory_usage_bytes(root, HOST), 48 * MIB)

    def test_no_container_cgroup_leaves_usage_to_the_process_and_allocation_to_the_host(self):
        empty = self.cgroup({})
        # The host's own root cgroup counts the whole machine, not this application.
        v2_host = self.cgroup({'cgroup.controllers': 'cpu memory', 'cpu.stat': 'usage_usec 9', 'memory.stat': 'inactive_file 9'})
        v1_host = self.cgroup({'cpu/cpu.cfs_quota_us': '-1', 'cpu/cpu.cfs_period_us': '100000', 'cpuacct/cpuacct.usage': '9',
                               'memory/memory.limit_in_bytes': '9223372036854771712', 'memory/memory.usage_in_bytes': '9'})
        for root in (empty, v2_host, v1_host):
            self.assertEqual(cpu_limit_cores(root, cpus=8), 8)
            self.assertIsNone(cpu_usage_seconds(root))
            self.assertEqual(memory_limit_bytes(root, HOST), HOST)
            self.assertIsNone(memory_usage_bytes(root, HOST))
        registry = CollectorRegistry()
        registry.register(ContainerCollector(empty))
        before = time.process_time()
        used = registry.get_sample_value('app_cpu_usage_seconds_total')
        self.assertTrue(before <= used <= time.process_time())
        self.assertGreater(registry.get_sample_value('app_cpu_limit_cores'), 0)
        self.assertGreater(registry.get_sample_value('app_memory_limit_bytes'), 0)
        rss = registry.get_sample_value('app_memory_usage_bytes')
        if process_rss_bytes() is not None:
            self.assertGreater(rss, 0)

    def test_collector_exports_the_container_values(self):
        root = self.cgroup({'cpu.max': '50000 100000', 'cpu.stat': 'usage_usec 2500000', 'memory.max': str(192 * MIB),
                            'memory.current': str(60 * MIB), 'memory.stat': f'inactive_file {12 * MIB}'})
        registry = CollectorRegistry()
        registry.register(ContainerCollector(root))
        self.assertEqual(registry.get_sample_value('app_cpu_limit_cores'), 0.5)
        self.assertEqual(registry.get_sample_value('app_cpu_usage_seconds_total'), 2.5)
        self.assertEqual(registry.get_sample_value('app_memory_limit_bytes'), 192 * MIB)
        self.assertEqual(registry.get_sample_value('app_memory_usage_bytes'), 48 * MIB)


if __name__ == '__main__':
    unittest.main()
