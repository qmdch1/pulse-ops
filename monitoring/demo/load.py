"""Bounded test-only traffic: no arbitrary target or fault injection endpoint."""
import concurrent.futures, time, urllib.request
def request(i):
    try:
        with urllib.request.urlopen('http://frontend:8080/api/'+['catalog','checkout','account'][i%3],timeout=9) as r: r.read(2048)
    except Exception: pass
with concurrent.futures.ThreadPoolExecutor(max_workers=4) as executor:
    while True:
        list(executor.map(request,range(4)))
        time.sleep(.3)
