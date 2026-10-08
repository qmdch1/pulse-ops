"""Read SSH config metadata only. Never read keys, execute Match/ProxyCommand, or connect."""
import argparse, fnmatch, glob, json, os, pathlib, re, shlex, sys
ALLOWED={'hostname','user','port','proxyjump'}
def read_config(path,seen=None):
    seen=seen or set();path=pathlib.Path(path).expanduser().resolve()
    if str(path) in seen or len(seen)>40 or not path.is_file(): return []
    seen.add(str(path));result=[]
    for line in path.read_text(encoding='utf-8',errors='replace').splitlines():
        try: tokens=shlex.split(line,comments=True,posix=True)
        except ValueError: continue
        if not tokens: continue
        if '=' in tokens[0]: tokens=tokens[0].split('=',1)+tokens[1:]
        key=tokens[0].lower()
        if key=='include':
            for pattern in tokens[1:]:
                expanded=os.path.expanduser(pattern)
                if not os.path.isabs(expanded): expanded=str(path.parent/expanded)
                for include in sorted(glob.glob(expanded))[:40]:result.extend(read_config(include,seen))
        elif key in ALLOWED|{'host','match'}: result.append((key,tokens[1:]))
    return result
def discover(path,platform):
    entries=read_config(path);aliases=[]
    for key,args in entries:
        if key=='host':aliases.extend(a for a in args if not re.search(r'[*!?%]',a) and re.fullmatch(r'[A-Za-z0-9_.-]+',a))
    results=[]
    for alias in dict.fromkeys(aliases):
        active=True;values={}
        for key,args in entries:
            if key=='host': active=any(fnmatch.fnmatchcase(alias,p) for p in args if not p.startswith('!')) and not any(fnmatch.fnmatchcase(alias,p[1:]) for p in args if p.startswith('!'))
            elif key=='match': active=False
            elif active and key in ALLOWED and args and key not in values:values[key]=' '.join(args)[:200]
        results.append({'alias':alias,'hostname':values.get('hostname',alias),'user':values.get('user','기본 사용자'),'port':values.get('port','22'),'proxyJump':values.get('proxyjump'),'platform':platform,'status':'not_connected','requiresReview':True})
    return results
def main():
    p=argparse.ArgumentParser();p.add_argument('--config',action='append',default=[]);p.add_argument('--output',default='.local/ssh/hosts.json');args=p.parse_args()
    defaults=[(pathlib.Path.home()/'.ssh/config','Windows' if os.name=='nt' else 'macOS' if sys.platform=='darwin' else 'Linux')]
    for extra in args.config:defaults.append((pathlib.Path(extra),'Imported'))
    hosts=[]
    for path,platform in defaults:hosts.extend(discover(path,platform))
    output=pathlib.Path(args.output);output.parent.mkdir(parents=True,exist_ok=True);output.write_text(json.dumps({'hosts':hosts,'notice':'설정 파일에서 발견한 연결 후보입니다. 도달 가능 여부는 검사하지 않았으며 자동 접속하지 않습니다.'},ensure_ascii=False,indent=2),encoding='utf-8')
    print(json.dumps({'hosts':len(hosts),'output':str(output)},ensure_ascii=False))
if __name__=='__main__':main()
