# ADR 0016: Milestone C PostGIS CLI 경계

## 상태

Accepted — 2026-10-23

## 결정

Milestone C의 PostGIS 애플리케이션 연결은 QGIS식 공간 테이블 목록과 CLI 명령으로
제공한다. `postgis-list`는 `geometry_columns`와 PostgreSQL metadata를 이용해
공간 테이블만 반환하고, `postgis-import`/`postgis-export`는 레이어를 입출력한다.
`postgis-insert`/`postgis-delete`는 한 레코드씩 별도 트랜잭션으로 추가·삭제한다.
명령은 `drivers/postgis.Store`를 직접 노출하지 않고 DSN과 검증된 schema/table
식별자만 받는다.

공용 GIS 모델과 트랜잭션 경계는 유지한다. PostGIS 인증 정보는 명령 인자나
저장소 파일에 기록하지 않으며, DSN은 실행 환경에서 주입한다. 기본 빌드는
네이티브 GDAL/PROJ/GEOS가 필요하므로 `native` 빌드 태그가 없는 경우 명확한
안내 오류를 반환한다.

## 이유와 제한

- PostGIS 드라이버의 기존 rollback 테스트와 동일한 저장 경계를 재사용한다.
- CLI 배치 작업은 GUI와 독립적으로 자동화할 수 있어 Milestone C parity fixture의
  기준 경로가 된다.
- 레코드 삭제는 대상 ID가 없으면 오류를 반환하며, 추가·삭제 모두 commit 전
  실패 시 rollback된다.
- 데스크톱은 `--postgis-table`로 선택한 테이블을 직접 열고 기존 레코드 편집
  UI와 저장 경계를 재사용한다. DSN은 `GOGIS_POSTGIS_DSN` 환경 변수로만
  주입하며 workspace에는 저장하지 않는다.
- 실시간 viewport 조회는 전체 레이어 메모리 읽기 제한을 우회하지 않는다. 큰
  PostGIS 데이터셋에는 후속 bounded/viewport reader가 필요하다.

## 사용 예

```sh
gis-cli postgis-import --input roads.gpkg --dsn "$GOGIS_TEST_POSTGIS_DSN" --table public.roads --layer roads
gis-cli postgis-export --dsn "$GOGIS_TEST_POSTGIS_DSN" --table public.roads --output roads.dxf
```
