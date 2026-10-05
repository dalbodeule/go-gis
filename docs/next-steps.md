# 현재 구현 요약과 다음 목표

기준 요구사항은 [`desktop-gis-codex-brief.md`](../desktop-gis-codex-brief.md)다.
이 문서는 2026-10-05 현재 저장소와 대화에서 확인된 진행 상황을 요약한다.
사용자의 “이제 좀 빠르게 렌더링된다”는 피드백은 실제 사용 중 체감 개선으로
기록하되, 특정 데이터·축척·하드웨어의 성능 보증이나 정량 벤치마크로 해석하지 않는다.

## 지금까지 구현된 범위

- GDAL/PROJ/GEOS 기반 벡터 입력, CRS 변환, 공간 연산 및 Go 공용 명령 경계와
  CLI/제한된 Lua 경로를 마련했다.
- Qt Quick 데스크톱에 레이어 추가·관리, 속성 및 심볼/레이블 설정, 지도 이동·확대,
  편집·워크스페이스 저장, DXF/GeoPackage 내보내기를 연결했다.
- 대형 읽기 전용 레이어는 viewport 단위로 읽고 점진적으로 렌더링한다. 렌더 요청
  취소/교체, 공간 인덱스 캐시, 축척별 일반화·예산, 피처/geometry 안전 상한을 두어
  화면 응답성과 메모리 안전을 함께 다룬다. 사용자의 최근 피드백으로 체감 로딩이
  개선된 것은 확인했지만, 전국 단위 데이터나 모든 확대 수준의 수치 성능은 아직
  검증된 것이 아니다.
- 화면 레이블은 축척·겹침·개수 제한을 적용하고, DXF 레이블은 화면용 생략 규칙과
  분리해 출력한다. 점 레이블은 8방향 배치, 선/폴리곤은 중앙 대표점·회전 필드·긴
  선분 기준 자유 각도를 지원하도록 구현했다.
- DXF는 점 심볼, TEXT 정렬/오프셋/회전, 한글 UTF-8/CP949 프로파일, polygon SOLID
  채움과 내부 구멍 경계, CAD 레이어별 출력 설정 및 초기 뷰 정보를 기록한다.
  해치 무한 렌더링을 피하기 위해 채움은 SOLID 삼각형으로 표현한다.
- 레이어 속성 UI의 정렬·필드 힌트·투명 채움 설정과 번역 회귀를 보강했다.

관련 동작은 [레이어 속성/레이블 안내](layer-properties.md),
[ARES DXF 검증 절차](verification/ares-commander.md),
[MVP 체크리스트](verification/mvp-checklist.md),
[기술 결정 기록](decisions/)에서 확인한다.

## 다음 목표 — 우선순위 순

### 완료: ARES Commander 2027 실사용 검증

사용자가 ARES Commander 2027에서 실사용 검증을 완료했다고 확인했다. 별도의
OS/버전/profile별 결과표나 캡처는 전달되지 않았으므로 이 문서에는 완료 사실만
기록하며 검증 세부사항을 추정하지 않는다. 이 항목을 막힌 선행조건으로 두지 않는다.

### 1. 세 OS의 실제 앱 빌드와 배포 패키지

현재 확인된 빌드는 소스/CLI 또는 개발 머신의 데스크톱 빌드까지다. 실행파일 빌드와
사용자에게 전달 가능한 패키지는 다르다. ADR 0013을 기준으로 1차 패키지 목표를
아래처럼 둔다.

| OS | 1차 패키지 형태 | 현재 근거 | 남은 검증 |
| --- | --- | --- | --- |
| macOS ARM64 | Qt 및 GIS 런타임을 포함한 `.app` + `.dmg` | 아래 내부 테스트 `.app`/`.dmg` 생성; `codesign --verify --deep --strict` 통과, `hdiutil verify` CRC 유효. Qt Cocoa 플러그인이 로드되는 것까지 확인 | 이 실행 세션은 AppKit pasteboard/XPC 서비스 연결 오류 후 SIGABRT하여 UI 수용 테스트 미통과. 일반 macOS 사용자 세션에서 실행 및 GIS 데이터 열기 확인 필요. 파일 크기 최적화와 공개 배포 서명/공증도 남음 |
| Windows x64 | 런타임을 포함한 portable `.zip` | 최신 CI에서 Windows portable 테스트/vet/CLI 빌드 통과 | native GIS + Qt desktop 빌드, DLL/QML/data 번들링, 깨끗한 Windows에서 압축 해제 후 실행 |
| Linux x64 | 정의한 기준 배포판용 AppImage | 최신 CI에서 Ubuntu native GIS 테스트/race/native CLI 빌드 통과 | Qt desktop 빌드, AppImage 생성, plugin/shared-library/GDAL·PROJ data 번들링, 깨끗한 VM에서 실행 |
| Windows/Linux ARM64 | Qt + GDAL/PROJ/GEOS 포함 native 데스크톱 패키지 | 현재 미구현. 기존 ARM64 portable CLI 교차 빌드는 이 목표의 근거가 아님 | ARM64 native runner/toolchain, 전체 런타임 번들, clean-machine 앱 실행을 별도 구현·검증 |

최신 원격 [CI 실행](https://github.com/dalbodeule/go-gis/actions/runs/37301854222)은
2026-10-05 성공했다. 단, workflow는 Windows에서 portable CLI만 빌드하고 Linux/macOS에서
native GIS CLI를 빌드한다. 어느 runner에서도 Qt 데스크톱 패키지를 만들지 않는다.
로컬 Mac에서 실행한 빌드는 성공했지만 배포 번들이나 다른 OS의 앱 실행 증거는 아니다.

패키지 작업의 순서는 ① native desktop 빌드 재현, ② OS별 의존성/라이선스/data
목록 작성, ③ 로컬 portable bundle 생성, ④ 서명/설치 없는 깨끗한 VM에서 실행, ⑤
공개 배포가 필요할 때만 코드 서명·공증/Windows 서명 추가로 고정한다. 기본 패키지는
개발 머신의 Homebrew/OSGeo4W 절대 경로에 의존해서는 안 된다. Qt 및 GIS 런타임의
재배포 조건은 [배포 ADR](decisions/0013-desktop-distribution.md)에 따른다.

macOS 패키징 파일럿은 QML 플러그인 의존성 누락, ICU 이름 충돌, Homebrew 데이터의 외부
심볼릭 링크, 서로 다른 Qt 프레임워크 중복 적재 문제를 차례로 드러냈고 패키저에 이를
반영했다. 최종 내부 테스트 산출물은
[`GoGIS-0.1.0-dev-macos-arm64-20261005T122628Z.app`](../build/packages/macos-arm64/GoGIS-0.1.0-dev-macos-arm64-20261005T122628Z.app)
및
[`GoGIS-0.1.0-dev-macos-arm64-20261005T122628Z.dmg`](../build/packages/macos-arm64/GoGIS-0.1.0-dev-macos-arm64-20261005T122628Z.dmg)다.
앱 번들의 ad-hoc 서명과 DMG 체크섬은 검증했다. 직접 실행 시 Qt Cocoa 플러그인은
로드되지만 이 Codex 실행 세션에서는 `com.apple.pasteboard.*` XPC 연결이 거부된 뒤
SIGABRT가 발생했다. 따라서 UI의 실제 실행 성공이나 GIS 기능 수용으로 간주하지 않는다.
통상 로그인된 macOS GUI 세션에서 추가 확인해야 한다. 앱 번들은 약 1.1 GiB, DMG는 약
917 MiB이므로 배포 크기 최적화도 남아 있다. `scripts/package_macos.py`는 제한된 세션에서
앱 번들을 만든 뒤 `hdiutil`의 장치 구성 제한으로 종료했으며, DMG는 승인된 별도
`hdiutil` 실행으로 생성·검증했다. GDAL/PROJ 번들 데이터 경로 코드는 있고 관련 Qt/native
테스트도 통과했으나, 데이터 열기까지 검증된 것은 아니다.

### 2. Milestone C 잔여 범위 확정

Brief의 Milestone C를 저장소 코드와 대조한 결과, “데이터 처리 엔진”은 상당 부분
구현됐지만 사용 가능한 GUI 업무 흐름 및 PostGIS 연결까지 완료된 것은 아니다.

| Milestone C 항목 | 현재 구현/증거 | 실제 잔여 범위 |
| --- | --- | --- |
| 병합 및 GEOS 공간 연산 | 공용 `ProjectService`/command, CLI merge/spatial, Lua spatial API; 단위/native 테스트 | 데스크톱 UI에 입력 레이어/연산/결과 레이어/취소·오류 흐름이 없음. GUI에서도 같은 공용 command를 사용할지 구현·검증 필요. |
| 레이블 설정/생성 | 데스크톱 레이어별 표현식·Lua·스타일 설정, CLI label, Lua label/filter, DXF TEXT; 일부 Lua와 공용 command 결과 parity 테스트 | 대표 입력에서 GUI 설정 결과와 CLI/Lua 결과의 실제 산출물 동등성(geometry/속성/레이블)을 자동 비교하는 수용 테스트가 없음. |
| DXF/GeoPackage 출력 | DXF exporter와 GUI/CLI/Lua 연결; GDAL GeoPackage writer 및 GUI 저장 경로, native 테스트 | 일반 파일 왕복 시나리오를 CLI와 GUI에서 동일 fixture로 확인하고 데이터/CRS/레이블 보존을 명시적으로 검증. ARES 사용 확인은 완료로 처리. |
| PostGIS I/O 및 롤백 | `drivers/postgis`의 read/store 및 원자적 replacement; 2026-10-05 CI live PostGIS 롤백 테스트 성공 | PostGIS를 여는/저장하는 사용 경로가 CLI·데스크톱에 연결되어 있지 않음. 연결 설정, 선택한 schema/table, 읽기/쓰기 및 실패 rollback을 실제 workflow로 제공하고 통합 테스트해야 함. |
| CLI 배치 및 Lua 예제 | CLI convert/filter/merge/spatial/label/script, sandboxed Lua API, 예제 및 테스트 | 명령/API 문서를 공통 옵션·오류·취소 규약에 맞춰 정리하고, 구현된 같은 작업의 parity matrix를 보강. Lua 전체를 CLI와 동일 기능으로 확장하는 것은 brief가 요구하는 범위를 먼저 판정한 뒤 결정. |

이 표에서 가장 큰 기능 공백은 **PostGIS의 애플리케이션 연결**과 **GUI에서 병합/공간
연산을 실행하는 경로**다. parity 검증은 이 기능 경로를 실제로 제공한 후 같은 소형
fixture를 이용해 범위를 좁혀 수행한다. “Milestone C 완료”는 각 행의 기능 경로가
사용 가능하고 테스트/수용 기준이 통과했을 때만 선언한다.

### 3. 실제 지도에서 렌더링·레이블 품질 회귀 확인

세종 연속지적도·도근점·건물 레이어를 이용해 전체 범위, 중간 축척, 필지 단위 확대를
차례로 확인한다. 레이어 추가 시 중심/축척 유지, 축척 전환 시 세부 형상 복원,
화면 좌우 잘림, 과도한 로딩 지속, 레이블 중복/누락, 선·점의 화면상 크기를 기록한다.
회귀가 있으면 재현 축척·레이어·로그와 함께 일반화 단계, 청크 캐시, 겹침 생략,
화면 단위 심볼 크기를 조정한다. 원본 피처를 자동으로 삭제하거나 고치지 않는다.

### 4. 대형 데이터 안전 한도와 장시간 동작 검증

기존 viewport 반복 이동 테스트에 더해 polygon-heavy 입력에서 메모리 peak, 취소 후
작업 정리, 여러 레이어 교차 표시, 반복 확대/축소 후 자원 회수를 관찰한다. 6 GB는
사용자가 허용 가능하다고 제안한 상한이지 현재 할당 목표나 안전 보증으로 간주하지
않는다. 기본 한도는 OS 메모리·입력 복잡도와 함께 측정한 뒤 조정하며, 예산 초과 시
충돌 대신 취소 가능한 경고/축척 축소 안내를 제공한다.

### 5. MVP 체크리스트 갱신

ARES 수동 검증은 완료로 반영하고, Windows/Linux 앱/패키지·PostGIS 앱 통합·GUI
공간작업 경로는 미완료로 유지한다. 자동검증·실기검증·미구현을 섞지 않는다.

## 검증 현황 해석

2026-10-05 Mac ARM64에서 `./scripts/build.sh all-native` 및
`CGO_CXXFLAGS='-std=c++17' go test -tags 'qt native' ./cmd/gis-desktop ./ui/qt/native -count=1`
가 통과했다. 동일 날짜 원격 CI는 portable Go checks (3 OS), native GIS checks
(macOS/Linux), live PostGIS rollback 및 vulnerability scan을 통과했다.
`go test ./...` 및 `go vet ./...`도 통과했다. 동일 날짜 원격 CI는 portable Go checks
(3 OS), native GIS checks (macOS/Linux), live PostGIS rollback 및 vulnerability scan을
통과했다. Windows native/Qt desktop 및 Windows/Linux 배포 패키지는 미확인 상태다. macOS
`.app` 서명과 DMG 컨테이너는 통과했으나 GUI runtime 및 GIS 데이터 수용 게이트는 미완료다.
Windows/Linux용 Qt 앱 또는 배포 패키지는 해당 OS의 runner/환경에서 별도 구현과 검증이
필요하다.

저장소 규칙에 따라 에이전트는 커밋하지 않는다. 코드 변경 후 사용자가 검토해 직접
커밋하며, 로컬 Git 기본 GPG 키로 서명한다.
