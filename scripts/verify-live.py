"""Read-only integration checks against the registered assets in local test Compose."""
import json,urllib.request,urllib.error
base='http://127.0.0.1:13000'
def read(path):
    with urllib.request.urlopen(base+path,timeout=60) as response:return json.load(response)
data=read('/api/monitoring?range=900')
assert data['connected'] and data['mode']=='test'
assets=data['assets'];applications=[a for a in assets if a['kind']=='application']
assert len(applications)>=3
assert all(a['status']=='connected' for a in assets if a['enabled'])
assert all(not any(key in a for key in ['password','sshPassword','privateKey','passphrase']) for a in assets)
assert not any(a['kind'] in ['prometheus','grafana'] for a in assets)
for asset in applications:
    scoped=read('/api/monitoring?range=900&instance='+asset['id'])
    metrics={m['id']:m for m in scoped['metrics']}
    for name in ['requests','p50','p97','p99','memory','cache-hit','cookie-expiry']:
        assert metrics[name]['state']=='ok',(asset['name'],name,'allow 5 minutes after collector restart')
    assert metrics['p50']['latest']<=metrics['p97']['latest']<=metrics['p99']['latest']
    assert metrics['requests']['latest']>0
    assert all(s['labels']['assetId']==asset['id'] for m in metrics.values() for s in m['series'])
    assert metrics['p99-week']['state']=='missing'
for path in ['/api/monitoring?range=1','/api/monitoring?query=up','/api/monitoring?range=900&url=http://localhost']:
    try:read(path);raise AssertionError('Invalid input accepted')
    except urllib.error.HTTPError as e:assert e.code==400
hosts=read('/api/connections')['hosts'];assert all(h['status']=='not_connected' for h in hosts)
for headers in [{},{'Origin':'https://untrusted.invalid'}]:
    request=urllib.request.Request(base+'/api/control/assets',data=b'{"kind":"server"}',headers={'Content-Type':'application/json',**headers},method='POST')
    try:urllib.request.urlopen(request);raise AssertionError('Cross-origin write accepted')
    except urllib.error.HTTPError as e:assert e.code==403
print(json.dumps({'registered':len(assets),'applications':len(applications),'metrics':len(data['metrics']),'observed':sum(m['state']=='ok' for m in data['metrics']),'sshCandidates':len(hosts),'passed':True}))
