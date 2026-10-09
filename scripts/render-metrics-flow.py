"""Render the README's per-application /metrics flow (stdlib, shared SVG style)."""
from pathlib import Path
from xml.sax.saxutils import escape
import runpy

ROOT = Path(__file__).resolve().parents[1]
BASE = runpy.run_path(str(ROOT / 'scripts' / 'render-architecture.py'))
Svg, THEMES, shown, anim, T = (BASE[name] for name in ('Svg', 'THEMES', 'shown', 'anim', 'T'))
W, H = 1200, 690
PHASES = ((.3, 6, 'violet', '① 정상 요청'), (6.4, 12, 'red', '② 5xx 집계'),
          (12.4, 20, 'blue', '③ /metrics 수집'), (20.4, 26.5, 'green', '④ 지표 계산'))


def box(s, geometry, title, subtitle, mono=False):
    x, y, w, h = geometry
    s.rect(s.base, x, y, w, h)
    s.text(s.base, x + 16, y + 25, title, 14, 'text', 700, mono=mono)
    s.text(s.base, x + 16, y + 48, subtitle, 12, 'muted')


def build(theme):
    s, c = Svg(THEMES[theme]), THEMES[theme]
    for name in ('lane', 'violet', 'red', 'blue', 'green'):
        s.defs.append(f'<marker id="ah-{name}" viewBox="0 0 10 10" refX="8.5" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M1,1 L9,5 L1,9 z" fill="{c[name]}"/></marker>')
    s.rect(s.base, .5, .5, W - 1, H - 1, 16, 'bg', 'edge')
    s.text(s.base, 36, 36, 'PULSE / OPS · 애플리케이션 계측', 12, 'violet_ink', 700)
    s.text(s.base, 36, 68, '요청을 한 번 집계하고, 각 인스턴스의 /metrics로 제공합니다', 22, 'text', 700)
    for index, (start, end, color, label) in enumerate(PHASES):
        x = 36 + 285 * index
        s.rect(s.base, x, 86, 273, 34, 9, 'panel', 'border')
        s.text(s.base, x + 16, 108, label, 13, 'muted', 600)
        s.fx.append(f'<g opacity="0">{shown((start, end))}<rect x="{x}" y="86" width="273" height="34" rx="9" fill="{c[color]}" fill-opacity="{c["tint"]}" stroke="{c[color]}"/><text class="t" x="{x+16}" y="108" font-size="13" font-weight="700" fill="{c[color]}">{escape(label)}</text></g>')

    user, middleware, api = (36, 187, 148, 107), (258, 202, 166, 68), (476, 202, 170, 68)
    app_a, endpoint_a = (238, 152, 430, 238), (258, 299, 388, 72)
    app_b, endpoint_b = (238, 426, 430, 112), (258, 474, 388, 46)
    collector, calculate, chart = (750, 152, 414, 386), (772, 304, 370, 79), (772, 402, 370, 114)
    box(s, user, '사용자 요청', '업무 API 호출')
    s.rect(s.base, *app_a, 12, 'panel', 'border')
    s.text(s.base, 258, 179, 'App A', 14, 'text', 700)
    s.text(s.base, 646, 179, '서버 A의 프로세스', 11, 'muted', anchor='end')
    box(s, middleware, '공통 미들웨어', '모든 업무 요청 집계')
    box(s, api, 'API · 서비스', '최종 응답 코드 반환')
    box(s, endpoint_a, 'GET /metrics', 'Counter · Histogram · Gauge', mono=True)
    s.rect(s.base, *app_b, 12, 'panel', 'border')
    s.text(s.base, 258, 453, 'App B', 14, 'text', 700)
    s.text(s.base, 646, 453, '같은 코드 · 별도 집계', 12, 'muted', anchor='end')
    s.rect(s.base, *endpoint_b)
    s.text(s.base, 274, 503, 'GET /metrics', 14, 'text', 700, mono=True)
    s.text(s.base, 630, 503, '서버 B의 누적값', 12, 'muted', anchor='end')
    box(s, (36, 426, 148, 112), '/health', '해당 경로의 응답 확인', mono=True)
    s.text(s.base, 52, 508, '전체 오류율과 별개', 12, 'muted')
    s.rect(s.base, *collector, 12, 'panel', 'border')
    s.text(s.base, 772, 180, 'PULSE / OPS 수집기', 15, 'text', 700)
    s.text(s.base, 772, 215, '15초마다 각 /metrics 읽기', 14, 'blue_ink', 600)
    s.text(s.base, 772, 242, 'App A → 등록 ID A', 12, 'body')
    s.text(s.base, 772, 265, 'App B → 등록 ID B', 12, 'body')
    box(s, calculate, '연속 5분의 증가량으로 계산', '요청량 · 오류율 · P99')
    s.text(s.base, 788, 370, '5xx 증가량 ÷ 전체 요청 증가량 × 100', 12, 'body')
    box(s, chart, '같은 지표의 그래프', 'App A · App B는 각각 별도 선')
    for yy in (463, 484, 505):
        s.base.append(f'<path d="M791,{yy} H1120" stroke="{c["border"]}" fill="none"/>')
    for color, d in (('violet', 'M791,488 L840,470 L890,477 L940,458 L990,469 L1040,460 L1120,470'),
                     ('green', 'M791,501 L840,498 L890,502 L940,491 L990,495 L1040,498 L1120,493')):
        s.base.append(f'<path d="{d}" fill="none" stroke="{c[color]}" stroke-width="2" opacity=".35"/>')
        s.fx.append(f'<path d="{d}" fill="none" stroke="{c[color]}" stroke-width="2.5" pathLength="1" stroke-dasharray="1" stroke-dashoffset="1">{anim("stroke-dashoffset", [0,20.4,23,T], [1,1,0,0])}{shown((20.4,26.5))}</path>')

    s.lane('request', 'M184,217 H257')
    s.lane('dispatch', 'M424,217 H475')
    s.lane('response', 'M475,254 H425')
    s.lane('return', 'M257,254 H185')
    s.lane('count', 'M340,270 V298')
    s.lane('a-get', 'M750,230 H703 V314 H647')
    s.lane('a-text', 'M647,351 H726 V278 H750')
    s.lane('b-get', 'M750,230 H689 V486 H647')
    s.lane('b-text', 'M647,507 H713 V290 H750')
    s.lane('compute', 'M955,279 V303')
    s.lane('draw', 'M955,383 V401')
    for color, at, code in (('violet', .7, '200'), ('red', 6.7, '500')):
        s.glow_block(middleware, color, [(at, at+4.5)])
        s.glow_block(api, color, [(at+1.2, at+3)])
        s.glow_block(endpoint_a, color, [(at+3.4, at+5)])
        for path, begin, end, chip in (('request',at,at+.9,None), ('dispatch',at+1,at+1.8,None),
                                       ('response',at+2.1,at+2.7,code), ('return',at+2.9,at+3.6,code),
                                       ('count',at+3.3,at+4,None)):
            s.packet(path, begin, end, color, chip=chip, trail=False)
        message = '전체 요청 +1 · 응답시간 기록' if code == '200' else '전체 요청 +1 · 5xx +1 · 응답시간 기록'
        s.fx.append(f'<text class="t" x="260" y="411" font-size="12" fill="{c[color]}" opacity="0">{shown((at+3.6,at+5))}{escape(message)}</text>')
    for path, begin, end in (('a-get',12.8,14), ('a-text',14.3,15.5), ('b-get',16,17.2), ('b-text',17.5,18.7)):
        s.packet(path, begin, end, 'blue', chip='GET' if path.endswith('get') else 'text', trail=False)
    for endpoint, start, end in ((endpoint_a,12.8,15.5),(endpoint_b,16,18.7)):
        s.glow_block(endpoint, 'blue', [(start,end)])
    s.glow_block(calculate, 'green', [(20.4,23.5)])
    s.packet('compute',20.6,21.2,'green',trail=False)
    s.packet('draw',22,22.6,'green',trail=False)

    s.rect(s.base, 36, 563, 1128, 56, 10, 'panel', 'border')
    s.text(s.base, 54, 587, '5분 예시', 12, 'text', 700)
    s.text(s.base, 155, 587, 'A: 1,000건 중 5xx 20건 → 2%     B: 400건 중 5xx 2건 → 0.5%', 13, 'body')
    s.text(s.base, 155, 605, '전체 오류율에는 API 목록이 필요하지 않습니다. 경로별 분석에는 route 라벨을 추가합니다.', 12, 'muted')
    s.text(s.base, 36, 648, '요청 처리 → 응답 코드·시간 집계 → 각 /metrics 수집 → 인스턴스별 그래프', 13, 'body', 600)
    s.text(s.base, 1164, 673, '수치·속도는 설명용 예시 · 동작 줄이기 지원', 11, 'muted', anchor='end')
    style = '.t{font-family:Pretendard,"Malgun Gothic","Noto Sans CJK KR",system-ui,sans-serif}.m{font-family:ui-monospace,Consolas,monospace}text{text-rendering:geometricPrecision}@media(prefers-reduced-motion:reduce){.fx{display:none}}'
    title = '애플리케이션별 /metrics 구현과 수집 구조'
    desc = '공통 미들웨어가 실제 업무 요청의 응답 코드와 시간을 집계합니다. 각 애플리케이션 인스턴스가 자신의 /metrics를 제공하고 Pulse Ops가 15초마다 읽어 연속 5분 증가량으로 요청량, 5xx 오류율, P99를 계산합니다. /health는 전체 오류율과 별개입니다.'
    return f'<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 {W} {H}" width="{W}" height="{H}" role="img" aria-labelledby="title desc"><title id="title">{escape(title)}</title><desc id="desc">{escape(desc)}</desc><style>{style}</style><defs>{"".join(s.defs)}</defs>{"".join(s.base)}<g class="fx">{"".join(s.fx)}</g></svg>\n'


if __name__ == '__main__':
    output = ROOT / 'docs' / 'images'
    output.mkdir(parents=True, exist_ok=True)
    for theme, name in (('light','application-metrics-flow.svg'),('dark','application-metrics-flow-dark.svg')):
        (output / name).write_text(build(theme), encoding='utf-8')
    print('Wrote application-metrics-flow.svg and application-metrics-flow-dark.svg.')
