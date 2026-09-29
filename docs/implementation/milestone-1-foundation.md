# 1단계 MVP 착수 기록

## 이번 범위

1단계의 첫 수직 기반으로 UI와 파일 포맷에 종속되지 않는 공용 명령 계층을 추가했다.

- `core.Project`, `Layer`, `Feature`, `Field`, `CRS` 모델
- Geometry 드라이버를 받을 수 있는 코어 인터페이스
- 레이어 추가와 속성 수정의 commit/rollback 편집 트랜잭션
- GUI·CLI·Lua가 공유할 `commands.ProjectService`
- 한글 속성값을 포함한 편집 commit/rollback 테스트
- 드라이버 경계: 벡터 I/O, CRS 변환, 공간 연산, PostGIS 트랜잭션, DXF 출력
- UI 어댑터용 레이어 트리·속성 테이블·지도 뷰포트 읽기 모델
- 취소 가능한 백그라운드 작업과 진행률 수집기
- `native` 태그 기반 GDAL/PROJ/GEOS 바인딩 import 및 어댑터
- GDAL/OGR 벡터 layer reader와 WKT core 변환 경계
- GDAL/OGR 기반 GeoPackage·SHP writer와 실제 round-trip 회귀 테스트
- 다중 layer 입력을 위한 `drivers.LayerCollectionReader` 경계와 GDAL 구현
- PROJ 기반 EPSG:4326·5179·5186 좌표 회귀 테스트와 XY 축 순서 보정
- `ProjectService.SaveLayer` 저장 어댑터 경계와 writer clone 보장 테스트
- DXF AC1015 UTF-8·CP949 프로파일의 헤더·바이트 인코딩 회귀 테스트
- 포맷 독립 `core.Label` 모델과 DXF TEXT의 위치·회전·높이·스타일 출력
- PostGIS JSONB 기반 스키마 생성·레이어 읽기·원자적 저장 경로
- Lua allow-list 샌드박스와 context 기반 실행 취소 테스트
- PROJ XY 변환과 GEOS 교차·합집합·차집합·버퍼 어댑터
- `commands.ApplySpatialOperation` 공용 dispatch와 원자적 결과 레이어 적용
- native CLI `spatial` 명령의 GDAL 입력 → GEOS 연산 → DXF/GDAL 출력 경로
- 동일 스키마·CRS 레이어 병합과 중복 feature ID 검증
- 공용 속성 필터와 CLI/Lua 결과 레이어 생성
- 속성값 기반 `core.Label` 생성과 Point/LineString/Polygon 대표 위치 계산
- ARES 검증 전 단계의 명시적 ASCII DXF exporter와 한글 fixture
- pgx 기반 PostGIS 트랜잭션 writer 경계
- PostGIS `SaveLayer`의 기존 snapshot 삭제·재삽입을 단일 트랜잭션으로 수행
- gopher-lua 기반 최소 `layers`, `set_property`, `export_dxf` API
- gopher-lua `spatial` API를 통한 공용 공간 연산 dispatch와 결과 레이어 추가

## 남은 1단계 작업

- ARES Commander 실제 앱 호환성

Qt native desktop의 GDAL layer 입력·정규화 geometry·동적 layer tree·속성
table·선택 경로는 `desktop-native` target과 `testdata/sample.geojson`으로
검증했다. 다중 layer dataset은 `GDAL Reader.OpenAll`과 layer별 render source로
구성한다. `--save`를 지정한 native UI에서는 편집 Commit의
결과를 새 GeoPackage/SHP로 저장할 수 있다.

네이티브 드라이버는 OS별 런타임 배포와 라이선스 조사를 마친 뒤 `drivers/interfaces.go`의 경계에 연결한다. 현재 테스트는 외부 의존성 없이 실행된다.
