# 설치·운영·개발

[README](../README.md)

명령은 저장소 루트에서 실행합니다.

## 테스트 환경 실행

Docker Compose만 있으면 빌드·실행할 수 있습니다. 아래 `.local` 폴더는 선택적인 SSH 후보 파일을 위한 로컬 경로입니다.

```sh
mkdir -p .local/ssh
docker compose -f compose.test.yml --profile dashboard up -d --build
```

대시보드: [localhost:13000](http://localhost:13000) · 테스트 프런트: [localhost:18480](http://localhost:18480)

API 서버 3대, PostgreSQL, Redis, HTTP 프런트, SSH bastion과 node를 등록합니다. 테스트 비밀번호/키는 격리된 테스트 대상 전용입니다. DB와 SSH 포트는 호스트에 공개하지 않습니다.

MySQL·MariaDB·Oracle까지 검증하는 확장 구성:

```sh
docker compose -f compose.test.yml -f compose.test.databases.yml --profile dashboard up -d --build
```

이 구성은 11개 인프라를 등록합니다. [DB별 연결·권한·지표 범위](database-engines.md)를 확인하세요. Oracle 테스트 컨테이너에는 약 2.3GiB의 메모리 한도가 별도로 필요합니다.

Prometheus·Grafana·exporter는 필요할 때만 추가합니다.

```sh
docker compose -f compose.test.yml --profile observability up -d
```

Prometheus: [localhost:19090](http://localhost:19090) · Grafana: [localhost:13001](http://localhost:13001), 테스트 계정 `pulse` / `pulse_test_only`.

애플리케이션 변화율과 P50/P95/P97/P99/P99.9는 연속 5분 표본이 쌓인 후 계산합니다. 한 시간 SLO는 한 시간 표본이 필요합니다. 재시작·수집 공백·카운터 리셋 후에는 충분한 표본이 다시 확보될 때까지 미관측입니다. 테스트 API 수는 `--scale backend=5`로 변경한 뒤 `control-plane`을 재시작해 발견할 수 있습니다. 이전 주소나 사용자가 편집한 등록은 자동으로 삭제·덮어쓰지 않습니다.

## 운영 배포

`compose.prod.yml`은 **Go 서비스 하나와 registry 볼륨**만 구성합니다. 테스트 대상·초기 등록·브라우저 테스트 페이지는 운영 바이너리에 포함되지 않습니다.

1. `.env.prod.example`을 `.env.prod`로 복사합니다.
2. 운영 username, 24자 이상 비밀번호, 32바이트 난수의 base64 암호화 키, 정확한 HTTPS origin을 설정합니다.
3. 아래 명령으로 실행합니다. 지정한 서버 주소는 이후 인프라 등록 화면에서 입력합니다.

```sh
docker compose --env-file .env.prod -f compose.prod.yml up -d --build
```

기본 포트는 `127.0.0.1:13000`입니다. 기존 HTTPS 앞단은 외부 **Host와 Origin을 유지**하고 WebSocket Upgrade를 전달해야 합니다. Go가 직접 HTTPS를 제공하려면 인증서 파일을 읽기 전용으로 마운트하고 `PULSE_TLS_CERT`, `PULSE_TLS_KEY`, 리스닝 주소/포트를 함께 지정합니다. 운영에는 자동 등록이 없습니다.

기존 v2에서 전환할 때 SQLite 볼륨과 `CONTROL_MASTER_KEY`를 그대로 보존합니다. 이전 gateway와 dashboard 서비스만 중지·제거하고 새 Go 서비스를 시작합니다. `down -v`를 사용하지 마세요. 별도 `CONTROL_API_TOKEN`은 서비스 간 프록시가 사라져 더 이상 필요하지 않습니다.

암호화 키는 DB 백업과 별도로 보관하세요. 현재는 단일 워크스페이스·단일 운영 계정·단일 SQLite 인스턴스입니다. 조직 SSO/RBAC, 다중 고객 격리, HA와 장기 사건 이력은 추가 구현 범위입니다. [검증 범위와 운영 과제](production-readiness.md)를 참고하세요.

## 개발과 검증

Go 1.27 이상을 사용합니다. 프런트 파일은 `control-plane/web/`, 수집·등록·터미널은 `control-plane/*.go`에 있습니다. **프런트 설치나 빌드 단계는 없습니다.** xterm 6.0.0과 addon-fit 0.11.0의 배포용 ES 모듈만 MIT 라이선스와 SHA-256 목록을 함께 보관합니다.

```sh
sh scripts/build-verified.sh
# 실행 예시: 빈 로컬 등록 저장소, 자동 대상 등록 없음
MONITORING_MODE=test CONTROL_LISTEN=127.0.0.1:13000 \
CONTROL_DATA_DIR=.local/data \
CONTROL_ALLOWED_ORIGINS=http://localhost:13000,http://127.0.0.1:13000 \
.local/bin/pulse-ops
```

Go 파일과 정적 파일을 수정한 뒤 Go를 다시 빌드하면 됩니다. 실행 파일에는 정적 파일이 포함되어 다른 작업 폴더에서도 실행할 수 있습니다. 기본 실행은 운영 설정이 없으면 시작을 거부합니다. `MONITORING_MODE=test`는 운영자 인증을 끄므로 루프백 `CONTROL_LISTEN`(또는 테스트 Compose의 `testseed` 빌드)에서만 시작하고, `CONTROL_ALLOWED_ORIGINS`의 Host로 들어온 요청만 처리합니다.

```sh
cd control-plane
go test -race ./...
go vet ./...
cd ..
python tests/ssh-discovery.test.py
python scripts/verify-live.py
```

테스트 Compose의 [브라우저 회귀 검사](http://localhost:13000/__tests__/)는 일반 브라우저에서 순수 JS 지표·규칙·차트 상태 검사를 실행합니다. 운영에는 이 경로가 없습니다. Go 통합 검사는 격리 Compose 내부에서만 실행합니다. `PULSE_TEST_CONTROL=http://127.0.0.1:7080`, `PULSE_TEST_DATABASES=true`, `PULSE_TEST_SSH_JUMP=true`로 실제 DB/SSH/PTY·키·jump·티켓·리사이즈와 PostgreSQL·Redis의 jump 경유 수집을 검증합니다. [최신 검증 기록](native-web-verification.md)을 확인하세요.

전체 구조도는 `python3 scripts/render-architecture.py`, 애플리케이션 계측 구조도는 `python3 scripts/render-metrics-flow.py`, Cloudflare 구성 예시는 `python3 scripts/render-cloudflare-flow.py`로 다시 생성합니다. 세 스크립트는 표준 라이브러리만 사용하며 각각 라이트·다크 SVG를 `docs/images/`에 씁니다. SVG는 스크립트 없이 SMIL로 움직이므로 README의 `<img>`에서도 재생됩니다. 문서 이미지 생성 도구는 제품의 빌드·실행 의존성이 아닙니다.
