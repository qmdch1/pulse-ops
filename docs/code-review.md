# 개선 과제

[README](../README.md)

## 개선 과제

2026-10-08 커밋 `3606c39`의 코드 검토 결과입니다. Go 소스와 같은 버전의 의존 라이브러리 소스를 읽고 확인했습니다. 실행 재현과 부하 측정은 하지 않았으며, 영향 규모를 측정하지 않은 항목에는 **추정**을 붙였습니다. SSO/RBAC·HA 등 이미 정리된 운영 범위는 [검증 범위와 운영 과제](production-readiness.md)에 있습니다.

P1 두 항목은 수정했습니다.

- **SSH jump 경유 deadline**: 모든 jump 연결에 같은 deadline 브리지를 적용합니다. 이전에는 MySQL·Oracle만 적용했고, Redis는 jump 경유 수집이 매번 실패했으며 PostgreSQL은 응답이 멈추면 수집 제한 시간을 넘겨 대기했습니다. PostgreSQL·Redis도 수집 제한 시간이 끝나면 소켓을 닫습니다. 근거: [ssh.go:168](../control-plane/ssh.go#L168), [jump_test.go](../control-plane/jump_test.go)
- **test 모드 인증 해제 제한**: `MONITORING_MODE=test`는 `testseed` 테스트 빌드이거나 루프백 리슨 주소일 때만 시작합니다. `CONTROL_ALLOWED_ORIGINS`가 필요하고, 인증이 꺼졌다는 경고를 남깁니다. 허용 origin의 Host로 들어온 요청만 처리해 DNS rebinding으로 인증 없는 API를 읽지 못하게 합니다. 근거: [service.go:344](../control-plane/service.go#L344), [web.go:153](../control-plane/web.go#L153)

| 우선 | 개선할 점 | 제안 | 근거 |
| --- | --- | --- | --- |
| P2 | 15초마다 모든 대상의 수집을 한꺼번에 시작합니다. 35초 제한 시간은 슬롯을 기다리는 동안에도 줄어들고, 대기 중에 시간이 끝나면 상태·관측 기록 없이 수집이 빠집니다. | 대기 예산과 수집 예산을 분리합니다. 대상별 시작 시각을 분산하고, 건너뛴 수집을 기록하고, 동시 수를 설정값으로 바꿉니다. | [service.go:321](../control-plane/service.go#L321), [collect.go:34](../control-plane/collect.go#L34) |
| P2 | 관측 시각이 수집을 끝낸 시각으로 기록됩니다. 그래서 수집 지연이나 건너뜀으로 간격이 1.5주기를 넘으면 ‘지속’ 조건 규칙이 끊깁니다. | 예약된 틱 시각을 기록합니다. 빠진 틱은 미수집 표본으로 명시합니다. | [rules.js:8](../control-plane/web/lib/rules.js#L8) |
| P2 | 키가 맞지 않거나 손상된 행이 하나라도 있으면 등록 목록 API 전체가 실패하고 수집도 멈춥니다. 하지만 `/api/health`는 항상 `ok`를 반환합니다. | 시작할 때 키를 검증합니다. 손상된 행만 격리하고, DB·복호화·마지막 수집 시각을 확인하는 `/api/ready`를 추가합니다. | [store.go:106](../control-plane/store.go#L106), [web.go:129](../control-plane/web.go#L129) |
| P2 | 스냅샷 캐시 락을 SQLite 조회와 계열 계산이 끝날 때까지 잡고 있습니다. SQLite 연결이 1개라 읽기와 쓰기도 직렬화됩니다. | 같은 키의 요청만 하나로 합치고 계산은 락 밖에서 합니다. 읽기 연결과 쓰기 연결을 분리합니다. | [snapshot.go:158](../control-plane/snapshot.go#L158), [store.go:66](../control-plane/store.go#L66) |
| P2 | 화면을 갱신할 때마다 전체 스냅샷을 다시 JSON으로 만들고 압축합니다. ETag/304가 없고 응답 크기는 측정하지 않았습니다. | 인코딩된 바이트를 캐시하고, 최신 관측 시각을 ETag로 씁니다. 증분 응답이나 선택한 대상만 조회하는 방식도 검토합니다. | [worker.js:6](../control-plane/web/worker.js#L6), [snapshot.go:161](../control-plane/snapshot.go#L161) |
| P2 | 보존 정리는 시작 60분 뒤에 처음 실행되고 오류를 무시합니다. 재시작이 반복되면 15일 보존이 지켜지지 않을 수 있습니다. | 시작 직후 한 번 실행하고, 결과를 로그로 남기고, 점진적 VACUUM을 적용합니다. | [service.go:331](../control-plane/service.go#L331), [store.go:343](../control-plane/store.go#L343) |
| P2 | 6시간·24시간 그래프는 버킷마다 마지막 표본 하나만 써서 짧은 급등이 보이지 않습니다. | 버킷별 최소·최대·평균을 저장하고 밴드로 표시합니다. 집계 방식도 화면에 표시합니다. | [store.go:295](../control-plane/store.go#L295) |
| P2 | 모든 대상의 원시 값 1시간 이력을 메모리에 보관하지만, 실제로 쓰는 곳은 애플리케이션 수집뿐입니다. 대상을 삭제해도 메모리 상태가 남습니다. 메모리 규모는 추정입니다. | 애플리케이션에 필요한 키만 보관하고, 삭제할 때 메모리 상태를 정리합니다. | [collect.go:113](../control-plane/collect.go#L113), [service.go:144](../control-plane/service.go#L144) |
| P2 | 한 건만 조회할 때도 전체 등록 정보를 복호화합니다. 대상이 N개면 주기당 복호화 작업이 약 N²에 비례합니다(추정). | `WHERE id=?`로 한 건만 조회하고, 복호화한 레지스트리를 캐시해 쓰기 시 무효화합니다. | [store.go:93](../control-plane/store.go#L93), [store.go:127](../control-plane/store.go#L127) |
| P2 | 실행 중 로그와 수집기 자체 지표가 없고, 수집 오류는 버려집니다. CI가 없고 스케줄러·보존 정리·OS 출력 파서·Redis 수집의 단위 테스트도 없습니다. | `log/slog` 로그와 인증된 수집기 지표를 추가합니다. GitHub Actions에서 `go vet`과 `go test -race`를 실행합니다. | [service.go:326](../control-plane/service.go#L326) |
| P3 | 비밀번호가 있어도 Redis TLS 기본값은 평문입니다. Go가 직접 TLS를 제공할 때 HSTS가 없고, 인증서는 시작할 때만 읽습니다. | 평문 자격 증명을 저장할 때 경고하고, HSTS와 인증서 재로딩을 추가합니다. | [collect.go:433](../control-plane/collect.go#L433) |
| P3 | 터미널에 유휴 제한이 없습니다. 서비스 종료 시 `terminal.close` 감사가 유실될 수 있고, 인증 실패를 제한하지 않습니다. | 유휴 시간 제한, 종료 시 세션 정리·감사 기록, 인증 실패 지연을 추가합니다. | [terminal.go:126](../control-plane/terminal.go#L126), [terminal.go:97](../control-plane/terminal.go#L97) |
| P3 | 암호문에 키 버전이 없고, WAL을 쓰는 동안 안전하게 백업하는 명령이 없습니다. | 키 버전 접두어와 키 교체 명령, `VACUUM INTO` 백업 명령과 복구 절차를 제공합니다. | [store.go:82](../control-plane/store.go#L82) |
