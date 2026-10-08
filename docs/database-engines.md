# 데이터베이스 연결과 수집 범위

인프라 등록에서 PostgreSQL, MySQL, MariaDB, Oracle을 각각 선택합니다. 서버 주소·포트·계정·비밀번호·DB 이름과 SSH 경유 서버를 등록하며, 초안은 빈 값으로 저장할 수 있습니다. Oracle은 Service name 또는 SID를 선택하고 연결 시작 전에 해당 값을 입력합니다. 기본 포트는 PostgreSQL 5432, MySQL/MariaDB 3306, Oracle 1521입니다. Redis는 별도 캐시 종류입니다.

TLS 기본값은 인증서·호스트 검증입니다. 사설 CA는 관리 서비스의 신뢰 저장소에 설치해야 합니다. 인증서를 무시하는 모드는 제공하지 않습니다. 테스트 Compose는 격리 네트워크에서 명시적으로 TLS를 끄며 DB 포트를 호스트에 공개하지 않습니다. Oracle Wallet/mTLS, OS 인증, RAC 전체 인스턴스 집계는 이번 지원 범위에 포함되지 않습니다.

## 지표의 범위

| 엔진 | 범위 | 직접 수집하는 지표 |
| --- | --- | --- |
| PostgreSQL | 등록 DB | 읽기 응답 시간, 연결, 커밋/롤백, shared buffer, 배타 잠금, 데드락, 복제 지연 |
| MySQL / MariaDB | 서버 전체 | 읽기 응답 시간, 전체/실행 중 연결, 연결 한도 사용률, 클라이언트 명령 처리량, 느린 명령, InnoDB 버퍼 적중률·행 잠금 대기, DB 송수신량, 가동 시간 |
| Oracle | 접속한 서비스/컨테이너에서 보이는 로컬 인스턴스 통계 | 읽기 응답 시간, 사용자/활성/차단 세션, 세션 한도(제공되는 경우), 실행량, 커밋/롤백, 버퍼 적중률, SQL*Net 송수신량 |

MySQL/MariaDB에서 Database를 지정해도 GLOBAL 통계가 해당 DB로 제한되지는 않습니다. Oracle PDB에서 `v$resource_limit`의 sessions 행을 제공하지 않으면 한도와 사용률을 미관측으로 둡니다. root의 값을 PDB 한도인 것처럼 대체하지 않습니다. Oracle 세션 한도 사용률은 배경 세션을 포함한 `current_utilization`을 분자로 사용하며 사용자 세션 수와 다릅니다.

변화율은 수집 간격의 카운터 차이로 계산합니다. 첫 표본·카운터 초기화·읽기 0건에서는 계산할 수 없는 비율을 미관측으로 둡니다. MySQL의 Questions를 트랜잭션 수로 대체하지 않습니다. 간단한 읽기 왕복 시간은 업무 쿼리 P99가 아닙니다. SQL 본문과 업무 테이블 행은 수집하지 않습니다.

새 규칙은 읽기 응답 지연(200ms 초과), 연결·세션 한도 접근(80% 초과), 잠금 대기 지속(0 초과), 느린 명령 증가(1회/s 초과), 기본 통계 수집 실패입니다. 각 조건은 1분 지속되어야 발생합니다. 이벤트와 규칙 설명 모두 관련 DB 응답·활성 연결·잠금 그래프를 함께 제공합니다.

## 읽기 권한

MySQL/MariaDB는 `SELECT 1`, 선택한 GLOBAL STATUS와 GLOBAL VARIABLES만 조회합니다. 전체 관리 권한이나 데이터 변경 권한은 필요하지 않습니다. Database를 지정하면 해당 DB에 접속할 권한이 필요합니다. 테스트 계정에는 빈 테스트 DB의 SELECT만 부여합니다.

Oracle은 관리자와 협의해 모니터링 계정에 `CREATE SESSION`, `SYS.V_$SYSSTAT`, `SYS.V_$SESSION`, `SYS.V_$RESOURCE_LIMIT`에 대한 개별 SELECT를 부여합니다. DBA/SELECT ANY DICTIONARY 같은 광범위한 역할을 전제로 하지 않습니다. AWR·ASH·DBA_HIST 뷰를 호출하지 않습니다. 접속은 성공했지만 일부 통계 조회가 실패하면 연결을 정상으로 유지하고 `DB 통계 수집 상태=0`과 안내 문구/이벤트를 표시합니다. 조회 실패 지표를 0으로 채우지 않습니다.

수집은 연결 1개와 제한된 통계 조회를 사용합니다. 연결 8초, 개별 조회 3초, 전체 수집의 상위 제한과 동시 수집 4개를 적용합니다. SSH 경유에서도 연결 취소와 읽기/쓰기 시간 제한을 적용합니다.

## 격리 테스트 환경

```sh
docker compose -f compose.test.yml -f compose.test.databases.yml --profile dashboard up -d --build
```

기본 8개 대상에 MySQL, MariaDB, Oracle이 추가 등록됩니다. 확장 테스트 스택은 Oracle을 포함해 약 3.1GiB의 추가 컨테이너 메모리 상한을 사용합니다. 운영 Compose에는 이 DB 컨테이너나 자동 등록 설정을 포함하지 않습니다. 테스트 계정은 제품의 운영 기본값이 아닙니다.

```sh
cd control-plane
CGO_ENABLED=0 go test -c -o ../.local/control-databases.test
cd ..
docker run --rm --network pulse-ops-test_default \
  -e PULSE_TEST_DATABASES=true \
  -v "$PWD/.local/control-databases.test:/tests:ro" \
  -v pulse-ops-test_test-ssh:/test-ssh:ro \
  alpine:3.23 /tests -test.run=TestIntegrationDatabaseEngines -test.v
```

기본 테스트 Compose만으로 PostgreSQL·Redis의 직접/SSH bastion 경유 수집을 확인하려면 같은 명령에서 `-e PULSE_TEST_SSH_JUMP=true`와 `-test.run=TestIntegrationJumpPostgresRedis`를 사용합니다.

테스트는 세 엔진의 직접 연결/SSH 경유 수집, 잘못된 비밀번호 거부, 검증되지 않은 TLS 거부, Oracle SID, Oracle 권한 부족 상태를 검증합니다. 운영용 인증서로 성공하는 TLS/mTLS, Oracle RAC, 모든 과거 버전의 호환성을 검증한 것은 아닙니다.

## 구현 참고

- [Go MySQL Driver](https://github.com/go-sql-driver/mysql): MySQL/MariaDB 연결, TLS 및 개별 DialFunc.
- [go-ora](https://github.com/sijms/go-ora): Go Oracle 연결, Service name/SID와 커넥터.
- [Oracle V$RESOURCE_LIMIT](https://docs.oracle.com/en/database/oracle/oracle-database/21/refrn/V-RESOURCE_LIMIT.html): 리소스 사용량, 한도와 컨테이너 범위.
- [Oracle Free 테스트 이미지](https://github.com/gvenzl/oci-oracle-free): 격리 테스트 DB 초기화.
