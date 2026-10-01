# ADR 0001: 초기 기술 스택

- 상태: 제안 및 초기 채택
- 기준: `desktop-gis-codex-brief.md`

## 결정

### 코어와 애플리케이션 계층

Go를 주 언어로 사용한다. `internal/core`는 `Project`, `Layer`, `Feature`, `Geometry`, `CRS`, `Style`, `Label`의 포맷 독립 모델을 소유하고, `internal/commands`는 GUI·CLI·Lua가 공유하는 작업 경계를 소유한다.

### GIS 엔진과 데이터 접근

- GDAL/OGR: SHP, GeoPackage 등 벡터 I/O — `github.com/airbusgeo/godal`
- PROJ: CRS 식별 및 좌표 변환 — `github.com/twpayne/go-proj/v11`
- GEOS: 기본 공간 연산 — `github.com/twpayne/go-geos`
- PostgreSQL/PostGIS: 트랜잭션 기반 DB I/O

각 라이브러리는 `drivers/` 아래 어댑터로 감싼다. 초기에는 네이티브 런타임 배포와 CGO 동시성 규칙을 기술 검증에서 확인하며, 코어 패키지가 특정 바인딩 타입을 직접 노출하지 않도록 한다.

검토 기준일: 2026-09-29.

- `godal`은 GDAL 3.0 이상을 요구하고, CGO 호출 횟수를 줄이는 Go API를 제공한다. 다만 upstream README가 벡터와 공간참조 영역은 아직 완성도가 낮고 API가 호환성 깨지는 방향으로 바뀔 수 있다고 명시하므로, SHP/GPKG 경로를 먼저 작은 어댑터와 샘플로 고정한다.
- `go-proj/v11`은 PROJ 9.4 이상을 요구하며 대량 좌표 변환, 오류 처리, 자동 C 메모리 관리를 제공한다. 코어/WKT의 XY는 visualization order(경위도 및 동·북 좌표)로 고정하며 geometry와 bounds 변환 모두 `NormalizeForVisualization`을 사용한다. EPSG:5179/5186의 공식 축은 북·동 순서이므로 이를 동·북 순서로 정규화하는 회귀 테스트를 둔다. QGIS 독립 control point 대조와 좌표변환 작업의 geodetic accuracy 확인은 수동 검증 항목이다.
- `go-geos`는 GEOS의 thread-safe 재진입 API를 사용하고 GeoJSON/WKB/WKT 및 `database/sql` 연동을 제공한다. 장시간 실행 앱에서 C heap 압력이 Go runtime에 직접 보이지 않는다는 upstream 경고가 있으므로, 작업 단위별 context와 명시적 geometry 수명 관리를 적용한다.

### 사용자 인터페이스와 자동화

GUI는 Qt 바인딩과 Wails+WebGL/WebGPU를 수직 프로토타입으로 비교한다. 유지보수성, 3개 OS 패키징, 지도 렌더링 성능, 입력·편집 UX를 근거로 결정한다. 최종 GUI를 확정하기 전까지 `ui/`와 코어 API 사이에 명령 경계를 유지한다.

CLI는 Go 명령으로 먼저 제공하고, Lua는 UI 스레드나 내부 포인터를 노출하지 않는 안전한 공용 명령 API 위에 얹는다.

### DXF

`github.com/yofu/dxf`는 MIT 라이선스의 ASCII DXF 라이브러리지만 AC1015(AutoCAD 2000)만 지원한다. 기본 엔티티 생성의 참고 구현으로는 유용하나, ARES Commander 2027 한글 검증에 필요한 DXF 버전·`$DWGCODEPAGE`·UTF-8/CP949 조합을 충분히 제어할 수 있다는 증거가 없어 핵심 exporter로 채택하지 않는다. GoGIS는 필요한 엔티티와 인코딩을 직접 제어하는 `drivers/dxf` 어댑터를 우선 구현하고, `yofu/dxf`는 비교용 fixture 생성에만 사용할 수 있다.

## 선택 이유

브리프는 2D 벡터 GIS, 정확한 CRS 변환, 여러 입출력 포맷, GUI·CLI·Lua의 동일 명령 재사용을 우선한다. 따라서 UI보다 포맷 독립 코어와 드라이버 경계를 먼저 만들고, GDAL/PROJ/GEOS의 성숙한 기능을 재구현하지 않는 구성이 초기 위험을 낮춘다.

## 보류된 결정

- Go 모듈의 최종 공개 경로와 저장소 원격 주소
- Qt 바인딩 대 Wails+WebGL/WebGPU
- GDAL/PROJ/GEOS의 구체적인 Go 바인딩 및 3개 OS 배포 방식
- DXF 버전, 인코딩 조합, ARES Commander 호환 범위
- PostGIS 드라이버와 인증 방식

이 항목들은 마일스톤 A의 프로토타입과 실제 샘플 검증 이후 별도 ADR로 확정한다.

## 참고한 upstream 문서

- [godal README](https://github.com/airbusgeo/godal)
- [go-proj README](https://github.com/twpayne/go-proj)
- [go-geos README](https://github.com/twpayne/go-geos)
- [yofu/dxf README](https://github.com/yofu/dxf)
