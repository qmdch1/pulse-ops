# PULSE / OPS

서버의 현재 문제와 위험 징후를 지표 옆에서 조사하는 모니터링 웹 프로젝트입니다. 상단 이벤트, 하단 7개 지표 구역, 사이드바 이동으로 구성합니다.

## 실행

Node.js 22.20+, Python 3.12+, Docker Compose가 필요합니다.

```sh
python scripts/discover-ssh.py
docker compose -f compose.test.yml --profile dashboard up -d --build
```

- 대시보드: http://localhost:13000
- Grafana: http://localhost:13001/d/pulse-ops (테스트 계정 `pulse` / `pulse_test_only`)
- Prometheus: http://localhost:19090
- 테스트 프런트: http://localhost:18480

기본 애플리케이션 서버는 3대입니다. 아래 명령으로 변경할 수 있습니다.

```sh
docker compose -f compose.test.yml up -d --scale demo-api=5
```

테스트 종료는 `docker compose -f compose.test.yml --profile dashboard stop`입니다. 볼륨은 보존합니다. 최초 시작 시 변화율에는 2개 이상 표본, 지속 경보에는 해당 지속 시간이 필요합니다.

개발 화면은 `.env.example`을 `.env`로 복사하고 `npm ci`, `npm run dev -- --port 4317`로 실행합니다. 모든 값을 실제 Prometheus에서 읽으며, 임의의 운영 수치로 대체하지 않습니다.

## 구성

- Next.js / React / TypeScript, Recharts, 접근성 UI primitives
- Next standalone Docker 이미지 및 private Sites Worker 빌드
- 97개 PromQL 지표 계약, 의미·단위·수집기·검토 기준·NULL 상태
- P50/P95/P97/P99/P99.9, API별/버전별 히스토그램, 1주/4주 비교
- 지속 조건과 복합 근거를 사용하는 이벤트 규칙, 만료·용량 위험, 실제 ALERTS 발생·해제 이력
- 이벤트 조사 패널: 당시 스냅샷, 근거, 점검 절차, 그래프, JSON 내보내기
- 실제 Docker 테스트: Python API 3대 → PostgreSQL / Redis, Nginx 프런트, 제한된 테스트 부하
- Prometheus / Alertmanager / Grafana / HTTP probe / DB·캐시·프런트 exporter
- 브라우저 자동 갱신 30초, 조회 동시성 제한, 동일 조회 캐시, 요청·응답 크기 제한

## 운영 전환

`compose.prod.yml`은 운영 대시보드만 실행합니다. 테스트 구성과 합쳐 실행하지 않습니다.

1. `.env.prod.example`을 `.env.prod`로 복사합니다.
2. 승인된 Prometheus·Grafana 주소와 읽기 토큰을 설정합니다.
3. `DASHBOARD_USERNAME`과 **24자 이상 고유 비밀번호**를 설정합니다. 운영 모드에서 누락되면 접근을 차단합니다.
4. `docker compose --env-file .env.prod -f compose.prod.yml up -d --build`를 실행합니다.
5. 127.0.0.1 바인딩 앞에 HTTPS 프록시를 둡니다. 인터넷에 평문 Basic 인증을 노출하지 않습니다.

DB 주소를 연결할 때는 `compose.collectors.prod.yml`의 PostgreSQL/Redis 모니터링 계정 설정을 사용하고, exporter를 승인된 Prometheus에 등록합니다. DB를 대시보드 브라우저에서 직접 조회하지 않습니다. 임의 주소/PromQL을 API 파라미터로 받지 않습니다. 공급자별 이름은 `lib/monitoring/catalog.ts`의 계약 또는 recording rule에 맞춰야 합니다. 서버 주소·버전·방화벽을 받은 뒤 실제 exporter 접근을 검증해야 합니다.

운영 설정 예시는 `monitoring/prometheus/prometheus.prod.example.yml`에 있습니다. 실제 운영 서버에 배포·접속하지 않았습니다. 운영 이미지는 배포 시점의 취약점 검사와 지원 버전 검토가 필요합니다. 현재 제품은 단일 운영 워크스페이스와 단일 계정 접근 제어를 제공합니다. 고객별 데이터 격리, 조직 SSO/RBAC, 장기 감사 저장소와 과금은 구현 범위에 포함되지 않습니다. 판매 전 승인 기준은 `docs/production-readiness.md`에 구분했습니다.

## SSH 발견

Windows `%USERPROFILE%/.ssh/config`, Linux/macOS `~/.ssh/config`를 읽습니다. 다른 OS의 설정은 그 OS에서 실행하거나 `--config PATH`로 추가합니다. 로컬에 없는 다른 컴퓨터의 macOS 설정을 원격 탐색하지 않습니다.

Host/HostName/User/Port/ProxyJump 및 Include 메타데이터만 파싱합니다. 키·비밀번호를 읽거나 Match exec/ProxyCommand를 실행하지 않습니다. wildcard 호스트는 자동 연결 후보로 만들지 않습니다. 선택은 브라우저의 로컬 환경설정이며 SSH 접속 동의나 설치 작업이 아닙니다.

결과 `.local/ssh-inventory.json`은 Git, Docker 빌드 컨텍스트 및 클라우드 배포에서 제외합니다. 수동으로 실행하는 탐색 명령 외에는 SSH 설정 파일에 접근하지 않습니다.

## 검증

```sh
node --experimental-strip-types --test tests/monitoring.test.ts
python tests/ssh-discovery.test.py
npx tsc --noEmit
python scripts/verify-live.py
```

`python scripts/test-failure-recovery.py`는 **이 프로젝트 테스트 Redis만 155초 중단한 뒤 반드시 재시작**하는 장애 훈련입니다. 운영에서는 실행하지 않습니다. 실제 실패율·Prometheus 경보를 검증합니다. 테스트 결과와 로그는 `.local/`에 저장하며 Git에서 제외합니다.

Grafana 패널 재생성: `node --experimental-strip-types scripts/generate-grafana.ts`.

## 데이터 의미

- `missing`: 표본 없음, 무요청 백분위, 미설치 계측 또는 부족한 과거 이력
- `stale`: 최근 표본 부족. 최신 값은 표시하지 않습니다.
- `error`: 조회 실패. 정상 0으로 처리하지 않습니다.
- 예측은 추세 기반 점검 후보입니다. 디스크 외삽에는 6시간 전 표본과 15분 지속 조건이 필요합니다.
- 쿠키는 서버 측 만료 시각·보안 속성만 수집합니다. 쿠키 값/세션 ID/토큰 본문은 수집하지 않습니다. 명시 만료가 없는 쿠키는 미관측입니다.
- 예시 SLO 99.9%와 경계값은 제품 기본값입니다. 운영 계약에 맞춰 리뷰하고 조정해야 합니다.
- 테스트 보존은 35일 또는 1GB 중 먼저 도달한 제한입니다. 새 환경에는 1주·4주 비교 데이터가 없습니다.

설계 범위 대응표: `docs/coverage.md`.

참고: [서버 모니터링 분석 가이드](https://kciter.so/posts/server-monitoring-analysis-guide/), [Prometheus histograms](https://prometheus.io/docs/practices/histograms/), [Google SRE monitoring](https://sre.google/sre-book/monitoring-distributed-systems/), [Set-Cookie](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Set-Cookie).
