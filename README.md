# GoGIS

업무 특화 2D 벡터 GIS 데스크톱 애플리케이션을 Go 중심으로 개발하는 저장소입니다.

현재 저장소는 구현 착수를 위한 최소 뼈대입니다. 전체 목표와 MVP 범위는 [desktop-gis-codex-brief.md](desktop-gis-codex-brief.md), 기술 선택과 보류된 결정은 [기술 스택 결정 기록](docs/decisions/0001-tech-stack.md)에서 확인할 수 있습니다.

운영체제별 Go·CGO·GDAL/PROJ/GEOS 설치와 빌드는 [빌드 가이드](docs/build.md)를 참고합니다.

빌드 산출물은 저장소의 `build/` 폴더에 생성합니다. 공통 빌드 스크립트는
`scripts/build.sh`입니다.

마일스톤 B의 UI·부분 렌더링 후보와 벤치마크 기준은 [UI 렌더링 결정 기록](docs/decisions/0002-milestone-b-ui-rendering.md)에 정리되어 있습니다.

## 선택한 기술 스택

- Go: GIS 도메인 모델, 공용 명령/API, CLI, 작업 취소·진행률 제어
- GDAL/OGR: SHP·GeoPackage 등 벡터 입출력
- PROJ: CRS 확인과 좌표 변환
- GEOS: 교차·합집합·차집합·버퍼 등 공간 연산
- PostgreSQL/PostGIS: 트랜잭션 기반 DB 읽기·쓰기
- DXF exporter: 자체 포맷 경계와 실제 ARES Commander 검증을 거쳐 채택
- GUI: Qt 바인딩과 Wails+WebGL/WebGPU를 수직 프로토타입으로 비교한 뒤 결정
- Lua: 안전한 공개 명령 API 위에 최소 스크립팅 계층으로 추가

네이티브 의존성은 Go 코어에 직접 섞지 않고 `drivers/` 경계에 둡니다. 바인딩 모듈은 `go.mod`에 등록되어 있지만 실제 CGO import 경계는 `native` build tag 아래에 있으므로, 기본 CLI 빌드는 네이티브 GIS 설치 없이도 가능합니다. 운영체제별 설치와 네이티브 빌드는 [빌드 가이드](docs/build.md)를 따릅니다.

레이블은 `internal/core.Label`로 위치(X/Y), 회전, 높이, 문자 스타일을 표현하며 DXF TEXT로 내보냅니다. 포맷별 문자 인코딩과 ARES Commander 호환성은 별도 샘플 검증이 필요합니다.

공간 연산은 `internal/commands.ApplySpatialOperation`을 통해 `intersect`,
`union`, `difference`, `buffer`를 공통 dispatch하며, `ProjectService`에 결과
레이어를 원자적으로 추가할 수 있습니다. 실제 GEOS 구현은 `native` 빌드에서
`drivers/geos`를 주입합니다. 이 공용 경계에서 binary operation의 CRS 일치도
검사하므로 CLI·GUI·Lua가 서로 다른 CRS를 조용히 연산하지 않습니다.

Lua에서는 `gogis.spatial("buffer", "roads", "", "roads_buffer", 10)`처럼
동일한 공간 연산 명령을 호출할 수 있습니다.

속성 필터는 CLI에서 다음과 같이 실행합니다. 값 비교는 문자열·불리언·숫자
필드를 지원하며, 일치하는 feature만 결과 레이어로 복사합니다.

```sh
./build/gis-cli filter \
  --input data/roads.gpkg \
  --layer roads \
  --field kind \
  --value road \
  --output build/roads-only.gpkg
```

속성값을 geometry 대표 위치의 DXF TEXT 레이블로 생성할 수도 있습니다.

```sh
./build/gis-cli label \
  --input data/roads.gpkg \
  --layer roads \
  --field name \
  --height 2.5 \
  --style Korean \
  --output build/roads-labeled.dxf
```

동일 스키마 레이어 병합은 `commands.MergeLayers`를 사용합니다. CRS·필드
스키마가 다르거나 feature ID가 중복되면 명확한 오류를 반환하며, 프로젝트에
결과를 추가할 때는 편집 트랜잭션으로 원자적으로 커밋됩니다.

## 시작하기

```sh
go run ./cmd/gis-cli --help
go test ./...

# 기본 CLI 빌드: build/gis-cli
./scripts/build.sh cli

# Qt Quick 데스크톱 빌드: build/gogis-desktop
./scripts/build.sh desktop
```

네이티브 GIS 드라이버를 포함한 CLI는 다음과 같이 빌드합니다.

```sh
./scripts/build.sh native
# native CLI + native Qt desktop를 함께 빌드하려면
./scripts/build.sh all-native
```

`convert`는 SHP 또는 GeoPackage 벡터 레이어를 읽어 DXF로 내보냅니다. 필요하면 입력 CRS를 덮어쓰고 출력 CRS로 좌표를 변환할 수 있습니다.

```sh
./build/gis-cli convert \
  --input data/roads.gpkg \
  --layer roads \
  --output build/roads.dxf \
  --source-crs EPSG:4326 \
  --target-crs EPSG:5179 \
  --profile ares-utf8
```

DXF 프로파일은 `ares-utf8`과 `ares-cp949`를 지원합니다. 자동 사전 검증과
ARES Commander 수동 검증 절차는 [ARES 검증 문서](docs/verification/ares-commander.md)에
정리되어 있으며, 실제 앱 검증 전에는 두 프로파일 모두 실험적으로 취급합니다.
MVP 요구사항별 자동·수동 검증 범위는 [MVP 체크리스트](docs/verification/mvp-checklist.md)에
정리되어 있습니다.

공간 연산은 native CLI에서 다음과 같이 실행합니다. `buffer`는 `--distance`를
사용하고, 이외의 연산은 `--right-input`을 추가로 지정합니다. 출력 확장자는
`.dxf`, `.gpkg`, `.shp` 중 하나여야 합니다. GEOS 결과가
`MULTILINESTRING` 또는 `MULTIPOLYGON`이어도 DXF exporter가 각 component를
별도 polyline entity로 기록하며, `MULTIPOINT`와 `GEOMETRYCOLLECTION`의
지원 geometry도 개별 entity로 분해해 기록합니다.

```sh
./build/gis-cli spatial \
  --operation buffer \
  --input data/points.gpkg \
  --layer points \
  --output build/points-buffer.dxf \
  --distance 10
```

여러 입력을 병합하려면 `--input`을 반복하고, 레이어 이름을 지정할 때는
입력 순서와 같은 순서로 `--layer`를 반복합니다. 병합 CLI는 데이터셋별로
읽기 순서로 부여된 ID를 전체 결과에서 유일하도록 재부여합니다.

```sh
./build/gis-cli merge \
  --input data/roads-a.gpkg --layer roads \
  --input data/roads-b.gpkg --layer roads \
  --output build/roads-merged.gpkg
```

## 작업 규칙

기본 규칙은 [agents.md](agents.md)를 따릅니다. 특히 Codex나 자동화 도구는 커밋을 만들지 않으며, 모든 커밋은 사용자가 직접 주도해야 합니다. 커밋이 필요할 때는 사용자의 로컬 Git에 설정된 기본 GPG 키로 서명해야 합니다.

현재 변경 묶음의 검토·검증·사용자 커밋 절차는 [커밋 준비 기록](docs/commit-prep.md)에 정리되어 있습니다.
