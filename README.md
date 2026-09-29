# GoGIS

업무 특화 2D 벡터 GIS 데스크톱 애플리케이션을 Go 중심으로 개발하는 저장소입니다.

현재 저장소는 구현 착수를 위한 최소 뼈대입니다. 전체 목표와 MVP 범위는 [desktop-gis-codex-brief.md](desktop-gis-codex-brief.md), 기술 선택과 보류된 결정은 [기술 스택 결정 기록](docs/decisions/0001-tech-stack.md)에서 확인할 수 있습니다.

## 선택한 기술 스택

- Go: GIS 도메인 모델, 공용 명령/API, CLI, 작업 취소·진행률 제어
- GDAL/OGR: SHP·GeoPackage 등 벡터 입출력
- PROJ: CRS 확인과 좌표 변환
- GEOS: 교차·합집합·차집합·버퍼 등 공간 연산
- PostgreSQL/PostGIS: 트랜잭션 기반 DB 읽기·쓰기
- DXF exporter: 자체 포맷 경계와 실제 ARES Commander 검증을 거쳐 채택
- GUI: Qt 바인딩과 Wails+WebGL/WebGPU를 수직 프로토타입으로 비교한 뒤 결정
- Lua: 안전한 공개 명령 API 위에 최소 스크립팅 계층으로 추가

네이티브 의존성은 Go 코어에 직접 섞지 않고 `drivers/` 경계에 둡니다. 초기 빌드에는 외부 의존성을 넣지 않아 CLI 뼈대가 어떤 개발 환경에서도 컴파일되도록 했습니다.

## 시작하기

```sh
go run ./cmd/gis-cli --help
go test ./...
```

현재 CLI는 프로젝트 뼈대 확인용 `--help`와 버전 출력을 제공합니다. 실제 SHP → PROJ → DXF 수직 관통 경로는 마일스톤 A에서 추가합니다.

## 작업 규칙

기본 규칙은 [agents.md](agents.md)를 따릅니다. 특히 Codex나 자동화 도구는 커밋을 만들지 않으며, 모든 커밋은 사용자가 직접 주도해야 합니다. 커밋이 필요할 때는 사용자의 로컬 Git에 설정된 기본 GPG 키로 서명해야 합니다.
