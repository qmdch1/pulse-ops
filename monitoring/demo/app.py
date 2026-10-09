"""Isolated test workload. Never included in the production compose file."""
import gc, json, os, random, socket, threading, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse
import psutil, redis
from psycopg_pool import ConnectionPool
from prometheus_client import REGISTRY, Counter, Gauge, Histogram, generate_latest, CONTENT_TYPE_LATEST
from prometheus_client.core import CounterMetricFamily, GaugeMetricFamily

REQUESTS = Counter('http_requests_total', 'Completed HTTP requests', ['route', 'status', 'version'])
LATENCY = Histogram('http_request_duration_seconds', 'End to end request duration', ['route', 'status', 'version'], buckets=(.005,.01,.025,.05,.1,.2,.3,.5,1,2,5,10))
DB_TIME = Histogram('db_query_duration_seconds', 'Database query duration', buckets=(.001,.005,.01,.025,.05,.1,.25,.5,1,2))
DB_WAIT = Histogram('db_pool_wait_seconds', 'Time waiting for a database connection', buckets=(.001,.005,.01,.05,.1,.5,1,2))
ACTIVE = Gauge('app_active_requests', 'Requests in flight')
LIMIT = Gauge('app_request_capacity', 'Declared request capacity'); LIMIT.set(50)
POOL = Gauge('app_db_pool_active', 'Borrowed database connections')
POOL_MAX = Gauge('app_db_pool_max', 'Pool maximum'); POOL_MAX.set(8)
PENDING = Gauge('app_db_pool_pending', 'Database pool waiters')
CACHE = Counter('app_cache_requests_total', 'Cache operations', ['result'])
COOKIE = Gauge('session_cookie_expiry_timestamp_seconds', 'Expiry metadata only; never cookie values', ['cookie_name','domain'])
COOKIE.labels('demo_session','test.local').set(time.time()+2700)
COOKIE_SECURE = Gauge('session_cookie_secure', 'Secure policy for monitored cookie', ['cookie_name']); COOKIE_SECURE.labels('demo_session').set(1)
COOKIE_HTTP = Gauge('session_cookie_httponly', 'HttpOnly policy for monitored cookie', ['cookie_name']); COOKIE_HTTP.labels('demo_session').set(1)
COOKIE_SAMESITE = Gauge('session_cookie_samesite_valid', 'SameSite policy is configured', ['cookie_name']); COOKIE_SAMESITE.labels('demo_session').set(1)
HEAP = Gauge('app_memory_after_gc_bytes', 'Resident memory sampled immediately after a collection')
GC_PAUSE = Histogram('app_gc_duration_seconds', 'Garbage collection pause', buckets=(.001,.005,.01,.05,.1,.5,1))
THREADS = Gauge('app_threads_active','Active process threads')
CPU = Gauge('app_process_cpu_percent','Process CPU utilization in percent')
DISK = Gauge('app_disk_used_ratio','Container writable mount used fraction')
DISK_FREE = Gauge('app_disk_free_bytes','Container mount free space')
DISK_TOTAL = Gauge('app_disk_total_bytes','Container mount capacity')
BUILD = Gauge('app_build_info','Application build identity',['version']); BUILD.labels('1.8.2').set(1)
DEPLOY = Gauge('app_deployment_timestamp_seconds','Process deployment timestamp',['version']); DEPLOY.labels('1.8.2').set(time.time())
QUEUE = Gauge('app_queue_depth','Work waiting for admission')
RETRIES = Counter('app_retry_attempts_total','Retried operations')
AUTH = Counter('app_auth_requests_total','Authentication results',['result'])
TIMEOUT = Gauge('app_timeout_budget_seconds','Layer timeout setting',['layer'])
for layer,val in [('gateway',10),('application',8),('database',5)]: TIMEOUT.labels(layer).set(val)
for route in ['/api/catalog','/api/checkout','/api/account']:
    for status in ['200','400','401','429','500','503']:
        REQUESTS.labels(route,status,'1.8.2').inc(0)
        LATENCY.labels(route,status,'1.8.2')
AUTH.labels('failure').inc(0)
pool = ConnectionPool(os.environ['DATABASE_URL'],min_size=1,max_size=8,timeout=5)
cache = redis.Redis.from_url(os.environ['REDIS_URL'],socket_timeout=2)
proc = psutil.Process()

# Container CPU and RAM against its own cgroup allocation, like docker stats. A usage is None outside a
# container cgroup (the host's root cgroup counts the whole machine) and then falls back to this process.
CGROUP='/sys/fs/cgroup'
def cg_read(root,*names):
    for name in names:
        try:
            with open(os.path.join(root,name)) as f: return f.read().strip()
        except OSError: pass
def cg_num(text):
    try: return float(text)
    except (TypeError,ValueError): return None
def cg_stat(text,key): return next((cg_num(v) for k,_,v in (line.partition(' ') for line in (text or '').splitlines()) if k==key),None)
def cpu_limit_cores(root=CGROUP,cpus=None):
    cpus=cpus or len(os.sched_getaffinity(0)); q,_,p=(cg_read(root,'cpu.max') or '').partition(' ')
    if not q: q,p=cg_read(root,'cpu/cpu.cfs_quota_us'),cg_read(root,'cpu/cpu.cfs_period_us')
    q,p=cg_num(q),cg_num(p)
    return min(q/p,cpus) if q and q>0 and p else float(cpus)
def cpu_usage_seconds(root=CGROUP):
    if cg_read(root,'cpu.max') is not None:
        usec=cg_stat(cg_read(root,'cpu.stat'),'usage_usec'); return None if usec is None else usec/1e6
    q,ns=cg_num(cg_read(root,'cpu/cpu.cfs_quota_us')),cg_num(cg_read(root,'cpuacct/cpuacct.usage'))
    return ns/1e9 if q and q>0 and ns is not None else None
def memory_limit_bytes(root=CGROUP,host=None):
    limit=cg_num(cg_read(root,'memory.max','memory/memory.limit_in_bytes')); return limit if limit and 0<limit<host else host
def memory_usage_bytes(root=CGROUP,host=None):
    current=cg_num(cg_read(root,'memory.current'))
    if current is not None: return max(0.0,current-(cg_stat(cg_read(root,'memory.stat'),'inactive_file') or 0))
    limit,used=cg_num(cg_read(root,'memory/memory.limit_in_bytes')),cg_num(cg_read(root,'memory/memory.usage_in_bytes'))
    return max(0.0,used-(cg_stat(cg_read(root,'memory/memory.stat'),'total_inactive_file') or 0)) if used is not None and limit and limit<host else None
class Allocation:
    """Read on every scrape so the CPU counter matches the collector's clock."""
    def collect(self):
        host=psutil.virtual_memory().total; cpu=cpu_usage_seconds(); used=memory_usage_bytes(host=host); own=proc.cpu_times()
        yield GaugeMetricFamily('app_cpu_limit_cores','CPU cores allocated to the container',value=cpu_limit_cores())
        yield CounterMetricFamily('app_cpu_usage_seconds','CPU time of the whole container',value=own.user+own.system if cpu is None else cpu)
        yield GaugeMetricFamily('app_memory_limit_bytes','Memory allocated to the container',value=memory_limit_bytes(host=host))
        yield GaugeMetricFamily('app_memory_usage_bytes','Container memory without inactive page cache',value=proc.memory_info().rss if used is None else used)
REGISTRY.register(Allocation())

def sample():
    while True:
        stats=pool.get_stats(); POOL.set(stats.get('pool_size',0)-stats.get('pool_available',0)); PENDING.set(stats.get('requests_waiting',0))
        THREADS.set(threading.active_count()); CPU.set(proc.cpu_percent()); disk=psutil.disk_usage('/tmp')
        DISK.set(disk.used/disk.total); DISK_FREE.set(disk.free); DISK_TOTAL.set(disk.total)
        t=time.monotonic(); gc.collect(); GC_PAUSE.observe(time.monotonic()-t); HEAP.set(proc.memory_info().rss)
        time.sleep(10)

class Handler(BaseHTTPRequestHandler):
    def log_message(self,*args): pass
    def do_GET(self):
        route=urlparse(self.path).path
        if route=='/metrics':
            data=generate_latest(); self.send_response(200); self.send_header('Content-Type',CONTENT_TYPE_LATEST); self.end_headers(); self.wfile.write(data); return
        if route=='/health':
            self.send_response(200); self.end_headers(); self.wfile.write(b'ok'); return
        if route not in ['/api/catalog','/api/checkout','/api/account']:
            self.send_response(404); self.end_headers(); return
        start=time.monotonic(); ACTIVE.inc(); status=200
        try:
            key='catalog:summary'; cached=cache.get(key)
            CACHE.labels('hit' if cached else 'miss').inc()
            if not cached or route=='/api/checkout':
                wait=time.monotonic()
                with pool.connection() as conn:
                    DB_WAIT.observe(time.monotonic()-wait)
                    with DB_TIME.time(): conn.execute('SELECT pg_sleep(%s)',(random.uniform(.005,.045),)).fetchone()
                cache.setex(key,30,'available')
            time.sleep(random.uniform(.005,.025))
            if route=='/api/account': AUTH.labels('success').inc()
            payload=json.dumps({'server':socket.gethostname(),'version':'1.8.2','route':route}).encode()
        except Exception:
            status=503; payload=b'{"error":"dependency unavailable"}'
        finally:
            elapsed=time.monotonic()-start; LATENCY.labels(route,str(status),'1.8.2').observe(elapsed); REQUESTS.labels(route,str(status),'1.8.2').inc(); ACTIVE.dec()
        self.send_response(status); self.send_header('Content-Type','application/json'); self.end_headers(); self.wfile.write(payload)

threading.Thread(target=sample,daemon=True).start()
ThreadingHTTPServer(('0.0.0.0',8080),Handler).serve_forever()
