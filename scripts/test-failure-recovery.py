"""Bounded Redis fault drill against pulse-ops-test only; always restores Redis."""
import json,pathlib,subprocess,time,urllib.request
root=pathlib.Path(__file__).resolve().parent.parent
compose=['docker','compose','-f',str(root/'compose.test.yml')]
base='http://127.0.0.1:13000'
def read():
    with urllib.request.urlopen(base+'/api/monitoring?range=900',timeout=45) as r:return json.load(r)
def reconnect(asset_id):
    r=urllib.request.Request(base+'/api/control/assets/'+asset_id+'/connect',data=b'',headers={'Origin':base},method='POST')
    with urllib.request.urlopen(r,timeout=45) as response:return json.load(response)
before=read();assert before['mode']=='test' and before['connected']
asset=next(a for a in before['assets'] if a['name']=='Redis · test' and a['kind']=='redis')
record={'startedAt':time.time(),'assetId':asset['id']}
try:
    subprocess.run(compose+['stop','redis'],check=True,cwd=root,stdout=subprocess.DEVNULL)
    deadline=time.monotonic()+65
    while time.monotonic()<deadline:
        data=read();state=next(a for a in data['assets'] if a['id']==asset['id'])
        if state['status']=='error':break
        time.sleep(3)
    assert state['status']=='error','registered Redis failure must be visible'
    series=next(m for m in data['metrics'] if m['id']=='redis-up')['series']
    assert next(s for s in series if s['labels']['assetId']==asset['id'])['points'][-1]['value']==0
    record['failureObserved']=True
finally:
    subprocess.run(compose+['start','redis'],check=True,cwd=root,stdout=subprocess.DEVNULL)
    record['redisRestored']=True
deadline=time.monotonic()+40
while True:
    try:restored=reconnect(asset['id']);break
    except Exception:
        if time.monotonic()>deadline:raise
        time.sleep(3)
assert restored['status']=='connected'
record['collectionRecovered']=True
(root/'.local').mkdir(exist_ok=True)
(root/'.local/failure-drill-native.json').write_text(json.dumps(record,indent=2),encoding='utf-8')
print(json.dumps(record))
