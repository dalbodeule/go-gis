# MVP 요구사항 검증 체크리스트

기준 문서는 [`desktop-gis-codex-brief.md`](../../desktop-gis-codex-brief.md)다.
아래 상태는 현재 작업 트리에서 확인한 자동·수동 검증 범위를 구분한다.

| 기준 | 현재 구현 | 증거 | 상태 |
| --- | --- | --- | --- |
| SHP/GeoPackage 입력, 여러 파일 추가, 다중 레이어·속성 표시 | 파일 다중 선택 및 단일 파일 드롭 추가, GDAL `OpenAll`, 충돌 이름 정리, 동적 Qt layer tree와 layer별 속성 탭; 피처 카드에서 필드·값을 줄바꿈 표시 | `TestDesktopAddsMultipleVectorFilesAsLayers`, GeoPackage `OpenAll` tests | 자동 검증 완료; 실제 파일 수동 확인 권장 |
| 공통 CRS 표시, 이동/확대/축소, 선택·편집, 전체 저장/취소 | viewport-clipped Qt Quick canvas, coordinate navigation/status bar, multi-layer GeoPackage writer | `go test -tags native ./drivers/gdal ./internal/commands ./internal/render`; `CGO_CXXFLAGS=-std=c++17 go test -tags 'qt native' ./cmd/gis-desktop ./ui/qt/native`; offscreen QML launch | 코드·빌드 검증 완료; 실제 화면 상호작용은 Windows에서 수동 확인 필요 |
| PROJ 기반 4326/5179/5186 변환 | PROJ adapter와 CLI/native Qt CRS 옵션 | `drivers/proj`, `cmd/gis-cli/*_native_test.go` | 자동 검증 완료 |
| 필터·병합·GEOS 교차/합집합/차집합/버퍼 | 공용 commands와 CLI/Lua dispatch | `go test -tags native ./...` | 자동 검증 완료 |
| 한글 레이블 위치·회전·높이·스타일 | `core.Label`, 대표점 계산, DXF TEXT | `drivers/dxf/export_test.go`, label native test | 자동 검증 완료 |
| DXF를 ARES에서 열어 한글·레이어·좌표 확인 | UTF-8/CP949 exporter와 GDAL precheck | `scripts/verify-ares-precheck.sh` | ARES 실제 앱 검증 필요 |
| GeoPackage/PostGIS 읽기·쓰기와 트랜잭션 | GDAL writer, PostGIS atomic replacement | `go test -tags native ./drivers/postgis ./...` | 자동 검증 완료 |
| CLI 재현성과 최소 Lua API | CLI subcommands, Lua layers/property/spatial/export API | `go test ./...`, scripting tests | 자동 검증 완료 |

## 통합 검증

```sh
./scripts/verify.sh
./scripts/verify-ares-precheck.sh
```

`verify.sh`는 일반/native/race 테스트, native 빌드, Qt native 테스트와
패치 검사를 수행한다. ARES precheck는 두 DXF profile을 생성하고 GDAL로
재읽지만, ARES Commander의 실제 화면·글꼴 대체·재저장 호환성을 대신하지
않는다. 해당 항목은 [ARES 수동 절차](ares-commander.md)에 따라 Windows에서
수행하고 버전, profile, SHA-256, 결과와 캡처 경로를 기록해야 한다.

## Windows 데스크톱 수동 확인

Qt와 native dependencies가 준비된 Windows에서 다음을 확인한다.

1. `powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1 desktop-native`로 빌드하고 앱을 실행한다.
2. `Add vector files`에서 서로 다른 두 SHP를 한 번에 선택한다. 레이어가 함께
   표시되고, 이름이 충돌하면 구분되며, 각 레이어 속성 탭을 전환할 수 있어야 한다.
3. 기존 프로젝트에서 SHP 하나를 다시 선택하거나 지도에 드롭해 추가한다. 기존
   레이어가 유지되고 새 레이어가 추가되어야 한다.
   읽기 전용 대용량 프로젝트에서도 추가 후 속성 페이지가 원본 세션에서 읽히는지
   별도로 확인한다.
4. `Save GeoPackage`로 저장하고 앱을 재실행한 뒤 저장된 파일을 열어 모든 레이어와
   속성 값이 보존됐는지 확인한다.
5. 선/폴리곤을 지도 경계 밖으로 패닝해도 viewport 영역 밖에 그려지지 않는지,
   X/Y 좌표 이동·커서 좌표·근사 축척이 함께 갱신되는지 확인한다.
6. GDAL 사용 오류, Windows 경로/한글 파일명, 저장 대상 교체 실패가 없는지 기록한다.
