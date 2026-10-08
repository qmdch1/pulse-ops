"""Read-only integration checks against this project's local test stack."""
import json, urllib.request, urllib.error
base='http://127.0.0.1:13000'
def read(path):
    with urllib.request.urlopen(base+path,timeout=60) as response:return json.load(response)
data=read('/api/monitoring?range=900')
assert data['connected'] and data['mode']=='test'
assert len([t for t in data['targets'] if t['job']=='application' and t['health']=='up'])==3
assert all(t['health']=='up' for t in data['targets'])
assert not [m for m in data['metrics'] if m['state']=='error']
metrics={m['id']:m for m in data['metrics']}
for name in ['requests','p50','p97','p99','memory','db-up','cache-hit','cookie-expiry']:
    assert metrics[name]['state']=='ok',name
assert metrics['p50']['latest']<=metrics['p97']['latest']<=metrics['p99']['latest']
assert metrics['requests']['latest']>0
assert metrics['disk-forecast']['state']=='missing','Forecast must require six hours of evidence'
assert metrics['p99-week']['state']=='missing','Never invent missing weekly data'
for path in ['/api/monitoring?range=1','/api/monitoring?query=up','/api/monitoring?range=900&url=http://localhost']:
    try:read(path);raise AssertionError('Invalid input accepted')
    except urllib.error.HTTPError as e:assert e.code==400
hosts=read('/api/connections')['hosts'];assert all(h['status']=='not_connected' for h in hosts)
print(json.dumps({'targets':len(data['targets']),'applicationServers':3,'metrics':len(data['metrics']),'observed':sum(m['state']=='ok' for m in data['metrics']),'sshCandidates':len(hosts),'passed':True}))
