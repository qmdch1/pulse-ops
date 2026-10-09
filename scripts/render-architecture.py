"""Render the README architecture animation as light/dark SVG files.

Documentation tool only: standard library, no Pillow, browser or Node.
The SVG uses SMIL so it animates inside a GitHub README <img>. Timings show
logical order, not real latency or throughput.
"""
from pathlib import Path
from xml.sax.saxutils import escape

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / 'docs' / 'images'
W, H = 1200, 740
EASE = '0.65 0 0.35 1'      # ease-in-out cubic for a single packet trip
EASE_IN = '0.5 0 1 1'       # first leg of a chain: speeds up into the next box
EASE_OUT = '0 0 0.5 1'      # last leg of a chain: leaves at speed, settles on arrival
LINEAR = '0 0 1 1'          # middle legs keep their speed through each hand-off
FADE = '0.33 0 0.2 1'       # soft ease for highlights

THEMES = {
    'light': dict(
        bg='#ffffff', panel='#f8f9fa', block='#ffffff', border='#e9ecef', edge='#dee2e6', lane='#ced4da',
        text='#212529', body='#495057', muted='#868e96', faint='#adb5bd', screen='#212529', screen_text='#f8f9fa', on='#ffffff',
        violet='#7950f2', blue='#228be6', green='#2f9e44', amber='#f08c00', red='#e03131',
        violet_ink='#6741d9', blue_ink='#1971c2', green_ink='#2b8a3e', amber_ink='#d9480f', tint='0.07'),
    'dark': dict(
        bg='#0d1117', panel='#161b22', block='#0d1117', border='#21262d', edge='#30363d', lane='#3d444d',
        text='#e6edf3', body='#c9d1d9', muted='#8b949e', faint='#6e7681', screen='#010409', screen_text='#e6edf3', on='#0d1117',
        violet='#9775fa', blue='#4dabf7', green='#51cf66', amber='#ffa94d', red='#ff8787',
        violet_ink='#b197fc', blue_ink='#74c0fc', green_ink='#8ce99a', amber_ink='#ffc078', tint='0.12'),
}


def legs(start, *durations):
    """Contiguous legs: each one leaves the moment the previous one arrives."""
    result = []
    for duration in durations:
        result.append((start, start + duration))
        start += duration
    return result


# Timeline -----------------------------------------------------------------
# Travel times set the pace. Nothing waits inside a box: the next leg leaves the
# moment the previous one arrives, and a phase begins once the last packet of the
# previous phase has settled.
SETTLE = 0.8
REQ = legs(0.5, 0.9)[0]                                             # ① GET / · /assets/*
CHIPS = [(REQ[1] + i * 0.28, REQ[1] + i * 0.28 + 1.0) for i in range(3)]  # HTML · CSS · JS stream out
P2 = CHIPS[-1][1] + SETTLE
POST, API1, DBR, ROWS, RES1, BACK = legs(P2 + 0.3, 0.35, 0.9, 0.5, 0.5, 0.9, 0.35)  # ② cache miss
API2, RES2 = legs(BACK[1], 0.9, 0.9)                                # the next poll hits the cache
P3 = RES2[1] + SETTLE
OUT0 = P3 + 0.4                                                     # ③ four slots fan out
FANS = [(name, slot, OUT0 + 0.08 * i) for i, (name, slot) in enumerate((('server', 0), ('db', 1), ('redis', 2), ('http', 3)))]
QUEUE_IN = legs(FANS[3][2] + 1.0 + 0.9, 0.3)[0]                     # app takes slot 4 as http returns
JOBS = FANS + [('app', 3, QUEUE_IN[1])]
LAST_COMMIT = max(out for *_, out in JOBS) + 1.0 + 0.9 + 0.55
NOTIFY = legs(LAST_COMMIT, 1.0)[0]
P4 = NOTIFY[1] + SETTLE
TERM1, TICKET, WS, PTY = legs(P4 + 0.15, 0.9, 0.9, 0.85, 0.75)       # ④ ticket → WebSocket → SSH
TYPE_START = PTY[1]
KEY, KEY_PTY, OUT_PTY, OUT_TERM = legs(TYPE_START + 5 * 0.12, 0.45, 0.37, 0.37, 0.45)
T = round(OUT_TERM[1] + 1.4, 2)

PHASES = [  # (start, end, color, pill label, caption)
    (0.0, P2, 'violet', '① 화면 로드', 'Go가 실행 파일에 내장한 HTML·CSS·ES 모듈을 gzip·ETag로 내려 줍니다.'),
    (P2, P3, 'blue', '② 화면 갱신', 'Worker가 /api/monitoring을 읽습니다. 3초 안의 같은 조회는 공유 캐시로 답하고 수집을 호출하지 않습니다.'),
    (P3, P4, 'green', '③ 수집·알림', '15초 수집 결과를 저장한 뒤 이벤트를 판정합니다. 수신 분기·점검·묶음 적용 후 발생·복구와 요약을 전송합니다.'),
    (P4, T, 'amber', '④ 터미널', '30초 단회 티켓으로 WebSocket을 연 뒤 Go가 SSH PTY를 중계합니다. 감사 기록에는 연결·종료만 남습니다.'),
]

# Geometry -----------------------------------------------------------------
PANEL_Y, PANEL_H = 120, 395
BROWSER, GO, INFRA = (40, 260), (450, 340), (900, 260)        # x, width
XTERM, UI, WORKER = (58, 155, 224, 60), (58, 240, 224, 90), (58, 355, 224, 90)
TERM, STATIC = (470, 158, 300, 54), (470, 250, 300, 50)
API, SCHED = (470, 340, 142, 160), (628, 340, 142, 160)
SQLITE = (470, 572, 300, 70)
SERVER, DB, REDIS, HTTP, APP = [(916, y, 228, h) for y, h in ((155, 80), (247, 52), (311, 52), (375, 52), (439, 52))]
SLOT_X, SLOT_YS, QUEUE = 744, (362, 388, 414, 440), (753, 480)
LEFT_GAP, RIGHT_GAP = (282, 470), (770, 916)


def num(v):
    return f'{v:.4f}'.rstrip('0').rstrip('.')


def keytimes(times):
    for a, b in zip(times, times[1:]):
        if b < a - 1e-9:
            raise ValueError(f'non-monotonic key times: {times}')
    return ';'.join(num(t / T) for t in times)


def anim(attr, times, values, ease=FADE, discrete=False):
    """One looping SMIL track; every track shares the same T-second clock."""
    mode = 'calcMode="discrete"' if discrete else f'calcMode="spline" keySplines="{";".join([ease] * (len(times) - 1))}"'
    return (f'<animate attributeName="{attr}" dur="{num(T)}s" repeatCount="indefinite" '
            f'keyTimes="{keytimes(times)}" values="{";".join(str(v) for v in values)}" {mode}/>')


def shown(*windows, peak=1, fade=0.28, base=0):
    """Opacity track visible inside each (start, end) window with soft fades."""
    times, values = [0.0], [base]
    for start, end in windows:
        times += [max(start - fade, times[-1]), start, end, min(end + fade, T)]
        values += [base, peak, peak, base]
    times.append(T)
    values.append(base)
    return anim('opacity', times, values)


def motion(path, start, end, reverse=False, ease=EASE):
    points = '1;1;0;0' if reverse else '0;0;1;1'
    return (f'<animateMotion dur="{num(T)}s" repeatCount="indefinite" calcMode="spline" keyPoints="{points}" '
            f'keyTimes="{keytimes([0, start, end, T])}" keySplines="0 0 1 1;{ease};0 0 1 1">'
            f'<mpath xlink:href="#{path}"/></animateMotion>')


class Svg:
    def __init__(self, c):
        self.c, self.defs, self.base, self.fx = c, [], [], []

    def color(self, name):
        return self.c.get(name, name)

    # primitives
    def rect(self, layer, x, y, w, h, rx=10, fill='block', stroke='edge', sw=1, extra=''):
        layer.append(f'<rect x="{num(x)}" y="{num(y)}" width="{num(w)}" height="{num(h)}" rx="{rx}" '
                     f'fill="{self.color(fill)}" stroke="{self.color(stroke)}" stroke-width="{sw}" {extra}/>')

    def text(self, layer, x, y, value, size=12, fill='body', weight=400, anchor='start', mono=False, extra=''):
        cls = 'm' if mono else 't'
        layer.append(f'<text class="{cls}" x="{num(x)}" y="{num(y)}" font-size="{size}" font-weight="{weight}" '
                     f'fill="{self.color(fill)}" text-anchor="{anchor}" {extra}>{escape(value)}</text>')

    def lane(self, pid, d, arrow=True):
        self.defs.append(f'<path id="{pid}" d="{d}"/>')
        marker = ' marker-end="url(#ah-lane)"' if arrow else ''
        self.base.append(f'<path d="{d}" fill="none" stroke="{self.c["lane"]}" stroke-width="1.5" stroke-linecap="round"{marker}/>')

    def glow_lane(self, pid, color, windows, arrow=True, width=2):
        marker = f' marker-end="url(#ah-{color})"' if arrow else ''
        self.fx.append(f'<use xlink:href="#{pid}" fill="none" stroke="{self.c[color]}" stroke-width="{width}" '
                       f'stroke-linecap="round" opacity="0"{marker}>{shown(*windows)}</use>')

    def glow_block(self, box, color, windows, rx=10):
        x, y, w, h = box
        self.fx.append(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{rx}" fill="{self.c[color]}" '
                       f'fill-opacity="{self.c["tint"]}" stroke="{self.c[color]}" stroke-width="1.5" opacity="0">'
                       f'{shown(*windows)}</rect>')

    def pulse(self, x, y, color, at, radius=17):
        times = [0, max(at - 0.01, 0.001), at, min(at + 0.7, T - 0.001), T]
        self.fx.append(f'<circle cx="{x}" cy="{y}" r="5" fill="none" stroke="{self.c[color]}" stroke-width="2" opacity="0">'
                       f'{anim("r", times, [5, 5, 5, radius, radius], ease="0.2 0.7 0.3 1")}'
                       f'{anim("opacity", times, [0, 0, 0.55, 0, 0], ease="0.2 0.7 0.3 1")}</circle>')

    def packet(self, pid, start, end, color, reverse=False, chip=None, r=5, arrive=None, trail=True, ease=EASE):
        """A dot with a fading comet tail and an optional moving label."""
        tail = [(0.12, r * 0.48, 0.16), (0.06, r * 0.72, 0.34)] if trail else []
        for lag, radius, alpha in tail:
            self.fx.append(f'<g opacity="0">{motion(pid, start + lag, end + lag, reverse, ease)}'
                           f'{shown((start + lag, end + lag), peak=alpha, fade=0.14)}'
                           f'<circle r="{num(radius)}" fill="{self.c[color]}"/></g>')
        if chip:  # labelled packets ride the lane as a pill instead of a dot
            width = 14 + 6.6 * len(chip)
            body = (f'<rect x="{num(-width / 2 - 2)}" y="-10.5" width="{num(width + 4)}" height="21" rx="10.5" fill="{self.c["bg"]}" opacity="0.9"/>'
                    f'<rect x="{num(-width / 2)}" y="-8.5" width="{num(width)}" height="17" rx="8.5" fill="{self.c[color]}"/>'
                    f'<text class="t" y="3.8" font-size="10.5" font-weight="700" fill="{self.c["on"]}" text-anchor="middle">{escape(chip)}</text>')
        else:
            body = f'<circle r="{r + 2.2}" fill="{self.c["bg"]}" opacity="0.9"/><circle r="{r}" fill="{self.c[color]}"/>'
        self.fx.append(f'<g opacity="0">{motion(pid, start, end, reverse, ease)}{shown((start, end), fade=0.16)}{body}</g>')
        if arrive:
            self.pulse(*arrive, color, end)


def block(s, box, title, subtitle, mono=False):
    x, y, w, h = box
    s.rect(s.base, x, y, w, h)
    s.text(s.base, x + 16, y + 24, title, 13, 'text', 700, mono=mono)
    s.text(s.base, x + 16, y + 42, subtitle, 11.5, 'muted')


def panel(s, x, w, title, note):
    s.rect(s.base, x, PANEL_Y, w, PANEL_H, 14, 'panel', 'border')
    s.text(s.base, x + 18, PANEL_Y + 23, title, 12, 'text', 700)
    s.text(s.base, x + w - 18, PANEL_Y + 23, note, 11, 'muted', anchor='end')


def build(theme):
    c = THEMES[theme]
    s = Svg(c)
    p1, p2, p3, p4 = [(max(a + 0.1, 0.4), b - (0.4 if b == T else 0.2)) for a, b, *_ in PHASES]

    # arrowheads in neutral and phase colors
    for name in ('lane', 'violet', 'blue', 'green', 'amber'):
        s.defs.append(f'<marker id="ah-{name}" viewBox="0 0 10 10" refX="8.5" refY="5" markerWidth="6.5" markerHeight="6.5" '
                      f'orient="auto-start-reverse"><path d="M1,1.2 L8.8,5 L1,8.8 z" fill="{c[name]}"/></marker>')

    # canvas + heading
    s.rect(s.base, 0.5, 0.5, W - 1, H - 1, 16, 'bg', 'edge')
    s.text(s.base, 40, 46, 'PULSE / OPS · 트래픽 흐름', 11.5, 'violet_ink', 700, extra='letter-spacing="0.6"')
    s.text(s.base, 40, 77, 'Go 서비스 하나가 화면, 수집, 터미널 요청을 함께 처리합니다', 21, 'text', 700)

    # phase pills with progress bars
    px = 690
    for index, (start, end, color, label, _) in enumerate(PHASES):
        x = px + index * 118
        s.rect(s.base, x, 34, 108, 30, 15, 'bg', 'edge')
        s.text(s.base, x + 54, 53.5, label, 12, 'muted', 600, 'middle')
        win = (start + 0.12, end - 0.12) if index else (0.3, end - 0.12)
        s.fx.append(f'<g opacity="0">{shown(win, fade=0.25)}'
                    f'<rect x="{x}" y="34" width="108" height="30" rx="15" fill="{c[color]}" fill-opacity="{c["tint"]}" stroke="{c[color]}" stroke-width="1.5"/>'
                    f'<text class="t" x="{x + 54}" y="53.5" font-size="12" font-weight="700" fill="{c[color + "_ink"]}" text-anchor="middle">{escape(label)}</text>'
                    f'<rect x="{x + 16}" y="70" width="0" height="2.5" rx="1.25" fill="{c[color]}">'
                    f'{anim("width", [0, win[0], win[1], T], [0, 0, 76, 76], ease="0 0 1 1")}</rect></g>')

    # panels and blocks
    panel(s, *BROWSER, '브라우저', '같은 출처')
    panel(s, *GO, 'Go 서비스', '단일 프로세스 · 단일 컨테이너')
    panel(s, *INFRA, '등록한 인프라', '직접 연결')
    for i, dx in enumerate((0, 11, 22)):
        s.base.append(f'<circle cx="{BROWSER[0] + 84 + dx}" cy="{PANEL_Y + 19}" r="3" fill="{c["lane"]}"/>')

    block(s, XTERM, 'xterm', '열 때만 로드')
    block(s, UI, 'UI 스레드', 'Canvas 그래프')
    block(s, WORKER, 'Worker', '조회 · 이벤트 규칙 평가')
    block(s, TERM, '터미널 게이트웨이', '단회 티켓 30초 → WebSocket → SSH PTY')
    block(s, STATIC, '정적 파일', '실행 파일 내장 · gzip · ETag')
    block(s, API, '/api/monitoring', '3초 공유 캐시', mono=True)
    block(s, SCHED, '수집·이벤트', '등록 ID별 1개')
    s.text(s.base, SCHED[0] + 16, SCHED[1] + 60, '동시 최대 4', 11.5, 'muted')
    s.text(s.base, SCHED[0] + 16, SCHED[1] + 78, '발생·복구 판정', 10.5, 'muted')
    s.text(s.base, API[0] + 16, API[1] + 145, '/api/control/*', 9.5, 'muted', mono=True)
    for box, title, sub in ((SERVER, '서버', 'Linux · macOS · Windows'), (DB, '데이터베이스', 'PostgreSQL·MySQL·MariaDB·Oracle'),
                            (REDIS, 'Redis', 'PING · INFO'), (HTTP, 'HTTP 서비스', '응답 코드 · TLS 만료'),
                            (APP, '애플리케이션', '/metrics 카운터·히스토그램')):
        block(s, box, title, sub)
        s.base.append(f'<circle cx="{box[0] + box[2] - 16}" cy="{box[1] + 19}" r="3.5" fill="{c["lane"]}"/>')
    s.text(s.base, SERVER[0] + 16, SERVER[1] + 62, 'SSH · jump 최대 8홉 · 호스트 키 고정', 11.5, 'muted')

    # SQLite volume
    x, y, w, h = SQLITE
    s.rect(s.base, x, y, w, h)
    s.base.append(f'<g fill="none" stroke="{c["muted"]}" stroke-width="1.4"><ellipse cx="{x + 32}" cy="{y + 22}" rx="13" ry="4.5"/>'
                  f'<path d="M{x + 19},{y + 22} v24 a13,4.5 0 0 0 26,0 v-24"/><path d="M{x + 19},{y + 34} a13,4.5 0 0 0 26,0"/></g>')
    s.text(s.base, x + 58, y + 24, 'SQLite 볼륨', 13, 'text', 700)
    s.text(s.base, x + 58, y + 42, '등록·연동·운영 기록 AES-256-GCM', 11, 'muted')
    s.text(s.base, x + 58, y + 58, '관측 15일 · 감사·사건 90일 보존', 11.5, 'muted')

    # UI mini chart, xterm screen, static file icons, scheduler slots
    for gy in (258, 281, 304):
        s.base.append(f'<line x1="176" y1="{gy}" x2="268" y2="{gy}" stroke="{c["border"]}" stroke-width="1"/>')
    chart = [(178, 298), (186, 292), (194, 295), (202, 284), (210, 288), (218, 276), (226, 280), (234, 268), (242, 273), (250, 262), (258, 266), (266, 259)]
    s.rect(s.base, 168, 163, 104, 44, 6, 'screen', 'screen')
    s.text(s.base, 177, 181, '$', 11, 'green', 700, mono=True)
    for i, fx in enumerate((714, 732, 750)):
        s.base.append(f'<path d="M{fx},{262} h8 l4,4 v11 h-12 z" fill="none" stroke="{c["lane"]}" stroke-width="1.3" stroke-linejoin="round"/>')
    for sy in SLOT_YS:
        s.rect(s.base, SLOT_X, sy, 18, 18, 4, 'block', 'lane', 1.3)
    s.text(s.base, SLOT_X + 9, SLOT_YS[0] - 6, '슬롯', 10.5, 'faint', 600, 'middle')
    clock = (SCHED[0] + 26, SCHED[1] + 112)
    s.base.append(f'<circle cx="{clock[0]}" cy="{clock[1]}" r="10" fill="none" stroke="{c["muted"]}" stroke-width="1.4"/>')
    s.text(s.base, clock[0] + 18, clock[1] + 4, '15초 주기', 11.5, 'body', 600)

    # lanes ------------------------------------------------------------------
    l0, l1 = LEFT_GAP
    r0, r1 = RIGHT_GAP
    for name, yy in (('term', 174), ('static', 264), ('api', 389)):
        s.lane(f'{name}-req', f'M{l0 + 2},{yy} H{l1 - 2}')
        s.lane(f'{name}-res', f'M{l1 - 2},{yy + 22} H{l0 + 2}')
    s.lane('pty-req', f'M{r0 + 2},174 H{r1 - 2}')
    s.lane('pty-res', f'M{r1 - 2},196 H{r0 + 2}')
    s.lane('post-req', 'M112,332 V353')
    s.lane('post-res', 'M128,353 V332')
    s.lane('db-read', 'M530,502 V570')
    s.lane('db-rows', 'M552,570 V502')
    s.lane('commit', 'M699,502 V570')
    s.defs.append(f'<path id="queue-in" d="M{QUEUE[0]},{QUEUE[1]} V{SLOT_YS[3] + 9}"/>')
    targets = {'server': SERVER[1] + 67, 'db': DB[1] + 26, 'redis': REDIS[1] + 26, 'http': HTTP[1] + 26, 'app': APP[1] + 26}
    for slot, name in ((0, 'server'), (1, 'db'), (2, 'redis'), (3, 'http'), (3, 'app')):
        sy, ty = SLOT_YS[slot] + 9, targets[name]
        s.lane(f'fan-{name}', f'M{SLOT_X + 18},{sy} H{r0} C{r0 + 74},{sy} {r1 - 74},{ty} {r1},{ty}', arrow=False)

    labels = [((l0 + l1) / 2, 162, 'POST /terminals → WS', True), ((l0 + l1) / 2, 216, '단회 티켓 · 터미널 출력', False),
              ((l0 + l1) / 2, 252, 'GET / · /assets/*', True), ((l0 + l1) / 2, 306, 'gzip · ETag 응답', False),
              ((l0 + l1) / 2, 377, 'GET /api/monitoring', True), ((l0 + l1) / 2, 431, 'JSON 스냅샷', False),
              ((r0 + r1) / 2, 162, 'SSH PTY', True), ((r0 + r1) / 2, 216, '입력 · 출력', False)]
    for lx, ly, value, mono in labels:
        s.text(s.base, lx, ly, value, 10.5, 'muted', 500, 'middle', mono=mono)
    s.text(s.base, 138, 347, 'postMessage', 10.5, 'muted', 500, mono=True)
    s.text(s.base, 522, 541, '관측값 읽기', 10.5, 'muted', 500, 'end')
    s.text(s.base, 709, 541, '결과 저장', 10.5, 'muted', 500)
    s.rect(s.base, 820, 528, 340, 28, 7, 'panel', 'border')
    s.text(s.base, 990, 547, '알림 → Webhook · Slack · Discord · Teams', 11, 'muted', 500, 'middle')
    s.lane('notify', 'M772,607 H796 V542 H820')
    s.text(s.base, WORKER[0], WORKER[1] + 116, '화면이 숨겨지면 조회를 멈춥니다.', 11, 'faint')

    # ① page load --------------------------------------------------------------
    s.glow_block(UI, 'violet', [p1])
    s.glow_block(STATIC, 'violet', [p1])
    s.glow_lane('static-req', 'violet', [(REQ[0] - 0.15, REQ[1] + 0.15)])
    s.glow_lane('static-res', 'violet', [(CHIPS[0][0] - 0.1, CHIPS[-1][1] + 0.4)])
    s.packet('static-req', *REQ, 'violet', arrive=(l1, 264), ease=EASE_IN)
    for i, (name, (start, end)) in enumerate(zip(('HTML', 'CSS', 'JS'), CHIPS)):
        s.packet('static-res', start, end, 'violet', chip=name, arrive=(l0, 286) if i == 2 else None, ease=EASE_OUT)
        fx = 714 + i * 18
        s.fx.append(f'<path d="M{fx},{262} h8 l4,4 v11 h-12 z" fill="{c["violet"]}" fill-opacity="0.18" stroke="{c["violet"]}" '
                    f'stroke-width="1.3" stroke-linejoin="round" opacity="0">{shown((start - 0.05, p1[1]))}</path>')
    s.fx.append(f'<g opacity="0">{shown((CHIPS[-1][1], T - 0.45), fade=0.4)}'
                + ''.join(f'<line x1="176" y1="{gy}" x2="268" y2="{gy}" stroke="{c["lane"]}" stroke-width="1"/>' for gy in (258, 281, 304))
                + '</g>')

    # ② refresh ----------------------------------------------------------------
    def lit(leg, after=0.15):
        return (leg[0] - 0.1, leg[1] + after)
    for box in (UI, WORKER, API, SQLITE):
        s.glow_block(box, 'blue', [p2])
    s.glow_lane('post-req', 'blue', [lit(POST)])
    s.glow_lane('api-req', 'blue', [lit(API1), lit(API2)])
    s.glow_lane('db-read', 'blue', [lit(DBR)])
    s.glow_lane('db-rows', 'blue', [lit(ROWS)])
    s.glow_lane('api-res', 'blue', [lit(RES1), lit(RES2, 0.3)])
    s.glow_lane('post-res', 'blue', [lit(BACK)])
    s.packet('post-req', *POST, 'blue', trail=False, arrive=(112, 355), ease=EASE_IN)
    s.packet('api-req', *API1, 'blue', arrive=(l1, 389), ease=LINEAR)
    s.packet('db-read', *DBR, 'blue', arrive=(530, 572), ease=LINEAR)
    s.packet('db-rows', *ROWS, 'blue', arrive=(552, 500), ease=LINEAR)
    s.packet('api-res', *RES1, 'blue', chip='JSON', arrive=(l0, 411), ease=LINEAR)
    s.packet('post-res', *BACK, 'blue', trail=False, arrive=(128, 330), ease=EASE_OUT)
    s.packet('api-req', *API2, 'blue', arrive=(l1, 389), ease=EASE_IN)
    s.packet('api-res', *RES2, 'blue', chip='캐시', ease=EASE_OUT)
    chip_x, chip_y = API[0] + 16, API[1] + 96
    s.rect(s.base, chip_x, chip_y, 110, 24, 12, 'panel', 'edge')
    s.text(s.base, chip_x + 55, chip_y + 16, '캐시 상태', 11, 'faint', 600, 'middle')
    for (start, end), color, label in (((API1[1], ROWS[1] + 0.3), 'amber', '만료 → SQLite'), ((API2[1], RES2[1]), 'green', '캐시 적중')):
        s.fx.append(f'<g opacity="0">{shown((start, end), fade=0.2)}<rect x="{chip_x}" y="{chip_y}" width="110" height="24" rx="12" '
                    f'fill="{c["bg"]}" stroke="{c[color]}" stroke-width="1.5"/><text class="t" x="{chip_x + 55}" y="{chip_y + 16}" '
                    f'font-size="11" font-weight="700" fill="{c[color + "_ink"]}" text-anchor="middle">{escape(label)}</text></g>')
    s.fx.append(f'<g opacity="0">{shown((RES1[1], BACK[1] + 0.8), fade=0.2)}<rect x="{WORKER[0] + 16}" y="{WORKER[1] + 56}" width="78" height="22" rx="11" '
                f'fill="{c["blue"]}" fill-opacity="{c["tint"]}" stroke="{c["blue"]}" stroke-width="1.2"/><text class="t" x="{WORKER[0] + 55}" '
                f'y="{WORKER[1] + 71}" font-size="11" font-weight="700" fill="{c["blue_ink"]}" text-anchor="middle">규칙 평가</text></g>')
    line = 'M' + ' L'.join(f'{px},{py}' for px, py in chart)
    s.fx.append(f'<path d="{line}" fill="none" stroke="{c["blue"]}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" '
                f'pathLength="1" stroke-dasharray="1 1" stroke-dashoffset="1" opacity="0">'
                f'{anim("stroke-dashoffset", [0, BACK[1], BACK[1] + 0.85, T], [1, 1, 0, 0], ease=EASE)}{shown((BACK[1], T - 0.45), fade=0.35)}</path>')
    s.pulse(266, 259, 'blue', RES2[1], 12)

    # ③ collection -------------------------------------------------------------
    s.glow_block(SCHED, 'green', [p3])
    first_commit = JOBS[0][2] + 1.0 + 0.9 + 0.55
    s.glow_block(SQLITE, 'green', [(first_commit - 0.1, p3[1])])
    s.fx.append(f'<g opacity="0">{shown(p3)}<circle cx="{clock[0]}" cy="{clock[1]}" r="10" fill="{c["green"]}" fill-opacity="{c["tint"]}" '
                f'stroke="{c["green"]}" stroke-width="1.5"/></g>')
    s.fx.append(f'<line x1="{clock[0]}" y1="{clock[1]}" x2="{clock[0]}" y2="{clock[1] - 6.5}" stroke="{c["green"]}" stroke-width="1.6" '
                f'stroke-linecap="round"><animateTransform attributeName="transform" type="rotate" dur="{num(T)}s" repeatCount="indefinite" '
                f'calcMode="spline" keyTimes="{keytimes([0, P3 + 0.05, P3 + 0.4, T])}" keySplines="0 0 1 1;{EASE};0 0 1 1" '
                f'values="0 {clock[0]} {clock[1]};0 {clock[0]} {clock[1]};360 {clock[0]} {clock[1]};360 {clock[0]} {clock[1]}"/></line>')
    s.base.append(f'<path d="M{clock[0]},{clock[1] - 6} V{clock[1]} H{clock[0] + 5}" fill="none" stroke="{c["muted"]}" stroke-width="1.4" '
                  f'stroke-linecap="round" stroke-linejoin="round"/>')
    s.pulse(*clock, 'green', P3 + 0.1, 22)
    boxes = {'server': SERVER, 'db': DB, 'redis': REDIS, 'http': HTTP, 'app': APP}
    for name, slot, out in JOBS:  # each target answers the moment the query arrives
        box, sy = boxes[name], SLOT_YS[slot]
        reply, done = out + 1.0, out + 1.9
        s.fx.append(f'<rect x="{SLOT_X}" y="{sy}" width="18" height="18" rx="4" fill="{c["green"]}" opacity="0">'
                    f'{shown((out - 0.05, done), fade=0.18)}</rect>')
        s.glow_lane(f'fan-{name}', 'green', [(out - 0.1, done + 0.05)], arrow=False)
        s.glow_block(box, 'green', [(reply - 0.1, reply + 0.5)])
        s.packet(f'fan-{name}', out, reply, 'green', arrive=(r1, box[1] + (67 if name == 'server' else 26)), ease=EASE_IN)
        s.packet(f'fan-{name}', reply, done, 'green', reverse=True, ease=LINEAR)
        s.fx.append(f'<circle cx="{box[0] + box[2] - 16}" cy="{box[1] + 19}" r="3.5" fill="{c["green"]}" opacity="0">'
                    f'{shown((reply, p3[1]), fade=0.2)}</circle>')
        s.packet('commit', done, done + 0.55, 'green', trail=False, r=4, arrive=(699, 572), ease=EASE_OUT)
    s.glow_lane('commit', 'green', [(JOBS[0][2] + 1.8, LAST_COMMIT + 0.1)])
    s.glow_lane('notify', 'green', [(NOTIFY[0] - 0.15, NOTIFY[1] + 0.25)])
    s.packet('notify', *NOTIFY, 'green', trail=False, arrive=(820, 542), ease=EASE_OUT)
    s.fx.append(f'<g opacity="0">{shown((OUT0, QUEUE_IN[0] + 0.05), fade=0.18)}<circle cx="{QUEUE[0]}" cy="{QUEUE[1]}" r="5" fill="{c["green"]}"/>'
                f'</g>')
    s.fx.append(f'<g opacity="0">{shown((OUT0, QUEUE_IN[0]), fade=0.18)}<text class="t" x="{QUEUE[0] - 12}" y="{QUEUE[1] + 4}" font-size="11" '
                f'font-weight="700" fill="{c["green_ink"]}" text-anchor="end">대기 1</text></g>')
    s.packet('queue-in', *QUEUE_IN, 'green', trail=False, ease=EASE_IN)
    s.fx.append(f'<g opacity="0">{shown((first_commit, p3[1]), fade=0.2)}<rect x="{SQLITE[0] + 210}" y="{SQLITE[1] + 10}" width="78" height="20" rx="10" '
                f'fill="{c["green"]}" fill-opacity="{c["tint"]}" stroke="{c["green"]}" stroke-width="1.2"/><text class="t" x="{SQLITE[0] + 249}" '
                f'y="{SQLITE[1] + 24}" font-size="10.5" font-weight="700" fill="{c["green_ink"]}" text-anchor="middle">관측 저장</text></g>')
    s.fx.append(f'<g opacity="0">{shown((DBR[1] - 0.1, ROWS[1] + 0.25), fade=0.2)}<rect x="{SQLITE[0] + 210}" y="{SQLITE[1] + 10}" width="78" height="20" rx="10" '
                f'fill="{c["blue"]}" fill-opacity="{c["tint"]}" stroke="{c["blue"]}" stroke-width="1.2"/><text class="t" x="{SQLITE[0] + 249}" '
                f'y="{SQLITE[1] + 24}" font-size="10.5" font-weight="700" fill="{c["blue_ink"]}" text-anchor="middle">스냅샷 조회</text></g>')

    # ④ terminal ---------------------------------------------------------------
    for box in (XTERM, TERM):
        s.glow_block(box, 'amber', [p4])
    s.glow_block(SERVER, 'amber', [(PTY[1] - 0.1, p4[1])])
    s.glow_lane('term-req', 'amber', [lit(TERM1), (WS[0] - 0.1, p4[1])])
    s.glow_lane('term-res', 'amber', [lit(TICKET), lit(OUT_TERM, 0.25)])
    s.glow_lane('pty-req', 'amber', [(PTY[0] - 0.1, p4[1])])
    s.glow_lane('pty-res', 'amber', [lit(OUT_PTY, 0.2)])
    s.packet('term-req', *TERM1, 'amber', arrive=(l1, 174), ease=EASE_IN)
    s.packet('term-res', *TICKET, 'amber', chip='티켓 30초', arrive=(l0, 196), ease=LINEAR)
    s.packet('term-req', *WS, 'amber', chip='WS', arrive=(l1, 174), ease=LINEAR)
    s.packet('pty-req', *PTY, 'amber', arrive=(r1, 174), ease=EASE_OUT)
    s.packet('term-req', *KEY, 'amber', r=3.5, trail=False, ease=EASE_IN)
    s.packet('pty-req', *KEY_PTY, 'amber', r=3.5, trail=False, arrive=(r1, 174), ease=LINEAR)
    s.packet('pty-res', *OUT_PTY, 'amber', r=3.5, trail=False, ease=LINEAR)
    s.packet('term-res', *OUT_TERM, 'amber', r=3.5, trail=False, arrive=(l0, 196), ease=EASE_OUT)
    s.fx.append(f'<g opacity="0">{shown((PTY[1], p4[1]), fade=0.2)}<text class="t" x="{(l0 + l1) / 2}" y="148" font-size="10.5" font-weight="700" '
                f'fill="{c["amber_ink"]}" text-anchor="middle">연결 유지</text></g>')
    keys = [TYPE_START + i * 0.12 for i in range(5)]
    for i, ch in enumerate('df -h'):
        s.fx.append(f'<text class="m" x="{188 + i * 7}" y="181" font-size="11" fill="{c["screen_text"]}" opacity="0">'
                    f'{escape(ch)}{shown((keys[i], p4[1]), fade=0.02)}</text>')
    cursor_x = [188 + i * 7 for i in range(6)]
    s.fx.append(f'<rect x="188" y="172" width="6" height="11" fill="{c["amber"]}" opacity="0">'
                f'{anim("x", [0, *keys, T], [*cursor_x, cursor_x[5]], discrete=True)}'
                f'{shown((TYPE_START - 0.15, KEY[0] + 0.12), fade=0.05)}</rect>')
    for i, width in enumerate((68, 44)):
        s.fx.append(f'<rect x="177" y="{189 + i * 8}" width="{width}" height="4" rx="2" fill="{c["faint"]}" opacity="0">'
                    f'{shown((OUT_TERM[1] + i * 0.12, p4[1]), fade=0.18)}</rect>')
    s.fx.append(f'<g opacity="0">{shown((PTY[1] - 0.05, p4[1]), fade=0.2)}<rect x="{SQLITE[0] + 202}" y="{SQLITE[1] + 10}" width="86" height="20" rx="10" '
                f'fill="{c["amber"]}" fill-opacity="{c["tint"]}" stroke="{c["amber"]}" stroke-width="1.2"/><text class="t" x="{SQLITE[0] + 245}" '
                f'y="{SQLITE[1] + 24}" font-size="10.5" font-weight="700" fill="{c["amber_ink"]}" text-anchor="middle">감사: 연결만</text></g>')
    s.glow_block(SQLITE, 'amber', [(PTY[1] - 0.05, p4[1])])

    # timing strip and connection rules ---------------------------------------------
    s.text(s.base, 40, 578, '두 주기는 서로 독립적입니다', 12, 'text', 700)
    s.text(s.base, 40, 607, '화면 조회 · 예 3초', 11, 'muted')
    s.text(s.base, 40, 637, '실제 수집 · 15초', 11, 'muted')
    for yy in (603, 633):
        s.base.append(f'<line x1="150" y1="{yy}" x2="414" y2="{yy}" stroke="{c["border"]}" stroke-width="1.5"/>')
    for i, xx in enumerate(range(150, 415, 20)):
        s.base.append(f'<circle cx="{xx}" cy="603" r="3" fill="{c["lane"]}"/>')
    for xx in (150, 250, 350):
        s.base.append(f'<rect x="{xx - 2.5}" y="625" width="5" height="16" rx="2.5" fill="{c["lane"]}"/>')
    for xx, at in ((230, API1[0]), (250, API2[0])):
        s.fx.append(f'<circle cx="{xx}" cy="603" r="4" fill="{c["blue"]}" opacity="0">{shown((at, p2[1]), fade=0.2)}</circle>')
        s.pulse(xx, 603, 'blue', at, 12)
    s.fx.append(f'<rect x="247.5" y="625" width="5" height="16" rx="2.5" fill="{c["green"]}" opacity="0">{shown((P3 + 0.1, p3[1]), fade=0.2)}</rect>')
    s.pulse(250, 633, 'green', P3 + 0.1, 14)
    s.text(s.base, 820, 578, '직접 연결 원칙', 12, 'text', 700)
    for i, value in enumerate(('저장한 비밀값은 응답에 포함하지 않음', 'DB는 읽기 전용 모니터링 경로만 조회', '저장과 실제 연결은 별도 동작')):
        yy = 604 + i * 20
        s.base.append(f'<path d="M821,{yy - 4} l3.5,3.5 l6.5,-7" fill="none" stroke="{c["green"]}" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/>')
        s.text(s.base, 838, yy, value, 11.5, 'body')

    # caption bar ---------------------------------------------------------------
    s.rect(s.base, 40, 664, 1120, 50, 12, 'panel', 'border')
    s.text(s.base, 1144, 694, '논리 경로 · 실제 속도 아님', 10.5, 'faint', 500, 'end')
    s.base.append(f'<text class="t rm" x="60" y="694" font-size="13" fill="{c["body"]}">① 화면 로드 → ② 화면 갱신(3초 캐시) → ③ 15초 직접 수집 → ④ 단회 티켓 터미널</text>')
    for start, end, color, label, caption in PHASES:
        win = (start + 0.15, end - 0.3) if start else (0.3, end - 0.3)
        s.fx.append(f'<g opacity="0">{shown(win, fade=0.25)}<circle cx="64" cy="689" r="5" fill="{c[color]}"/>'
                    f'<text class="t" x="78" y="694" font-size="13" fill="{c["body"]}"><tspan font-weight="700" fill="{c[color + "_ink"]}">'
                    f'{escape(label)}</tspan>  {escape(caption)}</text></g>')

    title = 'Pulse Ops 트래픽 흐름'
    desc = ('브라우저는 Go 서비스에서 정적 파일과 /api/monitoring 스냅샷을 받고, Go 스케줄러는 15초마다 최대 4개 슬롯으로 등록한 '
            '서버·DB·Redis·HTTP·애플리케이션을 직접 수집해 SQLite에 저장합니다. /api/control/integrations에서 설정한 웹훅·Slack·Discord·Teams에는 '
            '서버에서 판정한 발생·복구 이벤트와 정기 요약을 수신 조건·점검 시간·묶음 설정을 적용한 암호화 대기열로 전송합니다. /api/control/events·mutes·deliveries·views에서 사건 처리, 점검, 전송 이력과 저장된 대시보드를 관리하며, 터미널은 단회 티켓과 WebSocket을 거쳐 SSH PTY로 연결됩니다.')
    style = ('.t{font-family:Pretendard,"Pretendard Variable","Apple SD Gothic Neo","Malgun Gothic","Noto Sans KR","Noto Sans CJK KR",'
             '"Segoe UI",system-ui,sans-serif}.m{font-family:ui-monospace,SFMono-Regular,Consolas,"Liberation Mono",Menlo,monospace}'
             'text{text-rendering:geometricPrecision}.rm{display:none}'
             '@media (prefers-reduced-motion:reduce){.fx{display:none}.rm{display:inline}}')
    return (f'<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 {W} {H}" width="{W}" height="{H}" '
            f'role="img" aria-labelledby="title desc"><title id="title">{escape(title)}</title><desc id="desc">{escape(desc)}</desc>'
            f'<style>{style}</style><defs>{"".join(s.defs)}</defs>{"".join(s.base)}<g class="fx">{"".join(s.fx)}</g></svg>\n')


if __name__ == '__main__':
    OUT.mkdir(parents=True, exist_ok=True)
    for theme, name in (('light', 'architecture-flow.svg'), ('dark', 'architecture-flow-dark.svg')):
        (OUT / name).write_text(build(theme), encoding='utf-8')
    print(f'Wrote {OUT / "architecture-flow.svg"} and {OUT / "architecture-flow-dark.svg"} ({num(T)}s loop).')
