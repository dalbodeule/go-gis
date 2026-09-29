# ADR 0001: 초기 기술 스택

- 상태: 제안 및 초기 채택
- 기준: `desktop-gis-codex-brief.md`

## 결정

### 코어와 애플리케이션 계층

Go를 주 언어로 사용한다. `internal/core`는 `Project`, `Layer`, `Feature`, `Geometry`, `CRS`, `Style`, `Label`의 포맷 독립 모델을 소유하고, `internal/commands`는 GUI·CLI·Lua가 공유하는 작업 경계를 소유한다.

### GIS 엔진과 데이터 접근

- GDAL/OGR: SHP, GeoPackage 등 벡터 I/O
- PROJ: CRS 식별 및 좌표 변환
- GEOS: 기본 공간 연산
- PostgreSQL/PostGIS: 트랜잭션 기반 DB I/O

각 라이브러리는 `drivers/` 아래 어댑터로 감싼다. 초기에는 네이티브 런타임 배포와 CGO 동시성 규칙을 기술 검증에서 확인하며, 코어 패키지가 특정 바인딩 타입을 직접 노출하지 않도록 한다.

### 사용자 인터페이스와 자동화

GUI는 Qt 바인딩과 Wails+WebGL/WebGPU를 수직 프로토타입으로 비교한다. 유지보수성, 3개 OS 패키징, 지도 렌더링 성능, 입력·편집 UX를 근거로 결정한다. 최종 GUI를 확정하기 전까지 `ui/`와 코어 API 사이에 명령 경계를 유지한다.

CLI는 Go 명령으로 먼저 제공하고, Lua는 UI 스레드나 내부 포인터를 노출하지 않는 안전한 공용 명령 API 위에 얹는다.

### DXF

DXF 출력은 최소 엔티티부터 구현하거나 기존 라이브러리를 비교 검증한다. UTF-8 또는 CP949를 선결정하지 않고 `$DWGCODEPAGE`, font override, DXF 버전과 함께 ARES Commander 2027 샘플 검증으로 결정한다.

## 선택 이유

브리프는 2D 벡터 GIS, 정확한 CRS 변환, 여러 입출력 포맷, GUI·CLI·Lua의 동일 명령 재사용을 우선한다. 따라서 UI보다 포맷 독립 코어와 드라이버 경계를 먼저 만들고, GDAL/PROJ/GEOS의 성숙한 기능을 재구현하지 않는 구성이 초기 위험을 낮춘다.

## 보류된 결정

- Go 모듈의 최종 공개 경로와 저장소 원격 주소
- Qt 바인딩 대 Wails+WebGL/WebGPU
- GDAL/PROJ/GEOS의 구체적인 Go 바인딩 및 3개 OS 배포 방식
- DXF 버전, 인코딩 조합, ARES Commander 호환 범위
- PostGIS 드라이버와 인증 방식

이 항목들은 마일스톤 A의 프로토타입과 실제 샘플 검증 이후 별도 ADR로 확정한다.
