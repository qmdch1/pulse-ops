"""Render separate service-traffic and operations-observation examples.

Standard library only. Each SVG has its own 20-second SMIL loop. Values are
synthetic five-minute samples; the Cloudflare API lane is a planned integration.
"""
from pathlib import Path
from xml.sax.saxutils import escape
import runpy

ROOT = Path(__file__).resolve().parents[1]
BASE = runpy.run_path(str(ROOT / 'scripts' / 'render-architecture.py'))
Svg, THEMES, anim, shown, legs = (BASE[k] for k in ('Svg', 'THEMES', 'anim', 'shown', 'legs'))
EASE_IN, EASE_OUT, LINEAR = (BASE[k] for k in ('EASE_IN', 'EASE_OUT', 'LINEAR'))
W, T = 1440, 20.0
anim.__globals__['T'] = T

DNS_OUT, DNS_BACK = legs(.4, .65, .65)
HIT_IN, HIT_LOOKUP, HIT_FOUND, HIT_BACK = legs(DNS_BACK[1] + .25, .7, .55, .55, .7)
API_IN, API_EDGE, API_APP, CACHE_GET, CACHE_MISS, SQL_GET, SQL_ROWS, API_REPLY, EDGE_REPLY, USER_REPLY = legs(
    5.6, .7, .7, .7, .8, .7, .7, .7, .8, .7, .7)
ERR_IN, ERR_EDGE, ERR_APP, ERR_REPLY, ERR_RETURN, ERR_USER = legs(14.5, .8, .8, .8, .8, .8, .8)
TRAFFIC_PHASES = [
    (0, 5, 'amber', '① 접속 · 캐시 응답', 'DNS로 주소를 찾고 정적 파일을 캐시에서 응답합니다. 원본 서버로 전달하지 않습니다.'),
    (5, 14, 'blue', '② API · DB 처리', 'Nginx → App A → Redis MISS → DB 조회 후 200 응답이 사용자에게 돌아갑니다.'),
    (14, T, 'red', '③ 500 오류 응답', 'App B의 500 응답도 같은 경로로 돌아갑니다. 최종 코드와 처리 시간은 앱에서 집계합니다.'),
]
JOBS = [
    ('a-metrics', .7, 1.15, 1.1, 0),
    ('b-metrics', .9, 1.35, 1.25, 1),
    ('db-stats', 1.1, 1.5, 1.25, 2),
    ('redis-stats', 1.3, 1.65, 1.4, 3),
]
HOST_START = JOBS[0][1] + JOBS[0][2] + JOBS[0][3]
HOST_GET, HOST_BACK = legs(HOST_START, .85, .85)
STORE, CHART = legs(5.3, .8, .9)
CF_GET, CF_DATA = legs(11, 1.4, 1.4)
OPS_GET, OPS_READ, OPS_ROWS, OPS_BACK = legs(14.4, 1.1, .5, .5, 1.1)
OBSERVATION_PHASES = [
    (0, 10, 'green', '④ 내부 지표 수집', '15초마다 최대 4개를 직접 조회합니다. 빈 슬롯에 SSH 수집을 이어서 실행하고 SQLite에 저장합니다.'),
    (10, T, 'amber', '⑤ 통합 화면 예시', '관리 화면은 저장된 지표를 조회합니다. 주황 점선의 Cloudflare API 연결은 연동 예정입니다.'),
]


def text(s, x, y, value, size=15, color='body', weight=400, **kw):
    s.text(s.base, x, y, value, size, color, weight, **kw)


def pill(s, x, y, label, color, width, future=False):
    s.rect(s.base, x, y, width, 23, 11.5, 'block', color, 1,
           extra='stroke-dasharray="4 3"' if future else '')
    text(s, x + width / 2, y + 16, label, 11.5, color, 700, anchor='middle')


def card(s, geometry, title, subtitle, accent='blue'):
    x, y, w, _ = geometry
    s.rect(s.base, *geometry, 12)
    s.base.append(f'<path d="M{x+16},{y+15} v18" stroke="{s.c[accent]}" stroke-width="3" stroke-linecap="round"/>')
    text(s, x + 27, y + 30, title, 18, 'text', 700)
    text(s, x + 18, y + 56, subtitle, 13.5, 'muted')


def note(s, x, y, label, value, color='body', width=200):
    text(s, x, y, label, 12.5, 'muted')
    text(s, x + width, y, value, 15, color, 700, anchor='end')


def tag(s, x, y, value, color, windows, size=13):
    s.fx.append(f'<g opacity="0">{shown(*windows)}'
                f'<text class="t" x="{x}" y="{y}" font-size="{size}" font-weight="700" '
                f'fill="{s.c[color]}">{escape(value)}</text></g>')


def trip(s, path, interval, color, reverse=False, chip=None, ease=LINEAR):
    s.glow_lane(path, color, [interval], arrow=False, width=2.5)
    if path == 'cf-api':
        s.fx[-1] = s.fx[-1].replace('stroke-linecap="round"', 'stroke-linecap="round" stroke-dasharray="5 5"')
    s.packet(path, *interval, color, reverse=reverse, chip=chip, r=5.3, ease=ease)


def canvas(theme, height, title, subtitle, phases):
    c = dict(THEMES[theme])
    c['red_ink'] = c['red']
    c['amber'] = '#ef7a21' if theme == 'light' else '#ffad66'
    c['amber_ink'] = '#b94a0c' if theme == 'light' else '#ffc18d'
    s = Svg(c)
    for name in ('lane', 'violet', 'blue', 'green', 'amber', 'red'):
        s.defs.append(f'<marker id="ah-{name}" viewBox="0 0 10 10" refX="8.5" refY="5" '
                      f'markerWidth="6" markerHeight="6" orient="auto-start-reverse">'
                      f'<path d="M1,1.5 L9,5 L1,8.5 z" fill="{c[name]}"/></marker>')
    s.rect(s.base, .5, .5, W - 1, height - 1, 18, 'bg', 'edge')
    text(s, 36, 38, 'PULSE / OPS  ×  CLOUDFLARE', 13, 'amber_ink', 700, extra='letter-spacing="1.4"')
    text(s, 36, 78, title, 27, 'text', 700)
    text(s, 36, 108, subtitle, 15, 'muted')
    pill(s, 1255, 30, '구성 · 데이터 예시', 'amber', 149)
    gap = 12
    width = (W - 72 - gap * (len(phases) - 1)) / len(phases)
    for i, (start, end, color, label, _) in enumerate(phases):
        x = 36 + i * (width + gap)
        s.rect(s.base, x, 136, width, 40, 10, 'panel', 'border')
        text(s, x + 16, 161, label, 14, 'muted', 600)
        window = (max(.2, start + .15), end - .2)
        s.fx.append(f'<g opacity="0">{shown(window, fade=.2)}'
                    f'<rect x="{x}" y="136" width="{width}" height="40" rx="10" '
                    f'fill="{c[color]}" fill-opacity="{c["tint"]}" stroke="{c[color]}"/>'
                    f'<text class="t" x="{x+16}" y="161" font-size="14" font-weight="700" '
                    f'fill="{c[color+"_ink"]}">{escape(label)}</text>'
                    f'<rect x="{x+14}" y="171" width="0" height="2" rx="1" fill="{c[color]}">'
                    f'{anim("width", [0,window[0],window[1],T], [0,0,width-28,width-28], ease=LINEAR)}</rect></g>')
    return s


def lanes(s, paths):
    for name, path in paths.items():
        s.lane(name, path, arrow=name not in ('store', 'chart', 'ops'))
        if name == 'cf-api':
            s.base[-1] = s.base[-1].replace(f'stroke="{s.c["lane"]}"', f'stroke="{s.c["amber"]}"')
            s.base[-1] = s.base[-1].replace('stroke-width="1.5"', 'stroke-width="1.5" stroke-dasharray="5 5" opacity=".45"')


def finish(s, height, title, desc, phases, footer):
    y = height - 72
    s.rect(s.base, 36, y, 1368, 44, 12, 'panel', 'border')
    fallback = ' → '.join(p[3][2:] for p in phases)
    s.base.append(f'<text class="t rm" x="56" y="{y+28}" font-size="14" fill="{s.c["body"]}">{escape(fallback)}</text>')
    for start,end,color,_,caption in phases:
        window = (max(.3,start+.2),end-.3)
        s.fx.append(f'<g opacity="0">{shown(window,fade=.2)}'
                    f'<circle cx="59" cy="{y+23}" r="5" fill="{s.c[color]}"/>'
                    f'<text class="t" x="76" y="{y+28}" font-size="14" fill="{s.c["body"]}">{escape(caption)}</text></g>')
    text(s,36,height-10,footer,12.5,'body',600)
    text(s,1404,height-10,'가상값 · 20초 반복 · 동작 줄이기 지원',12,'muted',anchor='end')
    style = ('.t{font-family:Pretendard,"Pretendard Variable","Apple SD Gothic Neo","Malgun Gothic","Noto Sans KR",'
             '"Noto Sans CJK KR","Segoe UI",system-ui,sans-serif}.m{font-family:ui-monospace,Consolas,monospace}'
             'text{text-rendering:geometricPrecision}.rm{display:none}'
             '@media(prefers-reduced-motion:reduce){.fx{display:none}.rm{display:inline}}')
    return (f'<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" '
            f'viewBox="0 0 {W} {height}" width="{W}" height="{height}" role="img" aria-labelledby="title desc">'
            f'<title id="title">{escape(title)}</title><desc id="desc">{escape(desc)}</desc>'
            f'<style>{style}</style><defs>{"".join(s.defs)}</defs>{"".join(s.base)}'
            f'<g class="fx">{"".join(s.fx)}</g></svg>\n')


def build_traffic(theme):
    height = 800
    s = canvas(theme,height,'서비스 요청 흐름','접속·캐시 → API·DB → 오류 응답 · app.example.com · 가상 5분 표본',TRAFFIC_PHASES)
    browser, dns, edge, cache = (56,352,178,198),(56,254,178,72),(286,340,234,216),(302,477,202,58)
    nginx, front = (584,374,190,150),(584,558,190,94)
    app_a, app_b, db, redis = (830,304,250,140),(830,498,250,140),(1140,304,244,140),(1140,498,244,140)
    for x,width,heading,subtitle in ((36,504,'인터넷 · Cloudflare','DNS 프록시 ON'),(564,840,'원본 서버 · 내부망','DB · Redis는 내부 연결')):
        s.rect(s.base,x,204,width,506,16,'panel','border')
        text(s,x+20,232,heading,15,'text',700)
        text(s,x+width-20,232,subtitle,12,'muted',anchor='end')
    lanes(s,{
        'dns':'M145,352 V327','edge':'M234,430 H286','cache':'M403,451 V477','origin':'M520,430 H584',
        'a':'M774,410 H797 Q810,410 810,397 V355 Q810,342 823,342 H830',
        'b':'M774,450 H797 Q810,450 810,463 V543 Q810,556 823,556 H830',
        'sql':'M1080,367 H1140','redis':'M1080,414 H1091 Q1104,414 1104,427 V554 Q1104,567 1117,567 H1140',
        'front':'M679,524 V558',
    })
    text(s,536,412,'HTTPS',10.5,'muted',anchor='middle')
    s.rect(s.base,*dns,12)
    text(s,74,282,'Cloudflare DNS',16,'text',700)
    text(s,74,309,'주소 조회',13,'muted')
    card(s,browser,'브라우저','app.example.com','violet')
    s.rect(s.base,74,427,142,99,7,'panel','border')
    text(s,85,449,'서비스 화면',13,'text',600)
    for yy,ww in ((463,108),(476,75),(489,92)):
        s.rect(s.base,85,yy,ww,5,2.5,'lane','lane',0)
    text(s,85,514,'정적 파일 + 업무 API',10.5,'muted')
    card(s,edge,'Cloudflare','프록시 · CDN · HTTPS','amber')
    text(s,304,422,'app 요청 10,000건 / 5분',15,'amber_ink',700)
    text(s,304,446,'원본 전달 4,000건',13,'body')
    s.rect(s.base,*cache,8,'block','amber')
    text(s,316,499,'정적 파일 캐시',13.5,'text',700)
    text(s,316,522,'HIT 6,000건 · 60%',14,'amber_ink',600)
    text(s,56,593,'주황 구름: 웹 요청이 통과',13,'amber_ink',600)
    text(s,56,618,'DNS only는 주소 조회만',12,'muted')
    text(s,56,664,'CDN HIT는 원본 요청에 포함하지 않습니다.',12,'muted')
    card(s,nginx,'Nginx','도메인 · 경로로 전달','blue')
    text(s,602,461,'/api → 백엔드',13,'body',mono=True)
    text(s,602,486,'/ → 프런트',13,'body')
    text(s,602,509,'원본 HTTPS · Full (strict)',10.8,'muted')
    card(s,front,'프런트 화면','HTML · CSS · JS','violet')
    text(s,602,631,'API는 캐시 우회',11.5,'muted')
    for geometry,title,sub,count,errors,rate,latency,color in (
        (app_a,'App A','backend-1','2,500건','10건','0.40%','240 ms','blue'),
        (app_b,'App B','backend-2','1,500건','5건','0.33%','180 ms','violet'),
    ):
        card(s,geometry,title,sub,color)
        x,y,_,_ = geometry
        note(s,x+18,y+82,'5분 요청 / 5xx',f'{count} / {errors}',width=214)
        note(s,x+18,y+106,'오류율 / P99',f'{rate} / {latency}',color,214)
        text(s,x+18,y+129,'완료한 응답 코드·시간 집계',11.5,'muted')
    card(s,db,'PostgreSQL','업무 데이터 조회','green')
    note(s,1158,386,'연결 수','24','green',208)
    text(s,1158,419,'앱에서 SQL 실행 → 결과 반환',12,'muted')
    card(s,redis,'Redis','업무 데이터 캐시','green')
    note(s,1158,580,'캐시 적중률','96%','green',208)
    note(s,1158,605,'사용 메모리','128 MiB',width=208)
    text(s,1158,627,'이 요청은 MISS → DB 조회',11.5,'muted')
    trip(s,'dns',DNS_OUT,'violet',ease=EASE_IN)
    trip(s,'dns',DNS_BACK,'violet',reverse=True,ease=EASE_OUT)
    s.glow_block(dns,'violet',[(DNS_OUT[0],DNS_BACK[1]+.25)])
    tag(s,69,343,'응답: Cloudflare 주소','violet',[(DNS_BACK[1],DNS_BACK[1]+.4)],11.5)
    for path,interval,reverse,chip,ease in (
        ('edge',HIT_IN,False,None,EASE_IN),('cache',HIT_LOOKUP,False,None,LINEAR),
        ('cache',HIT_FOUND,True,'HIT',LINEAR),('edge',HIT_BACK,True,'200',EASE_OUT),
    ):
        trip(s,path,interval,'amber',reverse,chip,ease)
    s.glow_block(edge,'amber',[(HIT_IN[1]-.1,HIT_BACK[0]+.2)])
    s.glow_block(cache,'amber',[(HIT_LOOKUP[0],HIT_FOUND[1]+.3)])
    tag(s,304,573,'/assets/app.js · 원본 요청 없음','amber',[(HIT_IN[0],4.7)],12)
    for path,interval,reverse,chip in (
        ('edge',API_IN,False,None),('origin',API_EDGE,False,None),('a',API_APP,False,None),
        ('redis',CACHE_GET,False,'GET'),('redis',CACHE_MISS,True,'MISS'),('sql',SQL_GET,False,None),
        ('sql',SQL_ROWS,True,'rows'),('a',API_REPLY,True,'200'),('origin',EDGE_REPLY,True,'200'),('edge',USER_REPLY,True,'200'),
    ):
        trip(s,path,interval,'blue',reverse,chip)
    for geometry,window in ((nginx,(API_EDGE[0],EDGE_REPLY[1])),(app_a,(API_APP[0],API_REPLY[1])),
                            (redis,(CACHE_GET[0],CACHE_MISS[1])),(db,(SQL_GET[0],SQL_ROWS[1]))):
        s.glow_block(geometry,'blue',[window])
    tag(s,588,692,'GET /api/orders → 200 · 180 ms 예시','blue',[(API_IN[0],13.6)])
    for path,interval,reverse,chip in (
        ('edge',ERR_IN,False,None),('origin',ERR_EDGE,False,None),('b',ERR_APP,False,None),
        ('b',ERR_REPLY,True,'500'),('origin',ERR_RETURN,True,'500'),('edge',ERR_USER,True,'500'),
    ):
        trip(s,path,interval,'red' if reverse else 'blue',reverse,chip)
    s.glow_block(app_b,'red',[(ERR_APP[1]-.1,ERR_USER[1]+.3)])
    tag(s,848,664,'App B: 최종 500 · 요청 수 / 5xx / 시간 집계','red',[(16.8,19.6)],12.5)
    tag(s,588,692,'500은 앱에서 발생 · 앞단이 전달','red',[(ERR_IN[0],19.7)])
    return finish(s,height,'서비스 요청 흐름: 접속·캐시, API·DB, 오류 응답',
        '20초 반복, 3개 장면. DNS 조회 후 정적 파일은 Cloudflare 캐시에서 응답합니다. API는 Nginx와 App A를 거쳐 Redis MISS 뒤 DB를 조회합니다. App B의 500 응답도 사용자에게 돌아갑니다. 모든 수치는 가상의 5분 표본입니다.',
        TRAFFIC_PHASES,'app 5분: 외부 10,000 = HIT 6,000 + 원본 4,000 · 원본 4,000 = A 2,500 + B 1,500')


def build_observation(theme):
    height = 920
    s = canvas(theme,height,'운영 관측 흐름','내부 지표 수집 → 통합 화면 · ops.example.com · 가상 5분 표본',OBSERVATION_PHASES)
    engine,sqlite,chart,cf_api,edge_stats,ui = (36,468,270,134),(36,666,270,114),(354,468,470,310),(872,468,250,120),(872,630,250,148),(1162,468,242,310)
    target_boxes = {
        'a-metrics':(36,224,240,126),'b-metrics':(312,224,240,126),'db-stats':(588,224,240,126),
        'redis-stats':(864,224,240,126),'host':(1140,224,264,126),
    }
    text(s,36,208,'등록한 개별 대상 · 관리망에서 직접 조회',13,'muted',600)
    text(s,1404,442,'녹색·청색: 현재 지원     주황 점선: 연동 예정',12.5,'muted',anchor='end')
    paths = {name:f'M171,468 V402 H{x+w/2} V350' for name,(x,_,w,_) in target_boxes.items()}
    paths.update({'store':'M171,602 V666','chart':'M306,723 H354',
                  'cf-api':'M306,557 H330 V810 H850 V528 H872','ops':'M1162,704 H1142 V834 H22 V560 H36'})
    lanes(s,paths)
    for path,title,sub,label,value,color in (
        ('a-metrics','App A','backend-1 · /metrics','오류율 / P99','0.40% / 240 ms','blue'),
        ('b-metrics','App B','backend-2 · /metrics','오류율 / P99','0.33% / 180 ms','violet'),
        ('db-stats','PostgreSQL','읽기 전용 통계 조회','연결 / 응답시간','24 / 4 ms','green'),
        ('redis-stats','Redis','PING / INFO','적중률 / 메모리','96% / 128 MiB','green'),
        ('host','서버 OS · SSH','고정 조회 명령','CPU / RAM','38% / 62%','green'),
    ):
        box = target_boxes[path]
        card(s,box,title,sub,color)
        x,y,w,_ = box
        note(s,x+18,y+87,label,value,color,w-36)
        text(s,x+18,y+113,'디스크 41%' if path == 'host' else '인스턴스별 관측값',11.5,'muted')
    card(s,engine,'Pulse Ops · Go','15초 주기 · 최대 4개 동시','green')
    for i in range(4):
        xx = 55 + i*61
        s.rect(s.base,xx,552,48,25,5,'panel','lane')
        text(s,xx+24,570,str(i+1),12,'muted',600,anchor='middle')
    text(s,54,592,'슬롯이 비면 다음 대상 수집',11.5,'muted')
    card(s,sqlite,'SQLite','수집 결과 · 등록 정보 보관','green')
    text(s,54,752,'화면 조회는 수집을 호출하지 않음',12,'muted')
    card(s,chart,'백엔드 응답시간 P99','연속 5분의 Histogram 증가량','blue')
    for yy in (580,632,684):
        s.base.append(f'<path d="M374,{yy} H804" fill="none" stroke="{s.c["border"]}"/>')
    for color,path in (
        ('blue','M374,668 L428,648 L482,657 L536,609 L590,626 L644,566 L698,586 L752,573 L804,580'),
        ('violet','M374,703 L428,686 L482,692 L536,662 L590,679 L644,637 L698,650 L752,628 L804,632'),
    ):
        s.base.append(f'<path d="{path}" fill="none" stroke="{s.c[color]}" stroke-width="2" opacity=".32"/>')
        s.fx.append(f'<path d="{path}" fill="none" stroke="{s.c[color]}" stroke-width="3" pathLength="1" stroke-dasharray="1" stroke-dashoffset="1">'
                    f'{anim("stroke-dashoffset",[0,CHART[0],CHART[1]+1.2,T],[1,1,0,0],ease=LINEAR)}{shown((CHART[0],T-.4))}</path>')
    text(s,374,738,'A 240 ms',14,'blue',700)
    text(s,620,738,'B 180 ms',14,'violet',700)
    text(s,374,761,'P99는 인스턴스끼리 평균 내지 않습니다.',11.5,'muted')
    s.rect(s.base,*cf_api,12,'block','amber',extra='stroke-dasharray="5 4"')
    text(s,890,498,'Cloudflare API',16,'text',700)
    text(s,890,526,'읽기 토큰 · Zone ID',13,'muted')
    pill(s,890,540,'연동 예정','amber',96,True)
    text(s,890,576,'별도 조회 주기 · 요금제별 범위',11.5,'muted')
    card(s,edge_stats,'외부 트래픽','app 도메인 · 연동 예정','amber')
    for i,(label,value,portion,color) in enumerate((('전체','10,000건',1,'amber'),('HIT','6,000건',.6,'amber'),('원본','4,000건',.4,'blue'))):
        yy = 709+i*26
        note(s,890,yy,label,value,color,214)
        s.rect(s.base,945,yy+6,159*portion,3,1.5,color,color,0,extra='opacity=".3"')
        s.fx.append(f'<rect x="945" y="{yy+6}" width="0" height="3" rx="1.5" fill="{s.c[color]}">'
                    f'{anim("width",[0,CF_DATA[1],CF_DATA[1]+1.3,T],[0,0,159*portion,159*portion],ease=LINEAR)}{shown((CF_DATA[1],T-.4))}</rect>')
    card(s,ui,'관리 화면','ops.example.com','violet')
    for y,label,value in ((565,'App A P99','240 ms'),(604,'App B P99','180 ms'),(643,'서버 CPU','38%'),(682,'DB 연결','24')):
        note(s,1180,y,label,value,'violet',206)
        s.rect(s.base,1180,y+10,206,2,1,'border','border',0)
    text(s,1180,738,'HTTPS 앞단 → Go → 저장값',11.5,'muted')
    text(s,1180,761,'운영자 로그인 · 화면 조회',11.5,'muted')
    for path,start,outgoing,incoming,slot in JOBS:
        out,back = legs(start,outgoing,incoming)
        trip(s,path,out,'green',chip='GET' if slot == 0 else None)
        trip(s,path,back,'blue',True,'data' if slot == 0 else None)
        s.glow_block(target_boxes[path],'green',[(out[1],back[0]+.3)])
        xx = 55+slot*61
        s.fx.append(f'<rect x="{xx}" y="552" width="48" height="25" rx="5" fill="{s.c["green"]}" fill-opacity=".25" stroke="{s.c["green"]}" opacity="0">{shown((out[0],back[1]))}</rect>')
    trip(s,'host',HOST_GET,'green',chip='SSH')
    trip(s,'host',HOST_BACK,'blue',True,'data')
    s.glow_block(target_boxes['host'],'green',[(HOST_GET[0],HOST_BACK[1]+.3)])
    s.fx.append(f'<rect x="55" y="552" width="48" height="25" rx="5" fill="{s.c["green"]}" fill-opacity=".25" stroke="{s.c["green"]}" opacity="0">{shown((HOST_GET[0],HOST_BACK[1]))}</rect>')
    s.glow_block(engine,'green',[(.6,7.3)])
    trip(s,'store',STORE,'green',chip='save')
    trip(s,'chart',CHART,'blue',chip='draw')
    s.glow_block(sqlite,'green',[(STORE[0],CHART[1]+.5)])
    tag(s,1110,385,'빈 슬롯에 SSH 수집','green',[(HOST_GET[0],HOST_BACK[1]+1)],12)
    trip(s,'cf-api',CF_GET,'amber',chip='API')
    trip(s,'cf-api',CF_DATA,'amber',True,'data')
    s.glow_block(cf_api,'amber',[(CF_GET[0],CF_DATA[1]+.6)])
    s.glow_block(edge_stats,'amber',[(CF_DATA[1],T-.5)])
    trip(s,'ops',OPS_GET,'violet',chip='GET')
    trip(s,'store',OPS_READ,'violet',chip='read')
    trip(s,'store',OPS_ROWS,'violet',True,'rows')
    trip(s,'ops',OPS_BACK,'violet',True,'200')
    s.glow_block(ui,'violet',[(OPS_GET[0],OPS_BACK[1]+.5)])
    tag(s,450,825,'ops 화면 조회 → Go → SQLite → 응답','violet',[(OPS_GET[0],T-.5)],12)
    return finish(s,height,'운영 관측 흐름: 지표 수집과 통합 화면',
        '20초 반복, 2개 장면. Pulse Ops는 15초 주기로 최대 4개 대상을 동시에 조회합니다. 첫 슬롯이 비면 다섯 번째 SSH 수집을 시작하고 SQLite에 저장해 그래프를 표시합니다. 관리 화면은 Go를 통해 저장값을 읽습니다. Cloudflare API와 외부 트래픽은 연동 예정이며 주황 점선으로 표시합니다. 수치는 가상값입니다.',
        OBSERVATION_PHASES,'직접 수집: 앱 /metrics · DB · Redis · SSH | 외부 요청은 Cloudflare API 연동 설계')


def build(theme, group='traffic'):
    return {'traffic':build_traffic,'observation':build_observation}[group](theme)


if __name__ == '__main__':
    output = ROOT/'docs'/'images'
    output.mkdir(parents=True,exist_ok=True)
    for group,stem in (('traffic','cloudflare-traffic-flow'),('observation','pulse-observation-flow')):
        for theme,suffix in (('light',''),('dark','-dark')):
            (output/f'{stem}{suffix}.svg').write_text(build(theme,group),encoding='utf-8')
    print('Wrote two 20-second animations, each in light/dark themes (3 + 2 scenes).')
