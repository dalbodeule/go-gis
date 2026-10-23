# 마일스톤 C 결과 parity 검증

기준 요구사항은 동일한 입력과 옵션을 GUI·CLI·Lua에서 실행했을 때 공용 명령 경계
아래에서 같은 GIS 결과를 얻는 것이다. 이 문서는 구현 경로와 현재 증거를 분리해
기록한다.

| 작업 | GUI 경로 | CLI 경로 | Lua 경로 | 공통 결과/검증 |
| --- | --- | --- | --- | --- |
| 필터 | 데스크톱 프로젝트 편집/속성 경로 | `filter`/`script` | `gogis.filter`, `filter_lua` | `TestLuaFilterAndLabelResultsMatchSharedCommandAPI` |
| 레이블 | 레이어 설정의 표현식·Lua·규칙 | `label`/`script` | `gogis.label`, `label_lua` | 같은 fixture의 피처·레이블 deep equality |
| 공간 연산 | 공간 연산 대화상자 → `applySpatialRequest` → `ProjectService.ApplySpatialOperation` | `spatial` → `commands.ApplySpatialOperation` | `gogis.spatial` → `ProjectService.ApplySpatialOperation` | `internal/commands/spatial_test.go`, `TestRuntimeSpatialUsesSharedCommandAndAddsResultLayer`, `TestApplySpatialRequestBuildsResultLayerInDesktopRuntime`, native CLI spatial tests |
| DXF 출력 | 데스크톱 저장/내보내기 | `convert`, `spatial`, `label` | `gogis.export_dxf` | exporter의 profile·레이어·geometry 회귀 테스트 |
| GeoPackage 출력 | 데스크톱 저장 | `convert`, `script` | `gogis.export_gpkg` | GDAL writer round-trip 테스트 |
| PostGIS 레코드 | `--postgis-table` 로드 후 확인 대화상자에서 추가/삭제 | `postgis-insert`/`postgis-delete` | 공용 프로젝트 편집 API 범위 밖 | Docker PostGIS rollback 및 desktop runtime add/delete 통합 테스트 |

## 검증 명령

전체 마일스톤 C 게이트는 Docker Desktop이 실행 중일 때 다음 한 명령으로 실행한다.

```sh
PATH="/Users/jioo0224/go/bin:$PATH" ./scripts/verify-milestone-c.sh
```

이 게이트는 Go 1.27.3/module verify, 공용 command·CLI·Lua, native Qt, QML,
PostGIS rollback·데스크톱 레코드 추가/삭제, 취약점 검사와 patch check를 순서대로
실행한다. Docker 또는 Qt가 없는 환경에서는 해당 통합 단계만 명시적으로 건너뛴다.

개별 검증은 다음 명령으로도 실행할 수 있다.

```sh
go test ./...
CGO_CXXFLAGS='-std=c++17' go test -tags native ./cmd/gis-cli ./drivers/postgis
CGO_CXXFLAGS='-std=c++17' go test -tags 'qt native' ./cmd/gis-desktop ./ui/qt/native
QT_QPA_PLATFORM=offscreen qmltestrunner \
  -import cmd/gis-desktop/qmltests \
  -input cmd/gis-desktop/qmltests -silent
```

현재 fixture 기반 parity는 공용 command/Lua와 각 exporter에 대해 자동화되어 있다.
실제 GUI와 CLI가 동일 파일을 읽고 만든 산출물을 byte-for-byte 비교하지 않는 이유는
DXF의 생성 시각·순서 같은 포맷 메타데이터 때문이다. 대신 geometry, 속성, CRS,
레이어와 label 구조를 모델 수준에서 비교하는 수용 테스트를 추가 대상으로 유지한다.

PostGIS 인증 정보는 fixture나 workspace에 넣지 않고 `GOGIS_TEST_POSTGIS_DSN` 또는
`GOGIS_POSTGIS_DSN` 환경 변수로만 주입한다.
