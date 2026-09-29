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
- PROJ XY 변환과 GEOS 교차·합집합·차집합·버퍼 어댑터
- ARES 검증 전 단계의 명시적 ASCII DXF exporter와 한글 fixture
- pgx 기반 PostGIS 트랜잭션 writer 경계
- gopher-lua 기반 최소 `layers`, `set_property`, `export_dxf` API

## 남은 1단계 작업

- GDAL/OGR 기반 SHP·GeoPackage 실제 쓰기 및 실제 샘플 회귀 테스트
- PROJ 기반 CRS 확인·변환의 EPSG:5179/5186/4326 회귀 테스트
- 선택한 GUI 기술의 실제 지도 캔버스 연결
- 파일 저장 어댑터 연결
- PostGIS 스키마/읽기 경로, DXF 버전·CP949 조합, Lua API 샌드박스 정책 보강

네이티브 드라이버는 OS별 런타임 배포와 라이선스 조사를 마친 뒤 `drivers/interfaces.go`의 경계에 연결한다. 현재 테스트는 외부 의존성 없이 실행된다.
