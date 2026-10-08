# PULSE / OPS

직접 등록한 인프라를 기준으로 관리·관측하는 웹 프로젝트입니다. 서버 주소, DB 계정, SSH 비밀번호, PEM/OpenSSH 키, passphrase, 경유 서버와 의존 관계를 입력합니다. 빈 값은 초안으로 저장할 수 있으며, **저장과 실제 연결은 별도 동작**입니다.

대시보드는 이벤트 요약과 등록 인프라 목록을 표시합니다. 인프라별 상세 모달, 실제 SSH 터미널, 이벤트 규칙의 조건·지속 시간·관련 그래프, 환경설정의 세 메뉴로 구성합니다. 같은 단위는 같은 그래프 축을 공유하고 두 단위까지 이중 축으로 비교합니다. 추가 단위는 같은 시간 범위의 별도 그래프로 표시합니다.

## 테스트 실행

Docker Compose가 필요합니다. SSH 후보 탐색은 Python 3.12 이상에서 실행합니다.

```sh
python scripts/discover-ssh.py
docker compose -f compose.test.yml --profile dashboard up -d --build
```

- 대시보드 / 터미널: http://localhost:13000
- 테스트 프런트: http://localhost:18480
- 별도 분석 도구: Prometheus http://localhost:19090, Grafana http://localhost:13001
- Grafana 테스트 계정: `pulse` / `pulse_test_only`

기본 등록 대상은 API 서버 3대, PostgreSQL, Redis, HTTP 프런트, SSH bastion, SSH node입니다. SSH bastion은 테스트 비밀번호, node는 생성된 키와 bastion 경유 연결을 사용합니다. SSH 포트와 DB 포트는 호스트에 공개하지 않습니다. 테스트 자격 증명과 키를 실제 서버에 사용하지 마세요.

호스트/DB/캐시는 약 15초마다 수집합니다. 애플리케이션 요청 변화율·P50/P95/P97/P99/P99.9는 연속 5분 표본이 쌓인 후 계산합니다. 한 시간 SLO 소진율은 한 시간 표본이 필요합니다. 재시작·수집 공백·카운터 리셋 후에는 표본이 다시 확보될 때까지 미관측입니다.

API 수 변경:

```sh
docker compose -f compose.test.yml --profile dashboard up -d --scale demo-api=5
docker compose -f compose.test.yml --profile dashboard restart control-plane
```

테스트 초기 등록은 현재 DNS에서 발견한 API 주소를 각각 등록합니다. 재생성으로 주소가 바뀌거나 축소된 경우 기존 등록을 자동 삭제하지 않습니다. 목록에서 이전 주소의 수집을 정지하거나 삭제하세요. 직접 수정한 연결 정보는 초기 등록으로 덮어쓰지 않습니다. `docker compose -f compose.test.yml --profile dashboard stop`은 데이터를 보존하며 중지합니다.

## 수집 구조

`브라우저 → Next 인증 API → Go 인프라 관리 → 등록 대상`

- Go 1.27: 등록 저장소, AES-256-GCM 인증 정보 암호화, SSH 및 최대 8홉 점프, 고정 호스트 키 검증, PTY/WebSocket 터미널, 수집 스케줄, 관측/감사 저장.
- Linux `/proc`·`df`, macOS `top`·`vm_stat`, Windows SSH의 PowerShell CIM 리소스 조회. 관리자 설치나 원격 임의 스크립트 배포 없이 고정 조회 명령만 사용합니다.
- PostgreSQL: 읽기 전용 세션, `SELECT 1` 왕복, 현재 DB의 통계·잠금·복제 상태. 원본 업무 테이블을 조회하지 않습니다.
- Redis: PING·INFO. HTTP: 등록 URL 응답과 TLS 만료. 애플리케이션: 등록된 `/metrics` 텍스트의 카운터·히스토그램을 직접 계산합니다.
- SQLite: 일반 인덱스만 사용, 관측 15일/감사 90일 보존. 차트는 최대 24시간 조회하며 규칙 평가는 화면 기간과 별도로 최근 1시간의 15초 표본을 사용합니다.
- 106개 지표 정의와 53개 규칙을 유지합니다. 현재 수집 경로에서 제공되지 않는 Kubernetes/JVM/API 라벨별/장기 기준선 등은 미관측입니다. 주소만으로 앱 내부 P99·GC·세션 만료를 추측하지 않습니다.
- Prometheus/Grafana는 별도 분석용 테스트 도구입니다. 이들의 수집 대상 목록으로 인프라 등록을 대체하지 않습니다. 기존 Prometheus 설정과 지표 계약은 선택적 통합 참고 자료로 유지합니다.

같은 등록 ID 안에서만 이벤트 조건을 평가합니다. 사용자가 지정한 DB·캐시·서버 의존 관계는 관련 그래프를 추가하며, 그 값을 다른 서버의 조건 값으로 대신 사용하지 않습니다. 연결/수집/터미널 감사 이력은 영구 저장되지만 화면 상관·예측 이벤트의 발생/해제 이력은 아직 영구 사고 저장소에 기록하지 않습니다.

## 연결 절차

1. 인프라 등록에서 이름/유형/주소를 입력하거나 빈 초안을 저장합니다.
2. SSH는 주소·username과 비밀번호 또는 키를 입력합니다. 서버 지문 조회 후 서버 관리자가 제공한 지문과 확인해 저장합니다. 조회 시 최종 대상 인증은 수행하지 않으며 경유 서버 인증은 필요합니다.
3. 경유 서버가 있으면 별도 서버로 먼저 등록하고 SSH jump에서 선택합니다. DB/HTTP 연결도 해당 경유 서버의 TCP 전달을 사용합니다.
4. 연결 관계에서 사용하는 DB·캐시를 선택합니다. 저장 후 연결 시작을 누릅니다.
5. 서버의 터미널 버튼은 실제 셸을 엽니다. 입력·출력 본문은 감사 저장소에 저장하지 않습니다. 세션은 창 닫기 또는 최대 2시간 후 종료합니다.

수정 시 비밀번호·키 입력을 건드리지 않으면 기존 값을 유지합니다. '저장된 값 삭제'는 명시적으로 비웁니다. 비밀값은 목록 API로 다시 보내지 않습니다. 다른 인프라의 jump/의존 대상으로 사용 중인 등록은 참조를 해제해야 삭제할 수 있습니다.

## 운영 Compose

`compose.prod.yml`은 gateway, dashboard, Go control-plane만 실행합니다. 테스트 서버·부하·테스트 등록 코드는 운영 Go 바이너리에 포함되지 않습니다.

1. `.env.prod.example`을 `.env.prod`로 복사합니다.
2. 운영 계정의 24자 이상 비밀번호, 내부 API의 32자 이상 난수 토큰, 32바이트 난수의 base64 암호화 키, 정확한 HTTPS origin을 설정합니다.
3. `docker compose --env-file .env.prod -f compose.prod.yml up -d --build`로 실행합니다.
4. 루프백 게이트웨이 앞의 HTTPS 프록시에서 **외부 Host와 Origin을 유지**하고 WebSocket Upgrade를 전달합니다. Go API는 외부 공개하지 않습니다.
5. 등록 화면에서 승인된 서버/DB 정보만 입력합니다. 운영에는 자동 등록이 없습니다.

암호화 키는 등록 DB 백업과 별도로 보관하세요. 키를 잃으면 인증 정보를 복구할 수 없습니다. 테스트는 볼륨 안에 생성한 0600 키를 사용하고, 운영은 지정된 `CONTROL_MASTER_KEY`를 요구합니다. 키 교체/조직 SSO/테넌트 분리/고가용성은 별도 운영 과제입니다. [현재 검증 범위](docs/production-readiness.md)를 확인하세요.

## SSH 후보와 GateDock 참고

탐색 스크립트는 Windows `%USERPROFILE%/.ssh/config`, Linux/macOS `~/.ssh/config`, 추가 `--config PATH`의 Host/HostName/User/Port/ProxyJump/Include 메타데이터만 읽습니다. Match exec/ProxyCommand를 실행하지 않으며 private key를 자동으로 읽지 않습니다. 다른 컴퓨터의 SSH 설정을 원격 탐색하지 않습니다. 후보 선택은 등록 양식만 채우며 실제 연결을 시작하지 않습니다. `.local/ssh-inventory.json`은 Git/클라우드 배포에서 제외됩니다.

동일 워크스페이스의 `../GateDock/internal/sshclient/client.go`, `../GateDock/internal/web/terminal.go`, 모델/등록 흐름을 참고했습니다. Go SSH 연결, 호스트 키 검증, 웹 터미널, 후보 탐색 패턴을 적용하고 PULSE의 직접 수집 및 암호화 저장소를 별도로 구현했습니다.

## 개발과 검증

Node.js 22.20+, Go 1.27이 필요합니다. Go 서비스를 개발용으로 실행할 때 `CONTROL_API_TOKEN`과 `CONTROL_ALLOWED_ORIGINS`를 설정하고 Next의 `.env`에 같은 토큰과 서비스 주소를 지정합니다. 터미널 검증은 WebSocket 게이트웨이가 포함된 Compose를 사용하세요.

```sh
npm ci
npx tsc --noEmit
node --experimental-strip-types --test tests/*.test.ts
python tests/ssh-discovery.test.py
cd control-plane
go test -race ./...
go vet ./...
```

격리 Compose 통합 테스트는 `control-plane/control_test.go`의 `TestIntegrationRegisteredTargetsAndTerminal`입니다. 테스트 바이너리를 Compose 내부 네트워크에서 `PULSE_TEST_CONTROL=http://control-plane:7080`으로 실행하면 DB/Redis/HTTP/SSH 관측, 비밀번호/키/점프 터미널, 입력·리사이즈·종료, 단회 티켓, 빈 등록을 검증합니다. 실제 고객 서버에 실행하지 않습니다.

Sites URL은 프런트 배포 사본입니다. 해당 호스팅은 로컬 Go 프로세스·SSH 경로를 실행하지 않습니다. 연결과 터미널의 작동 결과는 로컬 Compose에서 확인합니다. 실제 등록 정보나 키·로컬 관측 데이터는 클라우드 소스 패키지에 포함하지 않습니다.
