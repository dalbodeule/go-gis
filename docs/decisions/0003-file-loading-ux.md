# ADR 0003: 데스크톱 벡터 파일 열기 UX

- 상태: 구현 중인 첫 수직 슬라이스
- 기준일: 2026-09-29
- 범위: Qt Quick에서 SHP·GeoPackage·GeoJSON 파일을 선택하거나 드롭해 로드

## 결정

파일 열기 UX는 QML의 `FileDialog`와 `DropArea`를 사용하고, 파일 경로만
native Go runtime에 전달한다. 실제 GDAL 열기, CRS 정렬, 공통 extent 계산,
레이어/속성 payload 생성은 기존 `runtime_native_qt.go` 경로를 재사용한다.

파일 경로 전달은 `MapCanvas`의 `loadPath`와 `loadGeneration` 동적 속성을
통해 수행한다. generation을 사용해 같은 파일을 다시 선택하는 요청도
구분하고, C++ bridge는 Qt GUI thread에서 값을 snapshot한 뒤 Go polling
goroutine이 읽는다.

## 지원 범위

- `*.shp`: 같은 디렉터리의 `.shx`, `.dbf`, `.prj`, `.cpg` sidecar를 GDAL이
  함께 찾는 전제다. 단일 `.shp`만 복사해 전달하는 것은 완전한 dataset이 아니다.
- `*.gpkg`
- `*.geojson`, `*.json`

선택/드롭 후에는 기존 `--input` 경로와 동일하게 CRS를 읽고, 첫 유효 CRS를
기본 표시 CRS로 사용한다. CRS가 없는 layer와 변환 대상 CRS가 함께 있으면
오류를 표시하고 기존 화면을 유지한다.

## 후속 작업

1. GUI에서 source CRS/target CRS 입력 dialog 추가
2. `Save As` dialog와 dirty state 연결
3. 전체 extent로 이동하는 `Zoom to fit`
4. 파일 대화상자·드롭·오류 상태의 Qt native 회귀 테스트
5. 대용량 SHP에서 GDAL spatial filter/page 단위 읽기로 메모리 사용량 개선
