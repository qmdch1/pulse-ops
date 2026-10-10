"""Render a Cloudflare deployment example, using the README's SVG primitives.

Standard library only. All packets share one SMIL clock; no script, remote
assets, or runtime dependency is embedded in the images. Values are synthetic
five-minute samples. The Cloudflare API lane describes a future integration,
not a collector that Pulse Ops already implements.
"""
from pathlib import Path
from xml.sax.saxutils import escape
import runpy

ROOT = Path(__file__).resolve().parents[1]
BASE = runpy.run_path(str(ROOT / 'scripts' / 'render-architecture.py'))
Svg, THEMES, anim, shown, legs = (BASE[k] for k in ('Svg', 'THEMES', 'anim', 'shown', 'legs'))
EASE_IN, EASE_OUT, LINEAR = (BASE[k] for k in ('EASE_IN', 'EASE_OUT', 'LINEAR'))
W, H, T = 1440, 1180, 44.0
anim.__globals__['T'] = T

# Traffic trips use consecutive legs, including the reverse response.
DNS_OUT, DNS_BACK = legs(.5, .9, .9)
HIT_IN, HIT_LOOKUP, HIT_FOUND, HIT_BACK = legs(4.5, .9, .7, .7, .9)
API_IN, API_EDGE, API_APP, CACHE_GET, CACHE_MISS, SQL_GET, SQL_ROWS, API_REPLY, EDGE_REPLY, USER_REPLY = legs(
    9.6, .7, .7, .7, .8, .7, .7, .7, .8, .7, .7)
ERR_IN, ERR_EDGE, ERR_APP, ERR_REPLY, ERR_RETURN, ERR_USER = legs(18.5, .8, .8, .8, .8, .8, .8)
JOBS = [
    ('a-metrics', 24.7, 1.15, 1.1, 0),
    ('b-metrics', 24.9, 1.35, 1.25, 1),
    ('db-stats', 25.1, 1.5, 1.25, 2),
    ('redis-stats', 25.3, 1.65, 1.4, 3),
]
HOST_START = JOBS[0][1] + JOBS[0][2] + JOBS[0][3]
HOST_GET, HOST_BACK = legs(HOST_START, .85, .85)
STORE, CHART = legs(29.3, .8, .9)
CF_GET, CF_DATA = legs(35.0, 1.4, 1.4)
OPS_IN, OPS_EDGE, OPS_GET, OPS_BACK, OPS_RETURN, OPS_USER = legs(38.4, .55, .55, .7, .7, .55, .55)
PHASES = [
    (0, 4, 'violet', '① DNS 조회', 'DNS가 Cloudflare의 주소를 알려 주고, 이후 웹 요청은 프록시를 통과합니다.'),
    (4, 9, 'amber', '② CDN 캐시 HIT', '정적 파일은 엣지 캐시에서 응답합니다. 이 요청은 원본 서버에 도착하지 않습니다.'),
    (9, 18, 'blue', '③ API · DB 처리', 'API는 Nginx → App A로 전달됩니다. Redis MISS 뒤 DB를 조회하고 200으로 응답합니다.'),
    (18, 24, 'red', '④ 500 오류 응답', 'App B가 반환한 500이 같은 경로로 전달됩니다. 최종 응답 코드와 시간은 앱에서 집계합니다.'),
    (24, 34, 'green', '⑤ 내부 지표 수집', 'Pulse Ops는 15초마다 직접 수집합니다. 슬롯 4개 중 하나가 비면 서버 SSH를 수집합니다.'),
    (34, T, 'amber', '⑥ 통합 화면 예시', 'Cloudflare API 연동은 추가 구현 대상입니다. 외부 트래픽과 내부 지표를 구분해 표시하는 설계입니다.'),
]

BROWSER = (56, 352, 178, 198)
DNS = (56, 254, 178, 72)
EDGE = (286, 340, 234, 216)
CACHE = (302, 477, 202, 58)
CF_API = (286, 590, 234, 94)
NGINX = (584, 374, 190, 150)
FRONT = (584, 558, 190, 94)
APP_A = (830, 304, 250, 140)
APP_B = (830, 498, 250, 140)
DB = (1140, 304, 244, 140)
REDIS = (1140, 498, 244, 140)
ENGINE = (60, 808, 235, 112)
SQLITE = (60, 940, 235, 91)
CHART_BOX = (335, 808, 400, 223)
EDGE_STATS = (775, 808, 270, 223)
HOST = (1085, 808, 295, 92)


def text(s, x, y, value, size=15, color='body', weight=400, **kw):
    s.text(s.base, x, y, value, size, color, weight, **kw)


def pill(s, x, y, label, color, width, future=False):
    s.rect(s.base, x, y, width, 23, 11.5, 'block', color, 1,
           extra='stroke-dasharray="4 3"' if future else '')
    text(s, x + width / 2, y + 16, label, 11.5, color, 700, anchor='middle')


def card(s, geometry, title, subtitle, accent='blue', badge=None):
    x, y, w, h = geometry
    s.rect(s.base, x, y, w, h, 12)
    s.base.append(f'<path d="M{x+16},{y+15} v18" stroke="{s.c[accent]}" stroke-width="3" stroke-linecap="round"/>')
    text(s, x + 27, y + 30, title, 18, 'text', 700)
    text(s, x + 18, y + 56, subtitle, 13.5, 'muted')
    if badge:
        label, width = badge
        pill(s, x + w - width - 14, y + 12, label, accent, width)


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


def build(theme):
    c = dict(THEMES[theme])
    c['red_ink'] = c['red']
    c['amber'] = '#ef7a21' if theme == 'light' else '#ffad66'
    c['amber_ink'] = '#b94a0c' if theme == 'light' else '#ffc18d'
    s = Svg(c)
    for name in ('lane', 'violet', 'blue', 'green', 'amber', 'red'):
        s.defs.append(f'<marker id="ah-{name}" viewBox="0 0 10 10" refX="8.5" refY="5" '
                      f'markerWidth="6" markerHeight="6" orient="auto-start-reverse">'
                      f'<path d="M1,1.5 L9,5 L1,8.5 z" fill="{c[name]}"/></marker>')
    s.defs.append(f'<linearGradient id="edge-tint" x2="0" y2="1">'
                  f'<stop stop-color="{c["amber"]}" stop-opacity=".12"/>'
                  f'<stop offset="1" stop-color="{c["amber"]}" stop-opacity=".025"/></linearGradient>')
    s.rect(s.base, .5, .5, W - 1, H - 1, 18, 'bg', 'edge')
    text(s, 36, 38, 'PULSE / OPS  ×  CLOUDFLARE', 13, 'amber_ink', 700, extra='letter-spacing="1.4"')
    text(s, 36, 78, '외부 트래픽부터 내부 지표까지, 하나의 운영 화면으로', 27, 'text', 700)
    text(s, 36, 108, '공인 IP 서버 구성 예시 · app.example.com / ops.example.com · 모든 수치는 가상 5분 표본', 15, 'muted')
    pill(s, 1255, 30, '구성 · 데이터 예시', 'amber', 149)

    # Six scene tabs remain readable when animation is reduced or unsupported.
    for i, (start, end, color, label, _) in enumerate(PHASES):
        x, width = 36 + i * 230, 218
        s.rect(s.base, x, 136, width, 40, 10, 'panel', 'border')
        text(s, x + 16, 161, label, 14, 'muted', 600)
        win = (max(.2, start + .15), end - .2)
        s.fx.append(f'<g opacity="0">{shown(win, fade=.2)}'
                    f'<rect x="{x}" y="136" width="{width}" height="40" rx="10" '
                    f'fill="{c[color]}" fill-opacity="{c["tint"]}" stroke="{c[color]}"/>'
                    f'<text class="t" x="{x+16}" y="161" font-size="14" font-weight="700" '
                    f'fill="{c[color+"_ink"]}">{escape(label)}</text>'
                    f'<rect x="{x+14}" y="171" width="0" height="2" rx="1" fill="{c[color]}">'
                    f'{anim("width", [0, win[0], win[1], T], [0,0,width-28,width-28], ease=LINEAR)}</rect></g>')

    # Panel backgrounds precede the rails and all cards.
    for x, width, heading, subtitle in (
        (36, 504, '인터넷 · Cloudflare', 'DNS 프록시 ON'),
        (564, 840, '원본 서버 · 내부망', 'DB · Redis · /metrics는 내부 연결'),
    ):
        s.rect(s.base, x, 204, width, 506, 16, 'panel', 'border')
        text(s, x + 20, 232, heading, 15, 'text', 700)
        text(s, x + width - 20, 232, subtitle, 12, 'muted', anchor='end')
    s.rect(s.base, 36, 768, 1368, 302, 16, 'panel', 'border')
    text(s, 60, 793, 'PULSE / OPS · 관리 · 수집', 15, 'text', 700)
    text(s, 1380, 793, '청색·녹색: 직접 수집     주황 점선: 연동 예정', 12.5, 'muted', anchor='end')

    paths = {
        'dns': 'M145,352 V327',
        'edge': 'M234,430 H286',
        'cache': 'M403,451 V477',
        'origin': 'M520,430 H584',
        'a': 'M774,410 H797 Q810,410 810,397 V355 Q810,342 823,342 H830',
        'b': 'M774,450 H797 Q810,450 810,463 V543 Q810,556 823,556 H830',
        'sql': 'M1080,367 H1140',
        'redis': 'M1080,414 H1091 Q1104,414 1104,427 V554 Q1104,567 1117,567 H1140',
        'front': 'M679,524 V558',
        'ops': 'M584,494 H554 Q544,494 544,504 V720 Q544,736 528,736 H178 V808',
        'a-metrics': 'M178,808 V750 Q178,732 196,732 H784 Q796,732 796,720 V416 H830',
        'b-metrics': 'M178,808 V750 Q178,732 196,732 H784 Q796,732 796,720 V613 H830',
        'db-stats': 'M178,808 V750 Q178,732 196,732 H1104 Q1118,732 1118,718 V416 H1140',
        'redis-stats': 'M178,808 V750 Q178,732 196,732 H1104 Q1118,732 1118,718 V613 H1140',
        'host': 'M295,884 H314 V1048 H1068 V854 H1085',
        'cf-api': 'M60,859 H22 V728 H266 Q276,728 276,718 V637 H286',
        'store': 'M178,920 V940',
        'chart': 'M295,984 H335',
    }
    for name, path in paths.items():
        s.lane(name, path, arrow=name not in ('store', 'chart'))
        if name == 'cf-api':
            s.base[-1] = s.base[-1].replace('stroke-width="1.5"', 'stroke-width="1.5" stroke-dasharray="5 5"')
    text(s, 778, 727, '관리망 · 개별 대상 주소', 11, 'muted', anchor='end')
    # The public traffic and management rails are deliberately separate.
    text(s, 536, 412, 'HTTPS', 10.5, 'muted', anchor='middle')

    s.rect(s.base, *DNS, 12)
    text(s, 74, 282, 'Cloudflare DNS', 16, 'text', 700)
    text(s, 74, 309, '주소 조회만 담당', 13, 'muted')
    card(s, BROWSER, '브라우저', 'app.example.com', 'violet')
    s.rect(s.base, 74, 427, 142, 99, 7, 'panel', 'border')
    text(s, 85, 449, '사용자 화면', 13, 'text', 600)
    for yy, ww in ((463,108),(476,75),(489,92)):
        s.rect(s.base, 85, yy, ww, 5, 2.5, 'lane', 'lane', 0)
    text(s, 85, 514, 'ops: 운영자 로그인', 10.5, 'muted')

    card(s, EDGE, 'Cloudflare', '프록시 · CDN · HTTPS', 'amber')
    s.rect(s.base, 286, 340, 234, 216, 12, 'url(#edge-tint)', 'amber')
    text(s, 304, 422, 'app 요청 10,000건 / 5분', 15, 'amber_ink', 700)
    text(s, 304, 446, '원본 전달 4,000건', 13, 'body')
    s.rect(s.base, *CACHE, 8, 'block', 'amber')
    text(s, 316, 499, '정적 파일 캐시', 13.5, 'text', 700)
    text(s, 316, 522, 'HIT 6,000건 · 60%', 14, 'amber_ink', 600)
    s.rect(s.base, *CF_API, 12, 'block', 'amber', extra='stroke-dasharray="5 4"')
    text(s, 304, 622, 'Analytics API', 15, 'text', 700)
    text(s, 304, 646, '읽기 토큰 · Zone ID', 13.5, 'muted')
    pill(s, 410, 601, '연동 예정', 'amber', 96, future=True)
    text(s, 304, 666, '별도 조회 주기 · 요금제별 범위', 12, 'muted')
    text(s, 56, 593, 'app → 서비스 화면 + /api', 13.5, 'body')
    text(s, 56, 618, 'ops → Pulse Ops 관리 화면', 13.5, 'body')
    text(s, 56, 650, '주황 구름: 웹 요청이 통과', 12, 'amber_ink', 600)
    text(s, 56, 674, 'DNS only: 웹 통계 수집 불가', 12, 'muted')

    card(s, NGINX, 'Nginx', '도메인 · 경로로 전달', 'blue')
    text(s, 602, 461, '/api → 백엔드', 13, 'body', mono=True)
    text(s, 602, 486, 'ops → Pulse Ops', 13, 'body')
    text(s, 602, 509, '원본 HTTPS · Full (strict)', 10.8, 'muted')
    card(s, FRONT, '프런트 화면', 'HTML · CSS · JS', 'violet')
    text(s, 602, 631, 'API·관리 화면은 캐시 우회', 11.5, 'muted')

    for geometry, title, sub, count, errors, rate, latency, color in (
        (APP_A, 'App A', 'backend-1 · 개별 /metrics', '2,500건', '10건', '0.40%', '240 ms', 'blue'),
        (APP_B, 'App B', 'backend-2 · 개별 /metrics', '1,500건', '5건', '0.33%', '180 ms', 'violet'),
    ):
        card(s, geometry, title, sub, color)
        x, y, w, h = geometry
        note(s, x+18, y+82, '5분 요청 / 5xx', f'{count} / {errors}', width=214)
        note(s, x+18, y+106, '오류율 / P99', f'{rate} / {latency}', color, 214)
        text(s, x+18, y+129, 'Counter · Histogram', 11.5, 'muted', mono=True)
    card(s, DB, 'PostgreSQL', '내부 연결 · 모니터링 계정', 'green')
    note(s, 1158, 386, '연결 수', '24', 'green', 208)
    note(s, 1158, 411, '점검 응답시간', '4 ms', width=208)
    text(s, 1158, 433, '통계 조회 ≠ 업무 SQL 처리시간', 11.5, 'muted')
    card(s, REDIS, 'Redis', '내부 연결 · PING / INFO', 'green')
    note(s, 1158, 580, '캐시 적중률', '96%', 'green', 208)
    note(s, 1158, 605, '사용 메모리', '128 MiB', width=208)
    text(s, 1158, 627, '앱 요청: MISS → DB 조회 예시', 11.5, 'muted')

    card(s, ENGINE, '수집기', '15초 주기 · 최대 4개 동시', 'green')
    for i in range(4):
        xx = 79 + i * 49
        s.rect(s.base, xx, 884, 37, 22, 5, 'panel', 'lane')
        text(s, xx+18.5, 899, str(i+1), 12, 'muted', 600, anchor='middle')
    card(s, SQLITE, 'SQLite', '수집 결과 · 등록 정보 보관', 'green')
    text(s, 78, 1011, '화면 조회와 실제 수집은 독립', 12, 'muted')

    card(s, CHART_BOX, '백엔드 응답시간 P99', '연속 5분의 Histogram 증가량', 'blue')
    for yy in (895, 935, 975):
        s.base.append(f'<path d="M355,{yy} H715" fill="none" stroke="{c["border"]}"/>')
    for color, path in (
        ('blue','M355,956 L400,939 L445,945 L490,908 L535,925 L580,888 L625,903 L670,891 L715,895'),
        ('violet','M355,980 L400,968 L445,975 L490,951 L535,964 L580,937 L625,951 L670,932 L715,935'),
    ):
        s.base.append(f'<path d="{path}" fill="none" stroke="{c[color]}" stroke-width="2" opacity=".32"/>')
        s.fx.append(f'<path d="{path}" fill="none" stroke="{c[color]}" stroke-width="3" pathLength="1" '
                    f'stroke-dasharray="1" stroke-dashoffset="1">'
                    f'{anim("stroke-dashoffset",[0,CHART[0],CHART[1]+1.2,T],[1,1,0,0],ease=LINEAR)}'
                    f'{shown((CHART[0],T-.4))}</path>')
    text(s, 355, 1009, 'A  240 ms', 13, 'blue', 700)
    text(s, 555, 1009, 'B  180 ms', 13, 'violet', 700)

    card(s, EDGE_STATS, '외부 트래픽', 'app 도메인 · API 연동 설계', 'amber')
    pill(s, 915, 820, '연동 예정', 'amber', 116, future=True)
    for i, (label, value, portion, color) in enumerate((
        ('전체 요청','10,000건',1,'amber'), ('CDN HIT','6,000건',.6,'amber'), ('원본 전달','4,000건',.4,'blue'),
    )):
        yy = 902 + i * 43
        note(s, 793, yy-8, label, value, color, 234)
        s.rect(s.base, 793, yy, 234, 5, 2.5, 'border', 'border', 0)
        s.rect(s.base, 793, yy, 234*portion, 5, 2.5, color, color, 0, extra='opacity=".35"')
        s.fx.append(f'<rect x="793" y="{yy}" width="0" height="5" rx="2.5" fill="{c[color]}">'
                    f'{anim("width",[0,CF_DATA[1],CF_DATA[1]+1.3,T],[0,0,234*portion,234*portion],ease=LINEAR)}'
                    f'{shown((CF_DATA[1],T-.4))}</rect>')
    text(s, 793, 1017, '외부·원본 건수를 더하지 않음', 11.8, 'muted')

    card(s, HOST, '서버 OS · SSH', 'CPU 38% · RAM 62% · 디스크 41%', 'green')
    tag(s, 1103, 885, '빈 슬롯에 이어서 수집', 'green', [(HOST_GET[0],HOST_BACK[1]+1)])
    for x, title, value in ((1085,'DB 연결','24'),(1240,'Redis HIT','96%')):
        s.rect(s.base, x, 920, 140, 111, 10)
        text(s, x+16, 946, title, 13, 'muted', 600)
        text(s, x+16, 988, value, 29, 'green', 700)
        text(s, x+16, 1014, '직접 조회한 예시값', 11, 'muted')

    # DNS resolution completes before the HTTP sequence starts.
    trip(s, 'dns', DNS_OUT, 'violet', ease=EASE_IN)
    trip(s, 'dns', DNS_BACK, 'violet', reverse=True, ease=EASE_OUT)
    s.glow_block(DNS, 'violet', [(.4,3.3)])
    tag(s, 69, 343, '응답: Cloudflare 주소', 'violet', [(DNS_BACK[1],3.6)], 11.5)

    # A cache hit is answered without an origin leg.
    for path, interval, reverse, chip, ease in (
        ('edge',HIT_IN,False,None,EASE_IN), ('cache',HIT_LOOKUP,False,None,LINEAR),
        ('cache',HIT_FOUND,True,'HIT',LINEAR), ('edge',HIT_BACK,True,'200',EASE_OUT),
    ):
        trip(s,path,interval,'amber',reverse,chip,ease)
    s.glow_block(EDGE,'amber',[(HIT_IN[1]-.1,HIT_BACK[0]+.2)])
    s.glow_block(CACHE,'amber',[(HIT_LOOKUP[0],HIT_FOUND[1]+.3)])
    tag(s,304,573,'/assets/app.js · 원본 요청 없음','amber',[(4.5,8.4)],12)

    # An API request uses Redis, misses, then reads the database.
    for path, interval, reverse, chip in (
        ('edge',API_IN,False,None), ('origin',API_EDGE,False,None), ('a',API_APP,False,None),
        ('redis',CACHE_GET,False,'GET'), ('redis',CACHE_MISS,True,'MISS'),
        ('sql',SQL_GET,False,None), ('sql',SQL_ROWS,True,'rows'),
        ('a',API_REPLY,True,'200'), ('origin',EDGE_REPLY,True,'200'), ('edge',USER_REPLY,True,'200'),
    ):
        trip(s,path,interval,'blue',reverse,chip)
    for geometry, window in ((NGINX,(API_EDGE[0],EDGE_REPLY[1])),(APP_A,(API_APP[0],API_REPLY[1])),
                             (REDIS,(CACHE_GET[0],CACHE_MISS[1])),(DB,(SQL_GET[0],SQL_ROWS[1]))):
        s.glow_block(geometry,'blue',[window])
    tag(s,588,692,'GET /api/orders → 200 · 180 ms 예시','blue',[(9.6,17.6)])

    # The backend-generated error returns along the complete public path.
    for path, interval, reverse, chip in (
        ('edge',ERR_IN,False,None), ('origin',ERR_EDGE,False,None), ('b',ERR_APP,False,None),
        ('b',ERR_REPLY,True,'500'), ('origin',ERR_RETURN,True,'500'), ('edge',ERR_USER,True,'500'),
    ):
        trip(s,path,interval,'red' if reverse else 'blue',reverse,chip)
    s.glow_block(APP_B,'red',[(ERR_APP[1]-.1,ERR_USER[1]+.3)])
    tag(s,848,664,'App B: 최종 500 · 요청 수 / 5xx / 시간 집계','red',[(20.8,23.6)],12.5)
    tag(s,588,692,'500은 앱에서 발생 · Nginx·Cloudflare가 전달','red',[(18.5,23.7)])

    # Four independent jobs; the fifth starts as the first frees a slot.
    for path, start, outgoing, incoming, slot in JOBS:
        out, back = legs(start,outgoing,incoming)
        trip(s,path,out,'green',chip='GET')
        trip(s,path,back,'blue',reverse=True,chip='data')
        xx = 79 + slot*49
        s.fx.append(f'<rect x="{xx}" y="884" width="37" height="22" rx="5" fill="{c["green"]}" '
                    f'fill-opacity=".25" stroke="{c["green"]}" opacity="0">{shown((out[0],back[1]))}</rect>')
    trip(s,'host',HOST_GET,'green',chip='SSH')
    trip(s,'host',HOST_BACK,'blue',reverse=True,chip='data')
    s.fx.append(f'<rect x="79" y="884" width="37" height="22" rx="5" fill="{c["green"]}" '
                f'fill-opacity=".25" stroke="{c["green"]}" opacity="0">{shown((HOST_GET[0],HOST_BACK[1]))}</rect>')
    s.glow_block(HOST,'green',[(HOST_GET[0],HOST_BACK[1]+.3)])
    s.glow_block(ENGINE,'green',[(24.6,31.3)])
    trip(s,'store',STORE,'green',chip='save')
    trip(s,'chart',CHART,'blue',chip='draw')
    s.glow_block(SQLITE,'green',[(STORE[0],CHART[1]+.5)])
    tag(s,588,692,'수집기 → 인스턴스별 /metrics · DB · Redis','green',[(24.5,33.4)])

    # This lane is visibly labelled planned in both moving and static modes.
    trip(s,'cf-api',CF_GET,'amber',chip='API')
    trip(s,'cf-api',CF_DATA,'amber',reverse=True,chip='data')
    s.glow_block(CF_API,'amber',[(CF_GET[0],CF_DATA[1]+.6)])
    s.glow_block(EDGE_STATS,'amber',[(CF_DATA[1],T-.5)])
    for path, interval, reverse in (
        ('edge',OPS_IN,False),('origin',OPS_EDGE,False),('ops',OPS_GET,False),
        ('ops',OPS_BACK,True),('origin',OPS_RETURN,True),('edge',OPS_USER,True),
    ):
        trip(s,path,interval,'violet',reverse,'200' if reverse else None)
    tag(s,588,692,'ops.example.com → 관리 화면 · 캐시 우회','violet',[(38.4,43.5)])

    # Captions have a static accessible fallback, plus one animated scene at a time.
    s.rect(s.base,36,1090,1368,55,12,'panel','border')
    s.base.append(f'<text class="t rm" x="56" y="1123" font-size="14" fill="{c["body"]}">'
                  'DNS 조회 → CDN HIT → API·DB → 오류 응답 → 직접 수집 → 통합 화면 예시</text>')
    for start,end,color,_,caption in PHASES:
        window = (max(.3,start+.2),end-.3)
        s.fx.append(f'<g opacity="0">{shown(window,fade=.2)}'
                    f'<circle cx="59" cy="1118" r="5" fill="{c[color]}"/>'
                    f'<text class="t" x="76" y="1123" font-size="14" fill="{c["body"]}">{escape(caption)}</text></g>')
    text(s,36,1165,'app 5분 예시: 외부 10,000 = HIT 6,000 + 원본 4,000 · 원본 4,000 = A 2,500 + B 1,500',13,'body',600)
    text(s,1404,1165,'가상값 · 시간 압축 · 동작 줄이기 지원',12,'muted',anchor='end')
    style = ('.t{font-family:Pretendard,"Pretendard Variable","Apple SD Gothic Neo","Malgun Gothic","Noto Sans KR",'
             '"Noto Sans CJK KR","Segoe UI",system-ui,sans-serif}.m{font-family:ui-monospace,Consolas,monospace}'
             'text{text-rendering:geometricPrecision}.rm{display:none}'
             '@media(prefers-reduced-motion:reduce){.fx{display:none}.rm{display:inline}}')
    title = 'Cloudflare DNS·프록시와 Pulse Ops의 트래픽·수집 구성 예시'
    desc = ('44초 반복 애니메이션. DNS 조회 후 Cloudflare가 정적 파일 캐시 HIT를 직접 응답합니다. API 요청은 '
            'Nginx와 App A를 거쳐 Redis MISS 뒤 PostgreSQL을 조회하고, App B의 500은 사용자까지 반환됩니다. '
            'Pulse Ops는 15초 주기, 동시 최대 4개로 인스턴스별 /metrics·DB·Redis·SSH를 직접 수집해 SQLite에 저장합니다. '
            'Cloudflare API 수집과 외부 트래픽 카드에는 연동 예정 표시가 있으며 추가 구현 범위입니다. '
            '모든 값은 가상의 5분 표본입니다. app 도메인의 외부 10000건은 캐시 HIT 6000건과 원본 4000건으로 나뉘고, '
            '원본 요청은 App A 2500건과 App B 1500건입니다. 이동 속도는 실제 지연이 아닙니다.')
    return (f'<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" '
            f'viewBox="0 0 {W} {H}" width="{W}" height="{H}" role="img" aria-labelledby="title desc">'
            f'<title id="title">{escape(title)}</title><desc id="desc">{escape(desc)}</desc>'
            f'<style>{style}</style><defs>{"".join(s.defs)}</defs>{"".join(s.base)}'
            f'<g class="fx">{"".join(s.fx)}</g></svg>\n')


if __name__ == '__main__':
    output = ROOT / 'docs' / 'images'
    output.mkdir(parents=True, exist_ok=True)
    for theme, filename in (('light','cloudflare-traffic-flow.svg'),('dark','cloudflare-traffic-flow-dark.svg')):
        (output / filename).write_text(build(theme), encoding='utf-8')
    print(f'Wrote light/dark Cloudflare traffic examples ({T:g}s loop).')
