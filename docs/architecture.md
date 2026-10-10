# 구조와 배치 예시

[README](../README.md)

## 구조

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/architecture-flow-dark.svg">
  <img alt="Pulse Ops 트래픽 흐름: 브라우저가 Go 서비스에서 정적 파일과 /api/monitoring 스냅샷을 받고, Go 스케줄러가 15초마다 슬롯 4개로 등록 인프라를 직접 수집해 SQLite에 저장하며, 터미널은 단회 티켓과 WebSocket을 거쳐 SSH PTY로 연결됩니다." src="images/architecture-flow.svg" width="100%">
</picture>

애니메이션은 네 경로를 한 장면씩 보여 줍니다. 실제로는 각 경로가 서로 독립적으로 동시에 동작하고, 점의 속도는 실제 지연이나 처리량이 아닙니다. 운영체제의 ‘동작 줄이기’ 설정을 켜면 정지된 구조도만 표시합니다.

1. **화면 로드**: 브라우저가 Go에서 HTML, CSS, 네이티브 ES 모듈을 받습니다. 파일은 실행 파일에 포함되며 gzip·ETag로 전달합니다.
2. **화면 갱신**: 브라우저 Worker가 같은 출처의 `/api/monitoring`에서 관측값을 읽고 이벤트 규칙을 평가합니다. 3초 안에 들어온 같은 범위·대상 조회는 공유 캐시로 응답하며, 화면 갱신은 수집을 호출하지 않습니다. 화면은 Canvas로 필요한 그래프를 그립니다.
3. **수집**: Go 수집기는 화면 요청과 독립적으로 15초마다 실행합니다. 동시 수집은 최대 4개이고 등록 ID마다 하나씩만 진행합니다. 슬롯이 모두 차면 다음 대상은 빈 슬롯을 기다립니다. 등록한 SSH·DB·Redis·HTTP·애플리케이션에 직접 접근하고 결과를 SQLite에 저장합니다.
4. **터미널**: 터미널을 열 때만 내장된 xterm 파일을 읽습니다. 30초 단회 티켓 → WebSocket → Go SSH PTY 경로로 실제 셸을 연결합니다. 감사 기록에는 연결·종료만 남기고 입력·출력 본문은 보관하지 않습니다.
5. **저장**: 등록 정보는 AES-256-GCM으로 암호화하고 관측·감사 기록은 같은 SQLite 볼륨에 저장합니다. 관측 보존은 15일, 감사 보존은 90일입니다.

HTTPS는 Go의 인증서 설정 또는 이미 운영하는 HTTPS 앞단을 사용합니다. 애플리케이션 자체에 별도 Nginx·Node 컨테이너는 필요하지 않습니다. Prometheus/Grafana는 선택적 분석 도구이며 인프라 등록이나 기본 동작의 필수 요소가 아닙니다.

## Cloudflare DNS·프록시 구성 예시

공인 IP가 있는 서버에서 Cloudflare DNS·프록시와 Nginx를 사용하는 **배치 예시**입니다. `app.example.com`은 프런트와 `/api`, `ops.example.com`은 Pulse Ops 관리 화면으로 연결합니다. DB·Redis·각 백엔드의 `/metrics`는 내부망에서 직접 수집합니다.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/cloudflare-traffic-flow-dark.svg">
  <img alt="서비스 요청 3단계: 접속·캐시, API·DB 처리, 500 오류 응답. 모든 수치는 가상 예시값입니다." src="images/cloudflare-traffic-flow.svg" width="100%">
</picture>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="images/pulse-observation-flow-dark.svg">
  <img alt="운영 관측 2단계: 지표 수집과 통합 화면 조회. Cloudflare API 연결은 연동 예정입니다." src="images/pulse-observation-flow.svg" width="100%">
</picture>

두 영상은 각각 20초로 반복합니다. **서비스 요청**은 접속·캐시 → API·DB → 오류 응답의 3단계, **운영 관측**은 지표 수집 → 통합 화면의 2단계입니다. 수치·속도는 설명용 가상값이며, ‘동작 줄이기’ 설정에서는 정지 화면을 표시합니다.

| 가상의 5분 표본 | 예시값 | 관계 |
| --- | --- | --- |
| Cloudflare 서비스(app) 외부 요청 | 10,000건 | CDN HIT 6,000건 + 원본 전달 4,000건 |
| App A | 요청 2,500건 · 5xx 10건 · P99 240ms | 오류율 0.40% |
| App B | 요청 1,500건 · 5xx 5건 · P99 180ms | 오류율 약 0.33% |
| 내부 인프라 | 서버 CPU 38% · RAM 62% · DB 연결 24개 · Redis HIT 96% | 각 대상에서 별도로 얻는 관측값 |

트래픽 표본은 서비스 도메인(app)만 집계하며 관리 도메인(ops)은 별도입니다. 외부 요청 10,000건과 원본 요청 4,000건을 더하지 않습니다. 원본 요청 4,000건은 A 2,500건 + B 1,500건이며, CDN에서 끝난 6,000건은 백엔드에 도착하지 않습니다. 앱 요청의 Redis MISS는 경로 설명을 위한 한 건이고, Redis HIT 96%는 별도 명령 집계 예시입니다. P99는 인스턴스별 Histogram에서 얻는 예시값이며 서로 평균 내지 않습니다.

**현재 지원:** 인스턴스별 애플리케이션 `/metrics`, DB·Redis 직접 조회, 서버 SSH 수집과 SQLite 저장. 앱의 요청량·오류율·P99는 연속 5분 표본이 쌓여야 표시합니다. 그림의 수집기는 최대 4개를 동시에 조회하고 첫 슬롯이 비면 다섯 번째 대상인 서버 SSH를 조회합니다.

**추가 구현 범위:** 그림의 주황 점선 **Cloudflare Analytics API** 연결과 외부 트래픽 카드는 연동 예정인 설계입니다. Cloudflare·Nginx 원문 로그를 수집·검색하는 기능도 현재 지원 범위에 포함되지 않습니다. Cloudflare API의 조회 항목·기간은 [요금제와 데이터셋](https://developers.cloudflare.com/analytics/graphql-api/limits/)에 따라 달라집니다.

Cloudflare DNS의 [프록시를 켜야](https://developers.cloudflare.com/dns/proxy-status/) 웹 요청이 Cloudflare를 통과합니다. DNS only는 주소 조회만 제공합니다. 이 예시에서는 원본 인증서와 [Full (strict)](https://developers.cloudflare.com/ssl/origin-configuration/ssl-modes/full-strict/)를 사용하고, API·관리 화면은 캐시를 우회합니다. 관리용 Nginx는 외부 Host·Origin과 WebSocket Upgrade를 전달하며, Pulse Ops 운영 계정으로 로그인합니다.
