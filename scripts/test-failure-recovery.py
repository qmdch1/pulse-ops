"""Explicit, bounded fault drill against pulse-ops-test only; always restores Redis."""
import json,pathlib,subprocess,time,urllib.request
root=pathlib.Path(__file__).resolve().parent.parent
compose=['docker','compose','-f',str(root/'compose.test.yml')]
def read():
    with urllib.request.urlopen('http://127.0.0.1:13000/api/monitoring?range=900',timeout=45) as r:return json.load(r)
before=read();assert before['mode']=='test' and before['connected']
record={'startedAt':time.time(),'beforeRequests':next(m['latest'] for m in before['metrics'] if m['id']=='requests')}
try:
    subprocess.run(compose+['stop','redis'],check=True,cwd=root,stdout=subprocess.DEVNULL)
    deadline=time.monotonic()+155
    while time.monotonic()<deadline:time.sleep(5)
    data=read();record['during']={m['id']:m['latest'] for m in data['metrics'] if m['id'] in ['errors','p99','redis-hit','requests']};record['alerts']=[a['name'] for a in data['alerts']]
    assert record['during']['errors']>2,'Failure must be observable as real 5xx'
    assert 'HighServerErrorRatio' in record['alerts'],'Prometheus sustained error rule must activate'
finally:
    subprocess.run(compose+['start','redis'],check=True,cwd=root,stdout=subprocess.DEVNULL)
    record['redisRestored']=True
    out=root/'.local/failure-drill.json';out.write_text(json.dumps(record,indent=2),encoding='utf-8')
print(json.dumps(record))
