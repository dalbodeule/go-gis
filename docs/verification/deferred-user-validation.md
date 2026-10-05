# 사용자 개입 후속 확인 목록

자동으로 검증할 수 없는 CAD 호환성, 대상 OS 설치 동작, 네트워크가 필요한
취약점 조회를 한곳에 모은다. 자동 검증 결과와 실제 사용자 환경의 결과를
혼동하지 않도록 각 항목에 환경과 증거를 기록한다.

현재 데스크톱 변경의 범위·검증 결과·사용자 화면 확인 항목은
[커밋 준비 문서](../commit-prep.md)에 요약했다. 아래는 시점별 상세 기록이며,
오프스크린 QML/Qt 테스트와 실제 사용자의 화면 확인을 구별해야 한다.

`GO-2026-5970`은 잘못된 UTF-8 입력에서 `golang.org/x/text/unicode/norm.Iter`가 무한 루프에
빠질 수 있다고 보고하며, 수정 경계는 `golang.org/x/text v0.39.0`이다. 저장소를 `v0.42.0`으로
올렸다(Go 1.27.1 환경). 해당 모듈이 요구하는 `golang.org/x/sync`도 v0.23.0으로 갱신됐다.
`norm.Iter`에 invalid/truncated UTF-8 네 입력을 주고 제한시간 안에 끝나며 매 iteration마다
byte position이 전진하는 회귀 테스트를 추가했다. `go mod verify`, 일반/native 전체 테스트,
Qt/native 데스크톱 테스트와 일반 `go vet`가 통과했다.
초기 sandbox 환경의 `govulncheck` 실행은 `vuln.go.dev` DNS 조회 실패로 끝났다. 2026-10-02에
승인된 네트워크 실행으로 공식 DB 검사를 완료했으며 결과와 후속 모듈 수정은 아래에 기록한다.
공식 [Go 취약점 기록](https://pkg.go.dev/vuln/GO-2026-5970)과 이슈
[#80142](https://github.com/golang/go/issues/80142)을 근거로 삼았다.

2026-10-02 공식 DB 재검사(승인된 네트워크 실행): 루트 `./...`는 취약점 0건이었다.
별도 nested module `third_party/godal`에서는 초기 스캔에서 gRPC, `x/net`, OpenTelemetry,
`x/crypto` 및 Windows `x/sys` 구버전 취약점이 발견됐다. 이를 gRPC v1.83.2, `x/net` v0.58.0,
`x/crypto` v0.56.0, `x/sys` v0.47.0, OTel SDK v1.44.0 및 호환 전이 버전으로 올렸다.
최종 nested 스캔은 symbol-level 취약점 0건, package-level 추가 취약점 0건이었다.
남은 module-level 결과는 GO-2026-5932 (`x/crypto/openpgp`, 수정 버전 없음) 하나이며,
`go list -deps ./...`에는 OpenPGP 패키지가 없고 실제 사용 패키지는 cryptobyte/chacha20 계열이다.
따라서 이는 현재 import 경로의 취약점으로 보고되지는 않지만, nested module의 요구 모듈에 대한
DB 경고로 기록한다. [공식 advisory](https://pkg.go.dev/vuln/GO-2026-5932).

2026-10-02 재실행에서 승인된 네트워크 권한으로 공식 DB 검사를 갱신했다. 루트 portable 및
`-tags native` 검사는 각각 `No vulnerabilities found`; nested GODAL은 도달 가능한 취약점 0건과
호출되지 않는 module-level 결과 1건(GO-2026-5932)으로 종료 코드 0을 반환했다. 이는 CI에 nested
scan을 추가한 뒤 얻은 로컬 scan 증거이며, GitHub CI job 자체의 실행 결과와는 구분한다.

업데이트 후 nested `go vet ./...`, `go test ./... -skip '^TestVSIGCSNoAuth$'`, race 테스트,
`go mod verify`, `go mod tidy -diff`가 통과했다. 제외 테스트만 Google Cloud fixture가 필요해
실행하지 못했다. 루트 `scripts/verify.sh`, `go mod verify`, `go mod tidy -diff`와 공식 DB scan도
통과했다.

전체 GODAL test가 repository root에 `none`이라는 이름의 GTiff를 남기던
`TestViewshedCreationOptions`를 찾아, source/output 파일을 `t.TempDir()` 아래의 고유 경로로
분리했다. 해당 테스트와 nested vet/test/race가 통과했고, 전체 nested test 후 root artifact가
생성되지 않는 것을 확인했다.

2026-10-02 재확인: 루트 및 `third_party/godal` 모듈 그래프 모두
`golang.org/x/text v0.42.0`, `golang.org/x/sync v0.23.0`을 선택하며 취약 범위의 x/text 버전은
없다. 구형 x/text v0.29.0 checksum도 루트 `go.sum`에서 제거했다. 공식 DB의 루트 및 nested
모듈 결과는 위에 분리해 기록했다.

같은 날 `GOGIS_TEST_REPEATED_VIEWPORT_ABOVE_INDEX_CAP=1` stress test를 다시 실행했다.
1,000,001-point GeoJSON에서 128회 viewport 이동, 누적 1,952 window hit/label, peak RSS
188 MiB로 통과했다(Apple M3, 단일 실행). 이 수치는 synthetic point fixture와 Go 프로세스의
peak RSS이며 polygon-heavy 전국 실데이터, Qt/GPU 메모리 또는 UI frame rate를 보증하지 않는다.
중첩 `third_party/godal` 전체 테스트도 `go test ./... -skip '^TestVSIGCSNoAuth$'`로 통과했다.
제외한 테스트는 Google Cloud의 원격 fixture를 내려받아야 해 DNS 제한에서 실행할 수 없다.
전체 테스트에서 기존 GDAL 0-pixel 폭 RasterIO debug callback이 새 dimension guard에 의해
거부되는 회귀를 발견했다. 음수 크기는 계속 차단하면서 0폭 요청을 bounded buffer 검사와 함께
허용하도록 수정했고 관련 RasterIO 및 callback 테스트가 통과했다.

2026-10-02 bounded fuzz 재실행: core WKB decoder 679,603회, render WKB fast path 686,323회,
GDAL GeoJSON geometry bounds 527,754회, GeoJSON properties 528,842회, GeoJSON sequence
scanner 7,073회, PostGIS property byte estimator 51,754회 입력에서 모두 panic/failure 없이
통과했다. 새 interesting input은 Go fuzz cache에 기록됐고 worktree에는 failure corpus가
생성되지 않았다. 이 시간제한 실행들은 sanitizer/OOM stress나 native GDAL C/C++ fuzzing을
대체하지 않는다.

## 자동 검증 현황 (2026-10-01, macOS ARM64)

`GOCACHE=/private/tmp/gogis-go-cache ./scripts/verify.sh` 통과: 일반 Go 테스트와
vet, race 테스트, native 태그 테스트/race 테스트, native 빌드, Qt native 테스트,
QML 테스트 12개, `git diff --check`. 테스트 중 `Sans Serif` 대체 폰트 관련 Qt
경고 1건이 있었지만 실패 테스트는 없었다. 별도의 키/토큰 형태 secret 패턴 검색은
일치 항목이 없었다(휴리스틱 검색이며 secret scanner 전체 검사를 대체하지 않는다).
`go mod verify`도 통과했다. 최초 로컬 Go 취약점 DB 조회는 DNS 차단으로 실패했지만,
2026-10-02 승인된 네트워크 실행에서 루트 및 nested GODAL 검사를 완료했다(위 결과 참조).
현재 CI workflow는 portable/native 루트와 nested GODAL 모듈을 검사하도록 구성되어 있다;
새 nested 검사 step은 아직 CI에서 실행된 결과가 없다.

복잡 입력 회귀 확인으로 GeoJSON `GeometryCollection` 재귀를 64단계로 제한하고,
65단계는 거부하는 테스트를 추가했다. GDAL GeoJSON geometry bounds 퍼즈 타깃은 약
144만 입력, WKB render parser 퍼즈 타깃은 약 235만 입력을 각각 처리하며 panic 없이
통과했다. WKB 좌표의 NaN/Inf가 정규화 extent로 전파되지 않도록 렌더 입력 검사도
추가했고, Point와 LineString의 무한 좌표 회귀 테스트를 통과했다. 이는 parser
panic/재귀·좌표 방어 확인이지 native GDAL/Qt/GPU의 메모리 상한이나 모든 포맷의
fuzz coverage를 입증하지 않는다.

PostGIS의 원자적 layer 교체 회귀 테스트는 `GOGIS_TEST_POSTGIS_DSN`을 명시한 경우에만
실제 DB에 연결한다. `.github/workflows/verify.yml`의 별도 PostGIS service job에서
성공 snapshot 저장 후 교체 insert 중 의도적 geometry 오류를 내고, 이전 snapshot이
그대로 복구되는지 검사한다. 현재 로컬은 PostgreSQL/PostGIS service가 없어 테스트를
실행할 수 없었으며, 로컬 `go test ./drivers/postgis`는 일반 단위 테스트만 확인한다.
CI job의 실제 실행 결과가 나오기 전까지 live transaction 검증은 미완료로 취급한다.

## 세종 SHP overlay 재현 결과

Downloads의 두 원본은 GDAL에서 모두 EPSG:5186으로 읽혔다. 도근점 파일은
11,971개 Point, 연속지적도는 208,015개 Polygon이며, native read-only 로더에서
두 파일을 함께 여는 테스트는 219,986개 feature를 약 5.7초에 적재했다. test-only
Go heap profile에서 fill mesh 준비가 가장 큰 할당 경로였다. 최적화 전 일회성
기준 약 3,931 MiB 누적 할당/488 MiB GC 후 heap에서, 색상 재계산 제거·타일 fill
buffer 사전 용량 예약·단순 convex ring triangulation fast path 후 약 2,128 MiB/
485 MiB가 됐고, polygon buffer capacity 추정치를 좁힌 최종 실행은 약 2,093 MiB/
449 MiB였다. process max RSS는 초기 일회 실행에서 약 1,544 MiB, 최종 실행에서
약 1,091 MiB였지만 측정은 각각 한 번이며 변동 가능하다. 최종 runtime benchmark는
10K convex polygon당 약 1.31 MB/op, 270 ns/feature였다. 실제 GUI/Qt scene graph
상호작용은 테스트하지 않았으므로 사용자가 앱에서 같은 파일을 열어 확인해야 한다.

2026-10-01 현재 체크아웃의 `TestWindowedReadOnlyLargeSourceIntegration`에 두 원본 SHP를
직접 지정해 다시 실행했다. feature count는 위와 동일했고, 합쳐진 read-only runtime은
geometry snapshot을 보유하지 않았다. 연속지적도는 거친 viewport chunk에서 20,000 feature
상한을 오류로 반환했고, bucket 4의 첫 허용 chunk는 8,962 hits/735,647 vertices를 생성했다.
같은 테스트에서 도근점은 bucket 0 chunk에 1 hit/2 vertices를 만들었다. 프로세스 peak RSS는
155 MiB, 테스트 본문은 약 1.75초였고 통과했다. 두 SHP에는 `.qix` sidecar가 없어 이 실행은
공간 필터의 sequential-scan 조건도 포함한다. 단, 테스트는 각 레이어의 첫 readable chunk만
만들며 실제 QML 화면·GPU scene graph를 띄우거나 crash report의 SIGABRT를 재현하지는 않는다.
2026-10-02 테스트를 확장해 각 실제 SHP에서 128회씩, 총 256회 viewport chunk 이동 뒤에도
활성 chunk·hit/ID/name/label map·feature/payload 상태가 상한 안에 남는지 매 이동마다 검사했다.
최신 재실행은 일반 모드에서 테스트 본문 3.15초/peak RSS 170 MiB, race detector에서 8.91초/
403 MiB였고 모두 통과했다. 과밀 창은 20,000-feature 안전 오류로 거부될 수 있으며, race 실행의
높은 RSS는 sanitizer overhead를 포함한다. 이 결과는 실제 Go 로더의 반복 chunk·캐시 경로 검증이지 Qt
화면/GPU 조작이나 GUI crash 원인 재현, 전국 규모 자원 상한의 증명은 아니다.

2026-10-02 native 앱 직접 실행도 시도했다. 화면 세션이 없는 기본 platform plugin에서는
`QQmlApplicationEngine.LoadData` 중 `Cannot create window: no screens available`로 abort했고,
이 시점은 initial data loading 호출 전이라 SHP 처리 실패가 아니다. 같은 연속지적도 SHP를
`QT_QPA_PLATFORM=offscreen QT_QUICK_BACKEND=software`로 실행하자 QML 앱은 12초 동안 abort나
load-error 로그 없이 유지되어 timeout exit 124로 종료됐다. 2026-10-02 재실행은
`GOGIS_PERF=1 QSG_RENDER_LOOP=basic`을 추가해 약 21초 관찰했다. `load-start`는 기록됐으나
`vertices-published` 이벤트가 없었고, 터미널에서 수동 종료(exit 130)했다. 따라서 이 실행도
실제 window·완료된 SHP 렌더·GPU/scene graph·RSS를 검증하지 않는다.
2026-10-02 후속 화면 검증에서 CUA는 앱 목록을 읽었지만 화면 surface를 제공하지 않았다.
현재 빌드(`/tmp/gogis-desktop-ui-check`)를 세종 연속지적도 SHP와 함께 실행하자 Qt가
`Cannot create window: no screens available`를 출력하고 `QQmlApplicationEngine.LoadData` 호출 중
SIGABRT(exit 2)로 종료됐다. 세종 파일 로딩 전의 플랫폼 창 생성 실패이므로 SHP 처리 경로의
실패 증거는 아니다. 이 실행에서 실제 UI·RSS 표시·pan/zoom·GPU 검증은 여전히 미완료다.
첨부 crash report(2026-09-30)는 `EXC_CRASH/SIGABRT`이며 triggered thread가 `CVDisplayLink`다.
그 스레드의 보이는 stack은 `pthread_kill → raise → Go runtime.raise_trampoline`이다. 로컬
Go 1.27.1 런타임 소스에서 `raise_trampoline`은 foreign thread가 처리하지 않은 신호를 기본
동작으로 다시 전달하는 경로로 확인했다. 따라서 이 frame은 SIGABRT를 발생시킨 원래 함수나
Go 런타임 결함을 특정하지 않는다. 별도 Qt `QSGRenderThread`가 당시
`GoGISMapCanvas::updatePaintNode` 실행 중이었던 것은 동시 실행 정황일 뿐 인과 증거가 아니다.
report에는 `EXC_RESOURCE`, allocator 실패, 또는 명시적 OOM 진단이 없어 OOM이라고 확정할 수 없다.
이는 실제 앱 실행 crash지만 현재 소스에서 원래 SIGABRT 발생 지점을 재현하지 못했다.
화면이 있는 macOS 로그인 세션이나 Windows
사용자 환경에서 실제 UI·RSS·pan/zoom 및 threaded/basic render-loop 비교 검증이 필요하다.

2026-10-02 후속 headless 검증에서 `QT_QPA_PLATFORM=minimal QT_QUICK_BACKEND=software
QSG_RENDER_LOOP=basic` 조합은 첫 실행에 화면 없이 Qt Quick software scene graph를 생성했고,
preview 147,045 vertices publish 후 `updatePaintNode` 계측이 273,585 vertices로 128–134 ms에
기록됐다. 하지만 같은 SHP를 연 두 번째 실행은 120초 timeout까지 `load-start`만 기록했고,
preview/full publish가 없었다. 앞선 45초 실행은 preview scene graph까지 갔지만 full render 전
timeout이었다. 따라서 headless backend가 일부 장면을 그릴 수 있다는 증거이지, 초기 SHP 로드의
재현 가능한 완료·실제 화면 표시·crash 부재 보증은 아니다.

재현 시 `GOGIS_TEST_LARGE_VECTOR_SOURCES`에 SHP 경로 두 개 이상을 OS path-list 구분자로
지정한다. 아래 예시는 macOS/Linux 기준이며 race flag를 빼면 일반 실행이다.

```sh
GOGIS_TEST_LARGE_VECTOR_SOURCES='/path/to/cadastre.shp:/path/to/control-points.shp' \
  CGO_CXXFLAGS=-std=c++17 go test -race -tags 'qt native' ./cmd/gis-desktop \
  -run '^TestWindowedReadOnlyLargeSourceIntegration$' -count=1 -v
```

대형 또는 feature count 미상 파일을 저장 경로가 없는 기존 editable project에 추가하면,
기존 in-memory 레이어와 속성을 보존한 채 project 전체를 read-only viewport runtime으로
전환한다. 신규 source는 chunk query로 읽으며 base layer hit-test와 attribute paging도 유지한다.
회귀 테스트는 100k 초과 GeoJSON append, 기존 레이어 속성 및 hit feature 유지, ID 충돌 회피를
검증한다. 저장 destination이 설정된 프로젝트는 자동 read-only 전환 경로에 들어가지 않으며,
materialized reader의 100k feature/128 MiB per-source 및 desktop project 누적 한도가 적용된다.
이는 저장/편집 의미를 바꾸지 않도록 한 선택이며 저장 경로가 있는 100만 feature 편집 지원을
의미하지 않는다.

도근점 파일의 두 점이 나머지 세종 EPSG:5186 범위에서 크게 벗어나며, combined
extent를 넓히는 것을 확인했다. 원본 데이터를 자동 수정하거나 해당 피처를
제외하지 않았다. 좌표가 잘못된 값인지 다른 기준/단위인지 데이터 공급자와
확인하고, 필요하면 복사본에서 정정한 후 화면 범위와 crash 재현을 비교한다.
첨부된 macOS crash report는 `SIGABRT`지만 OOM/jetsam 또는 invalid-memory access를
직접 증명하지 않고 panic 원인 문구도 없어, 보고된 crash의 확정 원인은 아직
미상이다. report에서 실제 crashed thread는 `CVDisplayLink`이며 native stack은
`runtime.raise_trampoline`에서 `raise(SIGABRT)`로 끝난다. 별도의 `QSGRenderThread`는
report snapshot 시점에 `GoGISMapCanvas::updatePaintNode`의 vertex-conversion lambda 안에
있지만, 그 thread는 crash로 표시되지 않았고 그 register state/fault address도 보고서에
없다. 따라서 이를 null write의 증거로 해석하면 안 된다. Renderer vertex 상한·allocation
결과 검사는 독립 코드 검토에서 확인한 예방 방어이며, 이 SIGABRT의 원인이나 재현된 수정으로
간주하지 않는다.

대용량 지원은 viewport 기반으로 한 단계 연결했다. CRS와 extent가 알려진 레이어는
read-only 로더가 시작할 때 geometry를 적재하지 않고 renderer가 요청한 chunk 범위만
원본 CRS에서 retained `AttributeSession.OpenWindowWithLimits`로 조회한 뒤 display CRS로
변환한다. 선택 이름은
window snapshot에서 가져오므로 OGR SQL 결과의 query-local ID를 전체 레이어 순번으로
오인하지 않는다. 공간창당 20,000 피처/32 MiB, 표시 viewport 전체 100,000 피처/128 MiB
상한을 두고,
초과 시 조용히 자르지 않고 해당 렌더 청크를 오류로 처리해 확대 안내를 표시한다.
지나간 chunk의 geometry/hit/label/cache는 viewport 이동 때 제거하며, 중복 라벨은
합친다. GeoPackage 창별 로드 및 속성명 연결 테스트가 통과했다.

GDAL→Go window 읽기는 WKB/property payload 예산을 feature 단위로 누적 검사해 상한을 넘는
feature를 result slice에 보관하지 않고 요청 전체를 오류 처리한다. GODAL의 `Geometry.WKBSize()`로
OGR geometry의 WKB 출력 크기를 버퍼 할당 전에 조회하고, 8 MiB를 넘으면 WKB exporter가 출력
버퍼를 만들기 전에 거부한다. 회귀 테스트는 초과 geometry의 크기 조회 및 사전 거부를 확인한다.
다만 OGR geometry 자체는 `NextFeature()`가 반환되기 전에 GDAL 드라이버가 이미 읽어 native
메모리에 구성할 수 있으므로 이 WKB 상한은 입력 geometry의 native 메모리 상한이 아니다.
WKB 출력 버퍼의 C `malloc` 실패는 exporter에 null 포인터를 넘기지 않고 GDAL 오류로 반환한다.
저메모리 상태에서 GDAL 오류 문자열의 `malloc`/`realloc` 실패도 null write로
이어지지 않도록 처리했고, raster band/layer 목록 및 color table 복사 버퍼의 C 할당 실패와
크기 산술 오버플로 검사도 추가했다. 이들 실패 경로를 실제 시스템 OOM으로 강제 주입한 것은
아니며, 성공 경로 빌드와 native 테스트 통과만 확인했다.
추가 검토에서 GDAL 호출마다 쓰는 작은 오류 컨텍스트도 `C.malloc` 결과를 확인하지 않고
역참조한다는 점을 발견해, 동기 C 호출 중에만 보관되는 C-layout 값을 Go 소유 메모리로 옮겼다.
native GDAL 테스트, `GOEXPERIMENT=cgocheck2` 검사, GDAL race 테스트가 통과했다. 강제 allocator
실패 주입은 하지 않았으므로 이는 null 역참조 원인의 제거 근거이지, 전체 프로세스 OOM 안전성
보증은 아니다.
GDAL Go 바인딩에 남아 있던 `C.CString` 호출 중 외부 dataset 경로, plugin 이름, WKT 및 GeoJSON
geometry 생성, 사용자 CRS, SQL query, scalar string field 문자열은 nil을 검사해 Go 오류로
돌려주는 C-copy helper로 교체했다. helper는 Go 문자열을 동기 복사만 하고 C heap pointer를
반환한다. `GDALOpenEx`뿐 아니라 rasterize/vector translate, VRT, grid/dem/viewshed/nearblack,
geometry GML, GCP 처리, VSI, metadata, field/layer 처리의 문자열도 검사한다. 옵션 string array는
pointer array와 각 원소 할당 오류를 확인한다. `createCGOContext`도 검사된 변환기를 사용하며
할당 오류를 기록해 `close()`가 돌려준다. 단, 현 C wrapper는 `close()` 전에 native API를 실행하므로
컨텍스트 옵션 할당 실패 시 native 호출을 사전 차단하지는 못한다. 강제 allocator 실패 주입은
하지 않았고, 전체 프로세스 OOM 안전성을 보장하지 않는다.
추가 C++ 검토에서 사용되지 않는 내부 driver registration helper의 unchecked `calloc` 및
`snprintf`에 포인터 크기를 전달하던 버그를 수정했고, VSI callback 오류 메시지가 할당 실패로
null일 때도 안전한 기본 문구를 사용한다.
또한 VSI `ReadAtMulti` fallback의 요청 범위당 고루틴 생성은 `min(GOMAXPROCS × 4, 64)` worker로 제한했다.
4,096개 범위 회귀 테스트로 동시 실행 상한과 결과 개수를 검증했다. 사용자 구현의 `KeyMultiReader`
경로는 해당 구현이 자체 동시성을 제한해야 한다. C→Go VSI callback은 범위 개수(최대 65,536),
개별 읽기 길이(최대 256 MiB), Go `int` 표현 가능성, nil buffer 및 int64 offset 범위를 검사한다.
일반 `ReadAt`와 사용자 제공 `KeyMultiReader` 모두 결과 바이트 수가 요청 buffer 범위를 벗어나지 않는지
검증한다.
사용자 VSI handler의 `ReadAt`, `Size`, `ReadAtMulti` panic은 callback 경계에서 오류로 바꾸며,
handler 반환 error의 `Error()`가 panic하는 경우도 C 경계로 전파하지 않는다. Size의 음수 응답도
실패로 처리한다.
GDAL Go API에서도 빈 Warp 입력·nil source dataset 및 좌표 변환의 불일치 slice 길이는 native
호출이나 slice 인덱싱 전에 오류로 돌려주며, 빈 binary field는 nil data pointer와 길이 0으로 전달한다.
RasterIO/GridCreate는 이제 지원하지 않는 buffer type, 빈/작은 buffer, 0/음수 또는 C int 범위를 벗어난
dimension, stride/span 곱셈 overflow를 panic/포인터 역참조 전에 error로 반환한다.
Window-backed read-only rendering은 concurrent chunk builder를 두 개로 제한한다. 이 제한은
최대 두 개의 window 작업이 동시에 진행되도록 할 뿐, 각 작업의 WKB serialization·geometry
decode·GEOS 출력 또는 전체 RSS에 정확한 byte 상한을 보장하지 않는다. Read-only window는
polygon coordinate 총량 250,000개, GEOS triangulation 경계는 단일 WKB geometry coordinate
250,000개/WKB 8 MiB 및 WKT 1,000,000 bytes로 제한하고, WKB 구조 element는 1,000,000개,
triangle 반환 slice는 250,000개로 제한한다. WKB `PointCount`도 빈 ring/child 남용을 포함한
구조 요소 수를 검증한다. 이들은 GEOS 호출 전 입력 복잡도 guard이며 GDAL에서 이미 수행된 WKB
materialization이나 GEOS native allocation 자체의 정확한 byte quota는 아니다.

아직 전국 규모 안전성이나 세종 자료의 화면 동작을 보장하지 않는다. 혼합된 알려진 CRS도
window 변환으로 지원한다. CRS 또는 extent가 없어서 window query가 불가능한 read-only layer는
OOM 위험을 피하기 위해 전체-geometry loader로 fallback하지 않고, `--source-crs` 지정 또는
dataset metadata 수정 안내와 함께 거부한다. byte budget은 Go에
보관할 직렬화 geometry/property/label 추정치이며 GDAL 내부 geometry, QML/GPU 메모리나 단일
피처를 읽어들이는 순간의 최대 RSS를 제한하지 않는다. 초기 전체 extent가 보이는 zoom에서는
많은 chunk가 활성화될 수 있다. 전국 자료 OOM 안전성의 완료 기준에는 혼합
거대 단일 geometry/속성의 byte 상한 또는 zoom 기반 LOD, 세종 SHP GUI 실제
pan/zoom, 대용량 fixture의 최대 RSS 측정이 여전히 포함된다. `SIGABRT` signal 전달의
정확한 원인과 report 실행 파일이 현재 소스와 동일한지는 아직 검증되지 않았다.

### 100만 피처 synthetic stress benchmark

macOS ARM64 Apple M3에서 100만 개의 균등 분포 Point를 갖는 약 100 MB급 GeoJSON을
streaming 방식으로 생성하고 native desktop 로딩 정책과 첫 viewport chunk를 측정했다.
새 `BenchmarkDesktopReadOnlyLoadGeoJSON1M`는 실제 파일을 열어 10만 threshold에서
자동 read-only 전환되는지, 전체 feature snapshot을 보유하지 않는지, bucket 2의 한
viewport chunk가 20,000 feature/32 MiB 및 visible viewport budget 안에 들어오는지를
확인한다. 재현 명령:

```sh
CGO_CXXFLAGS=-std=c++17 go test -tags 'qt native' ./cmd/gis-desktop \
  -run '^$' -bench '^BenchmarkDesktopReadOnlyLoadGeoJSON1M$' \
  -benchmem -benchtime=1x -count=1
```

첫 창뿐 아니라 반복 패닝으로 이전 geometry가 누적되지 않는지도 opt-in stress test로
확인한다. 100만 피처 synthetic GeoJSON을 열고 bucket 6의 서로 다른 viewport chunk를
128회 순차 조회하며, 매 이동 때 현재 chunk 외의 hit geometry·labels·feature names가
제거되는지와 viewport 예산을 검사한다.

```sh
GOGIS_TEST_REPEATED_VIEWPORT_1M=1 CGO_CXXFLAGS=-std=c++17 \
  go test -tags 'qt native' ./cmd/gis-desktop \
  -run '^TestWindowedReadOnlyRepeatedViewportMoves1M$' -v -count=1
```

Apple M3에서 세 차례 통과: 각각 128회 이동, 누적 1,952개 window hit, peak RSS 210, 224,
243 MiB. 이 값에는 100만 피처 GeoJSON metadata index가 포함되며 `getrusage` 기반
프로세스 peak다. 각 query의
임시 할당, Qt/GPU 메모리, 실제 UI frame rate 또는 실제 지도 화면 조작을 검증하지 않는다.

Index cap 초과 경로는 별도 opt-in test로 1,000,001 feature에서 같은 128회 이동을 검증한다.

```sh
GOGIS_TEST_REPEATED_VIEWPORT_ABOVE_INDEX_CAP=1 CGO_CXXFLAGS=-std=c++17 \
  go test -tags 'qt native' ./cmd/gis-desktop \
  -run '^TestWindowedReadOnlyRepeatedViewportMovesAboveIndexCap$' -v -count=1
```

GDAL GeoJSONSeq driver의 newline-delimited `.geojsonl`도 같은 million point로
대조했다. [GDAL GeoJSONSeq 문서](https://gdal.org/en/stable/drivers/vector/geojsonseq.html)는
FeatureCollection보다 incremental parsing에 적합한 형식이라고 설명하지만, 현재 GDAL
파이프라인의 실제 결과는 더 나빴다. metadata 전용 실행에서 등록 직후 약 60 MiB였던
peak RSS가 open/count 이후 2,407 MiB, bounds 이후 4,749 MiB로 상승했고, session 재사용을
반영한 desktop read-only + 첫 window 실행에서는 7,525 MiB 및 7.35초를 기록했다. 따라서 지금은
`.geojsonl` 변환을 메모리 절감 방안으로 권하지 않는다. 이는 형식의 특성이 현재
desktop/GDAL 경로의 자원 사용을 보증하지 않는 사례이며, 원인 분석과 별도 최적화가
필요하다.

동일 측정은 다음 명령으로 재현한다.

```sh
CGO_CXXFLAGS=-std=c++17 go test -tags 'qt native' ./cmd/gis-desktop \
  -run '^$' -bench '^BenchmarkDesktopReadOnlyLoadGeoJSONSeq1M$' \
  -benchmem -benchtime=1x -count=1
```

일반 GeoJSON과 GeoJSONSeq의 GDAL open에는 단일 JSON feature 최대 크기를 64 MiB로 제한했다.
이를 넘는 feature는 GDAL 오류로 거부되며, 1 MiB 한도로 구성한 회귀 테스트에서 초과
feature가 open 단계에서 실패함을 확인했다. 이 제한은 비정상적으로 큰 단일 feature의
파싱 위험만 낮춘다. 전체 GeoJSON FeatureCollection이 차지하는 native 메모리에는 상한을
제공하지 않으며, 위에서 관측된 수 GiB peak RSS를 해결하지 않는다.

2026-10-01 현재 checkout에서 같은 Apple M3로 두 벤치마크를 각각 `-benchtime=1x`로 다시
실행했다. FeatureCollection은 1.347 s/op, 첫 viewport window 32.75 ms, peak RSS 167.3 MiB,
retained Go heap 49.23 MiB, 누적 할당 344.8 MB/3.08M allocations였다. GeoJSONSeq는
1.047 s/op, 첫 viewport 34.04 ms, peak RSS 205.6 MiB, retained heap 49.23 MiB,
누적 할당 343.8 MB/3.08M allocations였다. 두 경로 모두 정확히 1,000,000 feature를 확인하고,
첫 창에는 3,969 feature/약 0.268 MiB를 유지했다. 각각 단일 실행이므로 이전 측정과의 차이를
성능 회귀 또는 안정된 peak 상한으로 단정하지 않는다. 첫 window 경계는 통과했으나 복잡 polygon,
다른 OS, Qt/GPU scene graph와 전국 단위 실데이터의 메모리 상한은 별도 검증이 필요하다.

동일한 피처 수의 indexed GeoPackage 비교:

```sh
CGO_CXXFLAGS=-std=c++17 go test -tags 'qt native' ./cmd/gis-desktop \
  -run '^$' -bench '^BenchmarkDesktopReadOnlyLoadGeoPackage1M$' \
  -benchmem -benchtime=1x -count=1
```

SHP spatial index의 유무를 비교하려면 `BenchmarkDesktopReadOnlyLoadShapefile1M`와
`BenchmarkDesktopReadOnlyLoadShapefile1MNoIndex`를 각각 실행한다. indexed fixture는
`.qix` sidecar의 실존을, 두 fixture 모두 exact million feature count를 검사한다.

```sh
CGO_CXXFLAGS=-std=c++17 go test -tags 'qt native' ./cmd/gis-desktop \
  -run '^$' -bench '^BenchmarkDesktopReadOnlyLoadShapefile1M(NoIndex)?$' \
  -benchmem -benchtime=1x -count=1
```

현재 per-feature envelope scan 경로의 올바른 session 재사용 단일 회차는 전체 setup+첫 chunk
2.95초, 첫 chunk 1.54초, 표시 3,969 피처/추정 payload 0.177 MiB, 누적 Go allocation
77.9 MB/op 및 약 4.06M allocations/op이었다. Darwin process peak RSS는 5,258 MiB였고,
inspection 후 2,638 MiB에서 read-only runtime 구성 직후 2,639 MiB, 첫 viewport scan 후
5,258 MiB로 상승했다. 별도 새 프로세스의 단계 측정에서는 GDAL 등록 직후 59.78 MiB,
100만 feature GeoJSON `godal.Open` 직후 2,223 MiB였다. 즉 재사용은 dataset 재오픈을
줄이지만 window 전체 scan 중의 높은 native peak를 제거하지 못한다. 강제 OGRSQL spatial-filter 비교는
전체 6.12초/첫 chunk 3.31초로 더 느렸지만 Go allocation은 5.63 MB/op/63,745회로
감소했다. 이전에는 두 GeoJSON window 방식 모두 매 viewport마다 per-feature envelope
scan을 수행했다. 결과는 1M synthetic Point dataset 하나의 Mac benchmark이지 SHP,
복잡한 polygon, 대화형 Qt frame rate의 대표값이 아니다.
benchmark에서 모든 feature를 Go에 상주시킨 snapshot은 0개였고, `runtime.GC()` 후
active runtime의 Go `HeapAlloc`은 약 1.13 MiB였다. 5.26 GiB peak는 Go retained heap과
viewport payload로 설명되지 않는 GDAL/OGR 및 기타 native memory 사용을 보여준다. 이
측정은 OOM 안전성을 보장하지 않는다. `ps`는 sandbox 권한상 차단되고
`/usr/bin/time -l`도 제한됐지만, 프로세스 자체의 `getrusage` 측정은 가능했다.

2026-10-02 baseline `BenchmarkGDALGeoJSONIndexedWindow1M`는 Apple M3에서 3회 조회 평균
13.66 ms/op, 4.05 MB/op, 약 83.4k allocations/op를 기록했다. 1,000,000개 bbox를 선형
순회해 3,969개 본문을 읽는 경로였다. 이후 이 후보 검색을 보완하기 위해 256×256 coarse
grid를 추가했다. feature당 최대 64 cell 참조, 총 2,000,000 references, overflow feature
비율, query cell 수 및 100,000 candidate 수를 제한한다. bounds가 너무 큰 feature는 항상
검사하는 overflow 목록에 두며, grid/reference/candidate 제한에 닿거나 query 범위가 넓으면
기존 선형 경로로 안전하게 fallback한다. per-session scratch의 dedupe bitmap/candidate buffer를
재사용한다.

같은 실행 묶음에서 baseline wide viewport는 14.43 ms/op, grid query는
wide viewport(3,969 features) 12.17 ms/op였고,
4.08 MB/op/83.4k allocations였고, 고배율 작은 viewport(1 feature)는 선형 3.61 ms/op에서
grid 1.24 ms/op로 약 2.9배 빨라졌다. Grid는 처음 query 시 한 번 생성되며 별도 build
benchmark는 18.54 ms/op, 4.93 MB/op였다. 이는 1M uniform-point synthetic 데이터와 Apple M3의
단일 환경 측정이다. 첫 query 지연과 추가 약 5 MiB 상주 index를 치르고 반복 작은 viewport
조회에서 이득을 얻는 tradeoff이며, complex polygon·전국 데이터·다른 OS에서의 결과는 보장하지
않는다. Differential test는 linear bbox scan과 후보 결과가 일치하는지 및 dense-overflow/wide
query fallback을 확인하며 cell을 가로지르는 bbox와 결정적 난수 128개 viewport를 대조한다.
Grid 연결 뒤 1M GeoJSON desktop benchmark는 첫 window 43.60 ms,
peak RSS 179.1 MiB를 기록했다(단일 실행; 이전 checkout 측정과 직접적인 회귀 비교는 아님).
128회 반복 viewport test와 1,000,001-feature index-cap/tail-block test 모두 각각 1,952 retained
hits/labels, peak RSS 189 MiB로 통과했다. native GDAL package vet/test/race 검증도 통과했다.
전체 `GOCACHE=/tmp/gogis-go-cache ./scripts/verify.sh`도 portable/native test 및 race,
native build, nested GODAL vet/test/race, Qt native와 QML 12개까지 통과했다.
재현 명령:

```sh
GOCACHE=/private/tmp/gogis-go-cache go test -tags native ./drivers/gdal \
  -run '^$' -bench '^BenchmarkGDALGeoJSON(Indexed|SpatialGrid)Window1M(Zoomed)?$' \
  -benchmem -benchtime=3x -count=1
```

같은 100만 피처를 GDAL `VectorTranslate`로 `SPATIAL_INDEX=YES` GeoPackage로 만든
후속 benchmark는 입력의 실제 `FeatureCount == 1,000,000`을 확인하고 변환 시간은
제외했다. Apple M3에서 desktop load+첫 window 전체는 12.1 ms, 첫 window 7.1 ms,
누적 Go allocation은 이전 회차 5.62 MB/op/63,724 allocations였고, GDAL field preflight
적용 후 재측정은 5.81 MB/op/67,691 allocations였다. 최신 첫 window는 7.80 ms,
GC 후 Go heap은 1.13 MiB였다.
같은 bucket-2 window에서 3,969 피처/0.177 MiB가 생성됐고, viewport와 chunk 예산을
넘지 않았다. fixture 생성/변환의 peak가 섞이지 않도록 별도 자식 프로세스에서 로더만
실행해 측정한 peak RSS는 69 MiB였다. 이는 GDAL GeoPackage RTree를 이용한 1M Point synthetic 경로의 기준선
이지, 100만 polygon, SHP의 `.qix`, Qt/GPU peak RSS나 실제 화면 프레임률까지 보증하지
않는다. 반복 query와 큰 geometry/property 데이터를 대상으로 한 별도 stress test가
여전히 필요하다.

동일한 million-point fixture를 GDAL로 ESRI Shapefile에 옮긴 뒤 추가 비교했다. `.qix`
sidecar가 실제 생성된 경우 desktop load+첫 window는 14.6 ms, 첫 window 8.9 ms였고,
`.qix`를 만들지 않은 대조 fixture는 각각 107.7 ms/101.5 ms였다. 첫 window는 약 11배
빨랐으며 두 경우 모두 3,969 피처/0.177 MiB, 약 5.62 MB/op Go allocation이었다.
격리 자식 프로세스의 peak RSS는 indexed/no-index 두 경우 모두 82 MiB였다.
이는 uniform point 데이터와 이 호스트에 한정한 결과지만, million-feature SHP에는
spatial index가 중요하다는 실증 근거다. 테스트는 sidecar를 가진 임시 fixture에서만
수행했고 원본 SHP에 index를 자동 생성하지 않는다. 실제 세종/전국 SHP에서 QIX 생성
비용·디스크 공간·배포 sidecar 취급 및 geometry 분포에 따른 속도는 사용자 데이터로
확인해야 한다.

2026-10-01 현재 checkout에서 같은 두 benchmark를 다시 각각 한 번 실행했다. 정확히
1,000,000 Point의 QIX SHP는 loader+첫 window 15.1 ms, 첫 window 9.22 ms, 격리 자식 peak
RSS 82 MiB였고, QIX 없는 SHP는 각각 108.1 ms, 102.8 ms, 82 MiB였다. 두 경우 Go 누적
할당은 약 5.81 MB, active runtime heap 약 1.13 MiB, window hit 3,969였다. 이는 기존
관측과 비슷한 순서의 차이를 재확인하지만, 매 경우 단일 synthetic uniform-point 실행이며
복잡한 세종 polygon이나 실제 GUI의 총 메모리/프레임률 보장은 아니다.

1,000,000개 GeoJSON index cap 바로 위의 `1,000,001`개 경계도 측정했다. 변경 전에는
인덱스가 초과 순간 통째로 폐기되어 매 viewport마다 전체 FeatureCollection을 스캔했고,
첫 창은 1,099 ms/op, loader 전체는 3.434 s/op였다. bounded prefix index를 유지하고
추가 tail만 스트리밍하는 경로로 바꾼 뒤 첫 창은 26.07 ms/op, 전체는 1.252 s/op가 됐다.
GeoJSONSeq도 같은 partial-index tail 처리를 적용했다. 1,000,001개 실행은 첫 창 26.32 ms,
전체 0.792 s였으며, 3,969 feature/0.268 MiB viewport 예산을 유지했다. 현재 단일 실행의
peak RSS는 GeoJSON 232.6 MiB, GeoJSONSeq 164.8 MiB였고, 반복 측정이 아니므로 안정된
상한으로 간주하지 않는다. 이 경계 개선은 index 메모리 상한을 늘리지 않으며, first million
entries는 메모리에 유지하고 그 이후 tail은 viewport마다 순차 검사한다.

partial-index tail 경로를 포함한 128회 반복 viewport 이동도 opt-in으로 검사했다.
1,000,001-feature GeoJSON에서 1,952 total hit/label, peak RSS 194 MiB, 1.65초로
통과했고 매 이동 후 cache key/chunk/feature/payload 상한을 확인했다. 이는 synthetic Point
데이터이며 실제 polygon mesh나 Qt/GPU scene memory/frame rate 검증은 아니다.

실제 세종시 연속지적도/도근점 SHP를 대상으로 한 opt-in native 회귀 테스트는 통과했다
(2026-10-01 재실행). 연속지적도 208,015개, 도근점 11,971개를 layer metadata로
확인했다. 연속지적도는 bucket 0–3의 시험 창에서 20,000 feature 상한을 초과해
안전하게 거부됐고, bucket 4(정규화 청크 크기 0.015625)의 첫 유효 창에서는 8,962
피처와 735,647 vertex를 만들었다. 이 실제 데이터의 처리된 창에서 단일 WKB 8 MiB
상한 초과는 관찰되지 않았다. 도근점은 기본 bucket 0의 첫 유효 창에서 1개 피처를
렌더 source까지 처리했다. 두 원본을 함께 로드하고 각 layer의 첫 유효 창을 처리한
테스트 프로세스 peak RSS는 155 MiB였다. 이는 Go test 프로세스의 `getrusage` 최대 RSS이며,
앱의 GUI/GPU 메모리나 반복 pan/zoom의 상한이 아니다. 줌에 따라 read-only 타일이 세분화되며
타일당 20,000 피처/32 MiB와 viewport당 100,000 피처/128 MiB 예산은 유지한다.
이 테스트는 실제 앱 UI를 구동하지 않으며 GPU/RSS 안전성 검증도 아니다. 세종 SHP를
실제 Qt 창에서 열어 확대·축소·패닝하고 최대 RSS를 관찰하는 작업은 사용자 확인 항목으로
남긴다.

윈도우 렌더 source는 요청한 청크의 정점만 만들고 선분을 해당 셀에 직접 clip한다.
다만 hit-test geometry는 질의 결과 전체를 보유하며, 하나의 복잡한 폴리곤에 대한 GEOS
triangulation 결과 크기는 아직 별도 제한이 아니다.

## QGIS 독립 CRS control-point 대조

PROJ geometry transform을 visualization axis order로 정규화하고 extent 변환과
축 순서를 일치시켰다. EPSG:4326 `POINT (127 37)`의 WKT XY 입력으로 자동 회귀는
다음을 기대하며, EPSG:5179/5186 점 bounds와 같은 좌표가 되는지도 검사한다.

| 대상 CRS | X (easting, m) | Y (northing, m) |
| --- | ---: | ---: |
| EPSG:5179 | 955511.809285 | 1889174.174347 |
| EPSG:5186 | 200000.000000 | 489012.955691 |

이는 현재 설치 PROJ의 선택된 coordinate operation에 대한 수치 회귀값이다. 테스트
허용오차 0.02 m는 같은 operation의 구현 회귀를 위한 값이지 측지 정확도 보증이
아니다. 로컬 `projinfo -s EPSG:4326 -t EPSG:5186`은 현재 후보 operation의 정확도를
1 m로 표시하므로 datum transformation의 실제 정확도를 2 cm로 주장하지 않는다.

brief에서 요구한 독립 비교는 아직 수행하지 않았다. QGIS가 설치된 지원 환경에서
EPSG:4326의 점 (127, 37)을 EPSG:5179 및 EPSG:5186으로 각각 재투영하고 QGIS에
표시된 XY가 위 수치와 축 순서상 일치하는지 확인한다. QGIS/PROJ 버전, 선택된
datum operation 및 필요한 grid 여부, 표시한 값과 GoGIS 출력값을 기록한다. 차이가
1 m 이내더라도 operation 선택 차이일 수 있으므로 자동으로 무시하지 말고 QGIS의
coordinate operation 설정과 PROJ database/data 경로를 대조한다.

2026-10-01 현재 개발 Mac에서 `qgis`, `qgis_process`, QGIS application bundle,
Python의 `qgis.core`/`pyproj`를 확인했으나 사용할 수 있는 QGIS/PyQGIS가 없었다.
따라서 위 독립 비교는 로컬 PROJ 출력만으로 대체하지 않았고, QGIS가 준비된 지원
환경에서 수행해야 한다.

## ARES Commander 2027 실제 파일 확인

자동 사전검증 및 GDAL 재읽기는 통과했다. Windows ARES 실제 앱에서는 기존
HATCH 채움 도면이 건물 표시 시 무한 로딩으로 멈췄다. SOLID 삼각형으로 바꾼
새 출력과 `*ACTIVE` 초기 뷰포트는 아직 Windows ARES에서 재확인해야 한다.
검증 호스트는 macOS 27.2이며 ARES 앱을 찾지 못했다. Graebert가 게시한 ARES
Commander 2027 SP1 macOS 지원 목록은 14, 15, 26이므로 현재 호스트에서 성공을
가정하지 않는다. 공식 시스템 요구사항과 배포 OS는
[ARES 검증 절차](ares-commander.md)를 참조한다.

지원되는 Windows/macOS/Linux ARES Commander 2027에서 다음 두 파일을 각각 연다.

- `testdata/ares/sample-utf8.dxf` — SHA-256 `8b0bc5d41d8b343bedcecb60324630939954ad5a0ea83d6c4c6ad3a57a5f6d3f`
- `testdata/ares/sample-cp949.dxf` — SHA-256 `fdadb149f2e46445e4564e27d7e07e79fb3ffaf7a3b93d6fe23745df23c08942`

각 파일에서 복구/오류 대화상자, `sample_labeled` 레이어의 geometry 위치,
`한글 도로`·`한글 건물` 표시, 도로 라벨 30° 회전, 높이 2.5, `Korean` 문자
스타일을 확인한다. ARES에서 저장한 복사본을 다시 열어 문자와 entity가 보존되는지도
확인한다. 기록할 항목: ARES 버전/build, OS 버전, profile, 위 SHA-256, 각 확인
결과, 오류 내용 및 캡처 경로.

샘플 재생성: `./scripts/generate-ares-samples.sh`; 재생성 후 SHA-256이 달라지면
이 문서와 `docs/verification/ares-commander.md`의 digest도 갱신한다.

## Windows 데스크톱 실제 상호작용

macOS native executable을 실제로 시작해 보니 빈 label slice가 Go JSON에서
`null`이 되어 QML의 `labels.length`를 실패시키는 초기화 오류가 드러났다.
nil slice를 `[]`로 직렬화하고 QML payload도 배열 타입으로 정규화하도록 수정했다.
회귀 검사와 QML 테스트 12개가 통과했고, 재빌드한 native 앱 시작 로그에는 해당
JavaScript 오류가 재현되지 않았다. 다만 이 smoke test에서는 지도 조작이나 실제
레이어 로드는 수행하지 않았고, Qt가 설치되지 않은 `Monospace` font fallback 경고는
남아 있다.

Windows에 Go 1.27.x, Qt 6, GDAL/PROJ/GEOS 및 필요한 빌드 도구를 준비한 뒤
`powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1 desktop-native`로
빌드한다. 이어서
[`mvp-checklist.md`의 Windows 데스크톱 확인 항목](mvp-checklist.md#windows-데스크톱-수동-확인)
1–13을 실제 파일로 수행한다. 특히 다중 레이어 추가/저장, 정점 편집 후 재열기,
대용량 읽기 전용, Lua 규칙, 작업공간 원본 재연결 및 언어 전환을 확인한다.
기록: Windows 버전, Go/Qt/native 라이브러리 버전, 명령, 결과, 로그/캡처 경로.

## 깨끗한 대상 OS에서 배포 확인

지금의 `scripts/build.*` 산출물은 installer/app bundle이 아니다. 사용자가
배포 대상을 정한 뒤 깨끗한 Windows, macOS, Linux 환경에서 해당 OS의 패키지를
설치해 다음을 확인해야 한다.

- 시작 시 Qt platform/QML plugin, GDAL driver/plugin 또는 PROJ/GDAL data 경로 오류가 없는가.
- SHP·GeoPackage 읽기/쓰기, EPSG 변환 및 요구되는 grid가 재현되는가.
- `otool -L`(macOS), `ldd`(Linux), `dumpbin /dependents`(Windows)의 외부 의존성이
  모두 패키지되거나 설치 전제조건으로 명시되어 있는가.
- 앱 제거/업데이트와 사용자 data 보존이 예상대로 동작하는가.

## 온라인 Go 취약점 DB 확인

로컬 `go mod verify`는 2026-10-01 재확인에서 통과했다. 로컬 module cache의
`golang.org/x/vuln@v1.8.0` 소스로 임시 scanner를 빌드했다. portable, `-tags native`,
`-tags qt,native` 소스 분석 모두 취약점 DB를 가져오는 단계에서 DNS 차단으로 중단되어
취약점 결과를 내지 못했다. qt,native 시도는 Go package/build configuration을 읽은 뒤 DB
요청에 도달했으나, 이것 역시 취약점 없음의 증거는 아니다. 공식 Go 패키지 문서는 2026-09-08
게시된 [v1.8.0](https://pkg.go.dev/golang.org/x/vuln?tab=versions)을 현재 최신으로 표시하고 있어
workflow의 고정 버전은 최신과 일치한다.
저장소 CI의 `vulnerability-scan` job은 네트워크 가능한 Ubuntu runner에서 portable 및
native GIS build configuration을 검사하도록 구성되어 있으나, 현재 확인 가능한 것은
workflow 구성뿐이며 실제 CI 실행 결과는 아직 확인되지 않았다. 첫 CI 결과를 확인해 이
문서에 기록한다. 로컬에서도 DB 접속이 가능한 환경이라면 저장소 루트에서 다음처럼 검사한다.

```sh
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 -tags native ./...
```

Go module 취약점 DB 검사는 GDAL/PROJ/GEOS/Qt의 네이티브 라이브러리 CVE와
라이선스/드라이버 구성을 대신하지 않는다. native 의존성의 버전·라이선스도
실제 배포 대상별로 별도 조사한다.

## 배포/보안 감사 결과 요약

- 기본 `build/gis-cli`는 macOS ARM64에서 2.5 MiB이고 OS system library 외 GIS
  shared library가 없지만, 공간·포맷 CLI 명령은 native build가 필요하다.
- `build/gis-cli-native`(5.5 MiB)는 GDAL/PROJ/GEOS dylib에, 데스크톱
  `build/gogis-desktop-native`(54 MiB)는 추가로 Qt framework에 동적 링크된다.
  `Main.qml`은 embed되지만 Qt runtime/plugin과 GDAL/PROJ data는 포함되지 않는다.
  단일 파일로 전체 기능을 배포한다는 검증은 없다. 상세 근거는 [`build.md`](../build.md#단일-실행-파일-배포-점검)에 있다.
- Lua `Run`/`RunFile` 소스는 1 MiB로 제한한다. 실행 중 heap 사용은 제한되지
  않으며, `gogis.export_dxf`는 현재 프로세스 권한으로 임의의 경로에 파일을
  생성/덮어쓸 수 있는 명시적 capability다. 신뢰되지 않은 스크립트를 실행하려면
  별도 process sandbox, OS memory/CPU limit, export root allowlist가 필요하다.
- Renderer의 일반 WKB fallback parser는 좌표·ring·child count가 입력 길이에
  실제로 들어맞는지 할당 전에 검사하고 geometry collection 중첩을 64단계로 제한한다.
  악성 count 및 과도한 중첩 회귀 테스트와 10초 fuzz 실행(약 183만 입력)이 통과했다.
  Ubuntu CI는 같은 parser에 5초 bounded fuzz smoke test를 수행한다. 이는 전체 앱의
  RSS 상한이나 GDAL의 WKB 직렬화 전 native 할당 제한을 보장하지 않는다.
- GDAL Go 바인딩의 `NewGeometryFromWKB`는 빈 slice에서 첫 원소를 참조해 panic할 수 있고,
  길이를 C `int`로 직접 변환했다. 이제 빈 입력과 C API 길이 초과를 호출 전에 거부한다.
  `WKBWithMaxSize`의 C 버퍼 해제도 지연 정리로 바꿔, GDAL context 오류 반환 시에도 버퍼가
  남지 않게 했다. nil/빈 입력 회귀 테스트와 native GDAL race 테스트가 통과했다.
- Hit-test uniform-grid의 보조 cell/span membership을 최대 1,048,576개로 제한했다.
  초과 시 부분 인덱스 배열과 map을 버리고 이미 보유한 geometry에 대한 선형 검색으로
  전환하므로 인덱스 빌드가 auxiliary memory를 계속 늘리지는 않는다. 초과 fallback의
  정합성 회귀 및 전체 Go/native/Qt 검증은 통과했다. fallback은 click 응답시간이 커질 수
  있고, window hit geometry 자체나 GEOS triangulation 출력의 byte 상한은 아니다.
- Qt에 전달하는 전체 지도 라벨 JSON은 marshal 전에 escape expansion 상한을 계산해
  20,000 label/8 MiB로 제한한다. 초과 시 위험한 대형 payload 대신 label 표시를 생략하고
  상태 문구를 낸다. HTML-escaped worst-case 문자열, 최대 numeric field 길이, 일반 UTF-8과
  label count 상한 회귀 테스트가 통과했다.
- Read-only polygon WKB의 좌표 수는 allocation-free parser로 세어, display CRS 변환 및
  label anchor GEOS 호출보다 먼저 window당 250,000 vertex cap을 적용한다. Fill triangulation 직전의 기존 point-array
  guard도 유지한다. 2D, ISO Z, nested collection, malformed byte count 테스트와 실제 세종시
  연속지적도 window가 통과했다. 이는 GEOS 자체 메모리 byte quota가 아니다.
- GDAL attribute session/reader는 feature의 문자열·바이너리·list 값을 Go slice/string으로
  복사하기 전에 전체 decoded-value budget 8 MiB를 검사한다. GeoPackage의 8 MiB 초과 문자열
  회귀 입력은 `OpenWindowWithLimits`에서 feature materialization 단계에 거부됐고, 세종 SHP
  실제 window와 100만 피처 viewport stress도 다시 통과했다. GDAL이 이미 native 쪽에 읽은
  값의 메모리까지 제어하지는 않는다.
- 사용하던 GopherLua `LState.SetMx`는 VM별 quota가 아니다. 로컬 v1.1.2 소스상
  프로세스 전체 `runtime.MemStats.Alloc`을 모니터링하고 임계값에서 `os.Exit(3)`을
  호출한다. 호출 인자는 MB 단위이며 기존 `8<<20` 사용은 실질적인 8 MiB 제한도
  만들지 않았다. GoGIS 호출을 제거했다.
- `scripts/build.sh clean`/`build.ps1 clean`은 build 디렉터리를 지우므로 symlink,
  junction 및 비정상 경로에 대한 방어를 추가했다. Bash의 symlink 거부 동작은
  격리된 임시 저장소에서 시험했다. PowerShell 실제 실행은 현재 Mac에서 확인하지 못했다.

프로젝트의 MIT `LICENSE`만으로 Qt 및 GDAL native distribution 의무가 모두
충족된다고 단정하지 않는다. Qt 모듈은 LGPL/GPL/commercial 선택에 따라 의무가
달라질 수 있고, GDAL binary의 optional driver dependency도 각각 별도 조건이
있을 수 있으므로 실제 포함 파일에 대한 notice/SBOM 및 배포 전 라이선스 검토가
남아 있다 ([Qt licensing](https://doc.qt.io/qt-6/licensing.html),
[Qt LGPL obligations](https://www.qt.io/development/open-source-lgpl-obligations),
[GDAL license](https://gdal.org/en/stable/license.html)).

## 2026-10-02 대용량/C 경계 후속 검증

추가 C ABI 감사에서 Go slice 길이를 `C.int`로 바꾸는 컬러 테이블, overview,
속성 list/binary, VSI read, GCP, raster band 경로를 찾았다. 모두 C `int` 범위를
넘는 길이를 변환하기 전에 거부하도록 했고, helper의 경계 회귀 테스트를 추가했다.
Raster I/O band count도 stride 및 native buffer 계산 전 확인한다. `third_party/godal`
의 연관 테스트는 `-race`로 통과했고, Go GIS/native 전체 테스트, 전체 race 테스트,
`go vet`, `go mod verify`, `cgocheck2` 대상 패키지 및 `git diff --check`도 통과했다.

Qt/native opt-in stress test를 `CGO_CXXFLAGS=-std=c++17`로 실행했다. 1,000,001개
feature에서 128회 viewport 이동 후 보유량 제한이 유지됐고 1,952 feature hit 및
1,952 label hit, 프로세스 peak RSS 189 MiB를 기록했다. 이는 해당 synthetic
GeoJSON/현재 macOS 실행의 측정치이며 대한민국 전역 데이터, GUI 상호작용, 임의의
단일 feature geometry 또는 전체 시스템 OOM 상한을 보장하지 않는다. 기본 Qt
test invocation은 C++17 compiler flag 없이 Qt 헤더 오류로 실패했으며 위 환경변수로
다시 실행했을 때 통과했다.

공식 [GO-2026-5970 기록](https://pkg.go.dev/vuln/GO-2026-5970)은 수정 버전을
`golang.org/x/text v0.39.0`으로 표시하고, 저장소는 v0.42.0을 사용한다. 따라서 이
특정 취약점의 알려진 vulnerable range 밖임을 확인했지만, `govulncheck` 데이터베이스
접속은 DNS 제한으로 실패해 전체 온라인 취약점 재검색은 여전히 미검증이다.

추가 module graph 감사에서 standalone `third_party/godal`은 별도 `go.mod`를 통해
`x/text v0.22.0`을 선택했고, `go mod why golang.org/x/text/unicode/norm`에서
`godal/cogify → cloud storage → x/net/idna → norm` 활성 import 경로가 확인됐다.
이를 놓치지 않도록 nested graph도 `x/text v0.42.0`, `x/sync v0.23.0`으로 올렸고,
이 버전들의 요구사항에 맞춰 nested module의 `go` directive를 1.26.0으로 맞췄다.
Standalone godal C-boundary/VSI `-race` 테스트와 nested `go mod verify`가 통과했다.

같은 날 bounded fuzz smoke test도 재실행했다: WKB render parser 약 896천,
GeoJSON geometry bounds 약 1.79백만, GeoJSON property decoder 약 1.53백만,
GeoJSON sequence scanner 약 6.8천 입력에서 panic/crash 없이 끝났다. 각 실행은
10초 안팎의 단일 macOS ARM64 fuzz run이며 입력 공간 전체 검증이나 RSS quota를
의미하지 않는다.

추가 재검증에서 `GOCACHE=/private/tmp/gogis-go-build-cache ./scripts/verify.sh`가
일반/native 테스트 및 race, vet, native 빌드, Qt/native 테스트와 QML 테스트
(12/12)를 모두 통과했다. 뒤이은 sandbox 내 `govulncheck -tags native ./...` 재실행은
`vuln.go.dev/index/modules.json.gz` DNS 조회 차단으로 결과를 반환하지 못했다. 이는 앞서
승인된 네트워크 실행에서 확보한 루트/nested 결과를 무효화하지 않으며, 현재 checkout의
수정된 CI workflow가 실제 실행된 결과는 아직 없다.

대용량 merge 경로도 추가 점검했다. 기존에는 결과 feature slice와 duplicate-ID map을
예약한 뒤 CRS/schema mismatch를 검사했다. 이제 메타데이터를 먼저 검증하며, 250,000개
feature를 가진 schema 불일치 입력은 오류로 끝나면서 추가 할당 4 MiB 미만이다. 또한
모든 ID를 먼저 검증하므로 마지막 ID가 중복인 경우 geometry/property 깊은 복사를
시작하지 않는다. 10,000개 feature × 1 KiB WKB 입력의 late-duplicate 회귀 테스트도
할당 4 MiB 미만을 확인했다. 두 오류 경로 모두 race 테스트를 통과했다. 현재 유효
10K owned-merge benchmark는 3회 측정에서 188.8 µs/op, 352,944 B/op였고, 표본이
적어 성능 추세값으로 해석하지 않는다. 변경 후 전체 `scripts/verify.sh`도 다시 통과했다.

PostGIS `ReadLayer`도 대용량 입력 경계를 갖지 않고 전체 `ORDER BY id` 결과를
materialize하던 것을 확인했다. 현재 query/reader는 100,000 feature와 128 MiB 추정
payload에서 중단하고, geometry는 50,000 point/2 MiB stored size/8 MiB WKT, JSONB는
8 MiB와 65,536 node/128 depth로 제한한다. 초과 시 조용히 잘라내지 않고 오류를
반환한다. 관련 parser/query 단위 테스트와 전체 verify script는 통과했지만, 실제
PostGIS 서버 통합 테스트는 DB 연결이 없어 확인하지 않았다. 이 경로의 한도는
materialized API 보호용이며, 백만 행 PostGIS 테이블 viewport 지원을 의미하지 않는다.
동일한 adapter audit에서 PostgreSQL identifier의 빈 component/63-byte 초과와
negative BIGINT ID의 uint64 wrap도 막았으며, 각각 경계 단위 테스트와 전체 verify를 통과했다.
PostGIS JSON budget parser는 10초 fuzz run에서 약 316천 입력을 panic/crash 없이 처리했다.

## 2026-10-02 최종 재검증 후속

현재 checkout에서 `GOCACHE=/tmp/gogis-go-cache ./scripts/verify.sh`를 다시 실행해
포터블/native test 및 race, vet, native build, nested GODAL test/race, Qt/native desktop,
QML 12/12를 모두 통과했다. 추가 opt-in viewport stress는 각각 1,000,000 및
1,000,001 synthetic Point feature, 128회 이동, 1,952 total hit/label, peak RSS 189 MiB로
통과했다. 앞선 기록의 sandbox DNS 차단과 달리, 승인된 네트워크 실행에서 현재 root
포터블 및 native 태그 `govulncheck`는 모두 `No vulnerabilities found`를 반환했다.
standalone `third_party/godal` 스캔은 reachable 0건이며, 사용 코드에서 호출되지 않는
module-only advisory 1건을 보고했다.

대용량 프로젝트 교체 검토에서는 `replaceWithLoadedMode`가 새 runtime의 render source와
viewport-backed read-only map/window state를 이전하지 않는 문제를 발견했다. 이에 새 상태를
함께 넘기고, 일반 runtime으로 교체할 때 이전 window hits/IDs/names/labels 및 base snapshot을
놓도록 수정했다. 두 전환 방향을 확인하는 회귀 테스트와 Qt/native desktop race test가
통과했다. 이 수정은 자동화된 runtime state 검증이며, 실제 GUI/GPU 동작을 대신하지 않는다.
현재 Downloads의 세종시 연속지적도(208,015 polygons)와 지적도근점(11,971 points)을
사용한 실데이터 통합도 다시 통과했다. 각 layer에서 128회 이동을 수행했고, 대형
연속지적도는 bucket 4에서 8,962 hit/735,647 vertices, 도근점은 bucket 0에서 1 hit였으며
프로세스 peak RSS는 171 MiB였다. 저배율에서 20,000-feature window cap으로 반환된
오류들은 의도된 보호 동작으로 테스트가 확인했다.
추가로 1,000,000-feature/128-move synthetic stress도 `-race`로 통과했으며, race
instrumentation 포함 peak RSS는 545 MiB였다(일반 실행의 189 MiB와 직접 비교할 수 없음).
WKB core/render 및 PostGIS JSON budget fuzz smoke는 각각 약 606K/271K/135K 실행,
GeoJSON geometry-bounds/property fuzz는 각각 약 517K/452K 실행에서 panic/crash 없이
종료했다. 각 fuzz run은 단일 macOS ARM64, 8초 제한의 smoke test이며 완전한 입력 공간
검증은 아니다.
이 1M synthetic viewport stress를 `scripts/verify.sh` 및 Windows의 `verify.ps1 -Native -Qt`
기본 게이트에 연결했다. 현재 macOS에서 전체 `scripts/verify.sh`를 다시 실행해 해당
스트레스 단계와 Qt/QML 12/12를 포함한 모든 게이트가 통과했다. Windows PowerShell 및
Windows GIS/Qt 런타임은 이 호스트에서 실행하지 못했다.
후속 `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...` cross-build는 exit code 0으로
통과했다. 이는 portable Go 패키지의 Windows/amd64 컴파일만 확인하며, Windows CGO,
GDAL/PROJ/GEOS, Qt 또는 GUI 실행을 검증하지 않는다.
추가 동시성 감사에서 `refresh`가 `viewportReadOnly`를 runtime mutex 해제 후 읽는 race를
찾았다. refresh 시작에서 렌더 상태 스냅샷을 mutex 안에서 취하도록 수정하고 concurrent
mode-toggle 회귀 테스트를 추가했다. 테스트 단독 및 Qt/native 전체 desktop `-race`가
통과했으며, 수정 후 전체 `scripts/verify.sh`도 다시 통과했다.
이어 기존 workspace base layer를 보존하면서 큰 source를 추가하는 windowed read-only 경로가
materialized fallback의 1M feature/256 MiB 합산 예산을 우회해 렌더 소스 구축 전에 추가 복사
및 변환을 시작하는 것을 확인했다. base layer usage preflight를 source open/CRS 정렬보다 먼저
적용하고, 과도한 중첩 속성의 base layer는 존재하지 않는 추가 source를 열기 전에 거부되는
회귀 테스트를 넣었다. 수정 후 `scripts/verify.sh`를 다시 실행해 포터블/native test 및
race, vet, native build, nested GODAL test/race, Qt/native desktop, 1M viewport stress,
QML 12/12를 모두 통과했다.
추가 메모리 점검에서 append 로더의 `Project()` 깊은 복제가 기존 project 전체 geometry와
property payload를 다시 복사하는 것을 찾아, COW 편집 계약을 이용한 feature-header render
snapshot으로 바꿨다. 10K synthetic WKT feature benchmark 3회 실행에서 snapshot은 1.369 ms,
3.92 MB/30,003 allocs였고 render snapshot은 30.8 µs, 402 KB/3 allocs였다. 이는 약 13× 적은
할당 바이트를 보인 단일 M3 측정이며 WKB/실데이터 전체 성능을 보장하지 않는다. 헤더/label은
독립 복사하고 geometry/property는 불변 공유하며, 편집 COW 격리 테스트를 추가했다. 이 새
변경 후 `scripts/verify.sh`를 다시 실행해 포터블/native test 및 race, vet, native build,
nested GODAL 검사, Qt/native 데스크톱, 1M viewport stress, QML 12/12가 모두 통과했다.

백만 feature metadata benchmark를 각기 새 프로세스에서 한 번씩 실행해 측정 경로를
분리했다. raw `godal.Open` GeoJSON FeatureCollection benchmark는 open 이후 process
peak RSS 2,637 MiB, GeoJSONSeq benchmark는 `FeatureCount` 후 2,408 MiB 및 `Bounds`
후 4,750 MiB를 보였다. 이 두 benchmark는 GDAL driver API를 직접 호출하며 desktop
loader 경로를 측정하지 않는다. 실제 `BenchmarkDesktopReadOnlyLoadGeoJSON1M`는 custom
bounded stream reader로 1,000,000 features를 확인했고, 첫 viewport 27.95 ms,
process peak RSS 174 MiB, retained Go heap 46.92 MiB를 기록했다. 이전 repeated-viewport
stress run은 1,000,001 feature에서 189 MiB였다. 이 측정들은 별도 1회 실행이고
synthetic point GeoJSON이므로, 실데이터/다각형 복잡도/OS 자원 차이를 대체하지 않는다.
대형 GeoJSON vector 입력은 raw `godal.Open`으로 우회하지 말고 desktop의 bounded
stream reader를 사용해야 한다. 공개 `GeometrySession` API가 같은 파일을 raw GDAL으로
열던 우회도 발견해 `AttributeSession` 기반 bounded path로 바꾸고, `.geojson` 및
`.geojsonl` 라우팅 회귀 테스트를 추가했다.

### 데스크톱 메모리 표시와 상태 UX

Qt 데스크톱 하단 상태바에 프로세스 메모리와 Go heap을 별도 sampler goroutine에서
1초 간격으로 표시한다. 동기 저장 같은 UI 요청 폴링을 막는 작업 중에도 표본 갱신은
계속된다.
Linux는 `/proc/self/statm`의 현재 resident set, Windows는 process working set,
macOS CGO 빌드는 Mach `task_info`의 현재 resident set을 사용한다. CGO가 비활성화된
macOS fallback만 `getrusage` peak RSS를 사용하며 UI에서 측정 종류를 구분한다.
샘플링할 수 없는 플랫폼에서는 Go heap만 표시하고 측정 불가를 명시한다. 프로세스 값은
Go/GDAL/Qt의 resident footprint를 포함하지만 GPU 메모리를 포함하지 않으며, Windows
working set은 RSS와 정의가 달라 다른 플랫폼 수치와 직접 비교할 수 없다.
표시값은 진단 정보이지 동적 메모리 제한이나 OOM 방지 quota가 아니다.

동적 렌더/로드 상태는 영어 원문을 오류 상세로 보존하면서 영어·한국어·일본어 안내로
표시하고, 오류(빨강)·취소/경고(황색)·진행 및 읽기 전용(파랑)·성공(초록)·중립(회색)을
구분한다. 확대 후 재시도 안내를 오류 상태로 표시한다. 로컬 Qt offscreen QML 테스트는
언어별 상태, 취소/오류/읽기 전용 색상, 메모리 표현을 검증한다. OS 별 프로세스 메모리
수집은 Linux/Windows 실행 환경에서 별도 확인해야 한다.
2026-10-02에는 status bar의 마지막 상태만으로는 이전 오류와 native stdout/stderr를 놓칠 수
있어, footer의 Logs 창과 bounded session collector를 추가했다. Go 애플리케이션의 오류 status와
프로세스 stdout/stderr를 최근 500건/512 KiB 범위로 보관하고, 한 줄은 16 KiB에서 잘라낸다.
macOS/Linux는 fd 1/2를 pipe로 tee해 실행 터미널 출력을 유지하고, Windows는 Go 표준 stream과
Win32 standard handle을 로그 pipe로 바꾼다. 파일에는 자동 저장하지 않는다. pipe 단위 capture/
mirror, ring cap/UTF-8 truncation, QML 표시 테스트가 통과했다. 실행 파일 초기화 시 stdout/stderr
pipe 설정 실패는 원래 stderr에 진단하고 앱 시작은 계속한다. OS별 native library 출력이 모든
플랫폼에서 잡히는지는 실제 Windows 앱에서 별도 확인해야 한다.
Qt C++ staging vertex vector는 빈 뷰포트에서 capacity를 해제하고 새 payload가 기존
capacity의 1/4 이하가 되면 축소한다. native bridge regression은 데이터 복사 소유권,
빈 payload 해제, 큰-후-작은 viewport 버퍼 감소를 직접 검사한다. 이는 Qt GPU/scene graph
allocator가 OS에 메모리를 반환하는 시점까지 보증하지 않는다. `updatePaintNode`의 최초
`QSGGeometryNode` 할당도 예외를 잡아 렌더 오류 상태를 내고 null node를 반환하도록 방어했다.
실제 allocator 실패 주입은 하지 못했으며, `scripts/verify.sh`의 Qt native 빌드/테스트는 통과했다.

저배율에서 세종 연속지적도 query가 20,000-feature window cap을 넘어 첫 타일이 표시되지
않는 문제를 줄이기 위해 read-only query cell을 최소 1/64 extent로 세분화했다. 현재 Downloads
실자료 integration은 초기 zoom bucket 0에서 첫 성공 셀(8,962 feature, 735,647 vertex)을
확인했고, 도근점 셀도 성공했다. 두 소스의 128회 viewport 이동 테스트는 process peak RSS
181 MiB에서 통과했다. 이것은 Go/GDAL builder 경로의 실자료 검증이며 visible Qt/GPU 앱 동작,
전국 데이터의 첫 프레임 시간, 100,000-feature/128 MiB viewport-wide cap 해소를 보장하지
않는다. 초과 셀은 계속 오류로 보고될 수 있다.

실제 `Main.qml`의 MapCanvas에 `diagnosticLogPayload` QML property 선언이 누락돼 native
bridge에서 갱신한 값이 Logs dialog binding에 전달되지 않던 것을 추가했다. QML regression은
dialog를 연 뒤 payload가 바뀌어도 stdout/stderr/application 오류가 나타나는지 확인한다.
Go 상태 오류와 native renderer 오류를 stdout에도 출력하고 native 오류의 stderr 출력은
유지한다. stdout/stderr 캡처와 offscreen QML 테스트는 통과했으나, 사용자의 실제 창에서
로그 확인 및 렌더/숨김 전환은 재검증이 필요하다. Logs 버튼은 좁은 창에서도 쉽게 찾도록
footer 도구줄 맨 왼쪽에 배치했고, QML test는 버튼 signal로 dialog를 열고 로그 payload를
표시하는 것과 레이어 체크박스 hide/show가 visibility generation/payload를 갱신하는 것을 확인한다.

2026-10-02 현재 변경을 포함한 코드에서 Downloads의 세종 연속지적도 SHP 208,015개와
지적도근점 SHP 11,971개를 함께 여는 `TestWindowedReadOnlyLargeSourceIntegration`을
재실행했다. 각 레이어 128회 viewport 이동을 통과했고 test process peak RSS는 166 MiB였다.
이는 실제 원본을 쓰는 Go/GDAL read-only window/runtime 통합 테스트이지, visible Qt 창,
GPU 메모리 또는 현재 RSS sampler의 화면 표시 검증은 아니다.

실제 초기 화면과 같은 `zoom=1`, `center=(0.5,0.5)`의 combined-source `refresh` 경로도
추가 검증했다. 최초에는 연속지적도 타일 하나가 GDAL window의 20,000-feature 제한을 넘어
나머지 geometry만 publish했다. 이 경우를 위해 과밀 타일만 bounded subdivision으로 재조회하고,
subcell 경계 중복은 WKB+속성 fingerprint로 제거한다. 깊이 8, 최대 256회 query, aggregate
32 MiB/window, 100,000 features/viewport 상한을 유지한다. polygon 입력이 triangulation의
250,000 vertex cap을 넘으면 최대 1,000,000 vertex까지 outline-only로 그리며 GEOS fill을
건너뛴다. Downloads 실자료 최신 실행은 8,978 chunks를 30.6초에 처리해 3,372,623 vertices를
publish했지만, 후속 타일의 32 MiB/window 및 4,194,304-vertex viewport batch 상한으로 여전히
`incomplete`였다. 상한을 제거하지 않고 과밀 타일의 화면상 외곽선을 더 많이 보이게 한 결과다.
따라서 “No visible layers”가 발생하지 않는 것과 전체 피처가 성공적으로 그려지는 것은 별개이며,
전역 대용량 데이터의 완전 렌더 보장이 아니다. 동일 뷰에서 도근점을 숨겼다 다시 켠 후에는
8,969 cache hits, 0 chunks built, 11ms에 Go 렌더 요청이 끝났다. 이는 Qt scene graph가 실제
프레임을 표시한 시간은 아니므로 화면에서의 최초 표시/레이어 토글 체감 및 실제 GUI는 별도다.
실패 로그는 과밀 source chunk key도 포함한다. 40,000 feature 한도를 실험했을 때도 같은
원래 타일은 초과했으므로 단순 상향 대신 bounded subdivision을 적용했다.

2026-10-02 실제 입력 재점검에서 도근점 SHP의 두 좌표 이상치가 전체 결합 extent를 크게
늘리는 것을 확인했다. 11,971개 중 2개가 나머지 점의 좌표군과 현저히 다른 위치에 있어도
원본 피처는 보존한다. GDAL overview에서 레이어별 CRS 변환 bounds와 geometry family를
전달하고, 첫 실행은 bounds가 있는 폴리곤 레이어를 우선 화면에 맞춘다. 레이어 우클릭의
“Zoom to layer”도 같은 bounds를 사용하며 프로젝트/원본 extent 자체는 바꾸지 않는다.
read-only 렌더 grid를 1/128 정규화 셀로 세분하고 viewport feature cap은 50,000, payload cap은
32 MiB로 유지했다. 실제 세종 자료의 폴리곤 맞춤 첫 화면은 5.2초에 1,934,704 vertices를
publish했고, 34,645 hit features / 18 MiB retained payload에서 viewport feature 상한에 걸려
`incomplete`를 기록했다. 최신 재실행은 숨김 후 재표시에서 258/258 cache hit, 0 chunk 재빌드,
약 3ms였다. 테스트 프로세스 peak RSS는 502 MiB(Go heap 약 159 MiB)였다. 따라서 최초 화면에 유효 geometry가 나타나고 앱의
안전 상한이 작동하는 것은 실자료에서 확인했지만, 해당 지방 전체 피처 완전 렌더는 아직
보장되지 않으며 실제 사용자 GUI/Qt GPU 프레임 확인도 남아 있다. 이 로그는 벡터 원본을
수정하거나 이상치를 제거하지 않는다.

같은 진단 바이너리의 visible macOS 창 실행은 pasteboard/Launch Services 연결 오류와 종료
코드 2로 실패해 실제 사용자 세션 화면 검증으로 사용할 수 없었다. `QT_QPA_PLATFORM=offscreen`
및 software scene graph에서는 Qt software backend/font 초기화까지만 로그로 확인했고, 25초
동안 `updatePaintNode`/프레임 진단은 나오지 않아 중단했다. 따라서 위의 실자료 geometry 및
hide/show 결과는 Go/GDAL/runtime 통합 확인이며 Qt/GPU 실제 화면표시나 체감 프레임 시간의
증거로 간주하지 않는다.

2026-10-02 사용자가 제공한 `chunks=7/1225`, `missing_results=1218` 로그를 조사해
`Scheduler.splitCachedRequest`의 두 cache/miss 분할 오류를 수정했다. 첫 cache hit 전에 여러
miss가 있으면 첫 miss만 보관하던 문제와, 요청 앞부분이 cache hit인 경우 뒤쪽 miss 목록을
pool buffer로 옮기며 버리던 문제가 함께 있었다. 두 회귀 테스트는 선행 miss, 선행 cache hit,
interleaved hit/miss 및 result/chunk key 짝을 검사한다. 사용자 세종 연속지적도 208,015 features를
전체범위 zoom=1로 여는 opt-in 테스트에서 QIX 캐시를 적용한 뒤 1,225/1,225 chunks,
308,213 render vertices가 2.153초에 `ready`로 완료됐다. 연속지적도+도근점 결합의 layer-fit
viewport는 270/270 chunks, 320,557 vertices가 2.888초에 완료됐고, hide/show 재표시는
270 cache hits / 0 rebuild로 끝났다. 이는 실자료를 쓰는 Go/GDAL/runtime와 스케줄러 완료 확인이며,
눈에 보이는 Qt 창이나 GPU scenegraph의 픽셀/프레임 확인은 아니다.

같은 날 million-point SHP 비교 benchmark를 다시 1회씩 실행했다. `.qix` 적용 fixture는
loader+첫 window 7.76 ms, 첫 window 1.75 ms였고, QIX 없는 fixture는 각각 105.4 ms,
99.24 ms였다. isolated loader child peak RSS는 두 경우 모두 76 MiB였다. 이 결과는 QIX가
있는 합성 uniform-point SHP의 window query 개선을 보여 주지만, QIX 준비/복사 시간은 fixture
생성 단계라 측정에서 제외되어 있으며 복잡한 polygon geometry 처리나 실제 Qt 프레임 속도와는
구분해야 한다.

2026-10-02 후속 visible Qt 앱 확인에서는 테스트용 `.app` bundle로 실제 사용자 세종시 SHP를
열었다. 로그에 QIX 사용, `request generation=0/1/2`, 이어서 `first-publish generation=2`
57 ms / 4,102 vertices, `ready generation=2 chunks=1225 vertices=302728 elapsed_ms=3094`
가 기록됐다. 즉 이전 `7/1225`, `missing_results=1218`은 재현되지 않았고 1,225개 요청 타일의
결과가 전부 도착했다. 창의 상태는 `Approximate overview ready; zoom in for exact geometry and
selection`이었고 전체 세종 영역이 지도 뷰포트에 보이는 것을 화면으로 확인했다. 관측된 RSS는
로드 중 약 523 MiB, 로그 창을 닫은 뒤 441 MiB였으며 Go heap은 약 20 MiB였다. 이는 이 한
실자료·한 기기에서의 정상 렌더 확인일 뿐 전국 규모의 메모리 상한 보증이나 GPU별 프레임
성능 검증은 아니다. 데이터의 세로형 extent 때문에 지도 양옆에 여백이 남는 것은 전체 영역을
맞춰 표시한 결과이며 누락 타일로 보이는 공백과 구별된다.

사용자가 제공한 첫 실행/전체 extent 스크린샷(2026-10-02)을 조사했다. 빈 화면의 가운데
`Add vector files` 버튼이 전체 map pan/click `MouseArea`보다 낮은 QML stacking order에 있어
마우스 입력을 가로챌 수 있음을 확인해 empty-state action을 위로 올렸다. 수정된 visible Qt
빌드에서 해당 버튼을 실제로 눌렀고 macOS OpenFiles 선택기가 열린 것을 확인했다. 전체 extent가
잘려 보이는 상태에서 복구할 수 있도록 `Zoom to full extent` 버튼도 추가했으며, 이 버튼은
현재 metadata dataBounds의 폭·높이를 viewport에 90%로 맞추고 pan을 초기화한다. 초기 layer fit이
개별 우선 polygon bounds와 전체 렌더 범위가 달라져 잘리지 않도록 초기 표시도 aggregate
metadata dataBounds 기준으로 맞추며, 레이아웃 크기 준비 전 실패하면 viewport 크기 변경 시
pending fit을 재시도한다.
2026-10-02 최신 native 빌드로 실제 세종시 SHP를 열었을 때 처음부터 세종시 전체 개요가
viewport 안에 표시됐고, `Approximate overview ready` 상태였다. 이어 좌표 이동 입력에
EPSG:5186 dataset extent 밖인 `(0, 0)`을 넣어 지도 형상이 사라지는 상태를 의도적으로 만든
뒤 `Zoom to full extent`를 눌렀다. overview가 다시 로딩되어 전체 영역이 화면에 표시되고
`Approximate overview ready`로 돌아오는 것을 visible Qt 화면에서 확인했다. 최초 로드 로그는
`ready generation=2 chunks=1225 vertices=302728 elapsed_ms=2412`였다. 따라서 최초 버튼 동작,
초기 fit, 범위 밖 이동 후 전체 범위 복원을 실제 SHP/Qt 화면에서 검증했다. 표시된 것은 넓은
축척의 근사 overview이며 정밀 경계는 확대 시 확인해야 한다.

같은 세종시 전체범위 개요가 사용자 화면에서 여전히 성기다는 후속 피드백을 반영했다. 이전
기능수 기반 샘플은 전체 208,015개에서 약 1/32(약 6,500개)만 남길 수 있었다. 전체범위의
overview budget을 50,000 중 80%(40,000)까지 사용하고, required stride를 다음 2의 거듭제곱으로
올림하지 않도록 변경했다. SHP feature ID modulo 대신 각 공간 창의 GDAL 결과 순서에서 일정
간격으로 골라 FID가 불연속인 데이터의 편향을 줄였다. 세종 데이터 계산은 stride 6 / 약 34,669
features다. 실데이터 전체 1,225 chunk 통합 테스트는 `ready`, 1,703,697 render vertices,
2.231초로 완료됐다. 이전 기준은 302,728 vertices / 약 2.4초였으며 새 결과도 2.5M-vertex
안전 한도 이하다. 사용자 눈으로 본 밀도 및 다른 지역에서의 경계 대표성은 다음 visible build
확인에서 최종 검증해야 한다.

2026-10-02 진단에서 C++ `GOGIS_PERF=1` 타이머 시작 함수가 workspace 경로에서 호출되지 않던
것을 발견했다. 이제 단일 파일, 파일 추가, workspace 로드 진입점 모두에서
`native.BeginLoadTrace()`를 호출하며, C++ 측에서 환경변수가 켜졌을 때만 trace를 출력한다. 진단용
workspace를 Qt offscreen/software + threaded render loop로 실행했을 때 첫 batch publication은
143.7 ms, 첫 scenegraph update는 178.3 ms / 14,148 expanded vertices로 기록됐고 60초간 이후
vertex/frame 로그 및 Go traceback은 없었다. 이 실행은 화면 캡처가 불가능했고 전체 load/ready
status도 관찰할 수 없어 완전 렌더나 문제 재현/비재현의 증거가 아니다. Visible 사용자 세션
재현 시 `GOGIS_PERF=1 GOTRACEBACK=all`로 터미널 출력을 함께 보존할 수 있다.

사용자가 제공한 2026-09-30 macOS 27.2 / Go 1.27.1 / Qt 6.11.2 crash report를 직접 확인했다.
보고서의 확정 정보는 `EXC_CRASH (SIGABRT)`, 종료 코드 6이며, faulting thread 이름은
`CVDisplayLink`이고 해당 stack에 `runtime.raise_trampoline`이 있다. 별도 QSGRenderThread는
Qt `QSGThreadedRenderLoop` 아래 `GoGISMapCanvas::updatePaintNode`의 vertex-conversion lambda에서
실행 중이었다. 보고서에는 `EXC_BAD_ACCESS`, `EXC_RESOURCE`, memory footprint 요약이나 OOM
원인이 없다. Go 공식 문서상 `GOTRACEBACK=crash`는 unrecovered panic/runtime condition 후
Unix에서 SIGABRT를 발생시킬 수도 있지만, 이 report의 `runtime.raise_trampoline`/`sigtrampgo`는
Go signal handler가 전달받은 미처리 신호를 OS 기본 동작으로 다시 보내는 경로다. 이것만으로
Go가 원래 abort를 발생시켰다고 할 수 없다. 당시 GOTRACEBACK 값 및 abort 직전 stderr/Go
traceback이 첨부되지 않아 Go runtime panic인지 C/C++ abort인지도 아직 확정하지 못했다. 다음 재현은 앱을 터미널에서 실행하고 두
파일을 추가해, SIGABRT 직전 terminal stderr와 앱 Logs 화면을 crash report와 함께 보존해야 한다.

2026-10-02 후속 재검증에서 위 실제 세종 SHP 통합 테스트를 `-race`로 재실행해 두 source의
128회 viewport 이동과 cache 기반 hide/show를 통과했다. race-instrumented test process peak RSS는
312 MiB였다. 별도 1,000,000-feature synthetic point viewport stress도 `-race`로 128회 이동을
통과했고 총 window hit/label은 각각 1,952개, peak RSS는 534 MiB였다. 두 RSS 값은 race
instrumentation을 포함한 테스트 프로세스의 peak 값이며, GUI/GPU 메모리 또는 전국 SHP의
완전 렌더 보장을 뜻하지 않는다. 실제 창에서의 최초 파일 추가, 화면 표시, layer toggle 체감과
SIGABRT 재현 여부는 계속 사용자 세션 검증이 필요하다.

2026-10-02 후속 race 검증에서 이전 숨김 캐시가 “모든 레이어를 숨길 때”만 보존되고 단일
레이어 숨김에서는 버려지는 누락을 발견했다. 이제 현재 뷰의 visible cache는 유지하고 hidden
layer cache는 최대 1,048,576 vertices까지 유지하며, 다시 표시된 layer는 같은 chunk key cache를
재사용한다. visible+hidden retention 한도, 전체 숨김 후 복원, 단일 layer 숨김/복원 회귀를
Qt/native/render `-race`에서 통과했다. 이는 scheduler cache 및 Go 상태 회귀 검증이며 실제
scene-graph의 프레임 표시 시각은 GUI에서 별도로 확인해야 한다.

같은 시점의 focused race 회귀 테스트 `TestRefreshVisibleLayerPublishesVerticesAndReusesCacheAfterToggle`와
`TestEmptyProjectRefreshDoesNotReportNoVisibleLayers`도 재실행해 통과했다. 첫 테스트 로그의 단일
`GoGIS: No visible layers`는 fixture의 유일한 레이어를 의도적으로 숨긴 순간 발생한 것이며,
초기 가시 레이어 렌더에서는 geometry publish와 재표시 cache hit를 확인했다. 그러므로 이 문구
하나만으로 SHP 로드 실패를 판정할 수는 없고, 사용자 재현 시 같은 시각의 render generation,
visible-layer/chunk 수와 로그를 함께 봐야 한다.

2026-10-02 native desktop 실측에서 이와 별도로 Qt scene-graph 전체 geometry batch rejection을
재현했다. 세종 workspace의 Go viewport batch가 4,109,955 source vertices까지 커진 뒤 Qt의
triangulated output vertex limit(8 Mi)을 넘었고, `updatePaintNode`가 geometry를 0개로 비우며
반복 `scene-graph vertex safety limit exceeded`와 `Render incomplete`를 남겼다. Qt는 한 source
vertex당 최대 3 output vertices를 만들 수 있는데, Go의 4 Mi batch limit은 이 확장 상한과
정렬되지 않았다. 이에 `render.MaxBatchVertices`를 2,500,000으로 낮춰 최악의 3배 확장도 Qt의
8 Mi 한도 아래(7.5 Mi)에 두고, 이를 확인하는 limit regression test를 추가했다. cmd/gis-desktop,
ui/qt/native, internal/render race tests, 실제 두 SHP의 128회 이동 통합 테스트 및 1M-feature
synthetic viewport race stress(128회 이동, 545 MiB peak RSS)는 수정 후 통과했다.

수정 후 native executable 빌드도 성공했다. 기본 macOS GUI 실행은 현재 Codex 실행 환경에서
pasteboard/Launch Services 연결 오류와 `Cannot create window: no screens available`로 시작하지
못했다. offscreen/software 실행도 초기 demo scenegraph 프레임 이후 workspace source 렌더 단계로
진행하지 않아, 수정 후 Qt scenegraph 상한 오류의 실제 소멸을 확인하지 못했다. 따라서 batch
상한의 Go-side safety 및 실데이터 runtime 동작은 검증됐지만, Qt 창에서 전체 batch publication과
실제 화면표시는 사용자 세션에서 재확인해야 한다.

후속 단일 SHP 직접 열기에서는 workspace loader와 달리 offscreen/software scene graph가 실제
연속지적도 source chunk를 처리했다. `QSG_RENDER_LOOP=basic`으로 약 115초 실행하는 동안
1,197,047 source vertices가 최대 2,118,627 scene-graph vertices로 변환되어 프레임 업데이트에
반영됐고, 이 관찰 구간에는 `No visible layers`, scene-graph safety 오류, panic이 없었다. 전체
viewport 로드가 끝나기 전 진단 목적으로 프로세스를 중단했으므로 완료 시간/최종 viewport
상태는 미검증이다. 로그는 `/private/tmp/gogis-cadastre-offscreen.log`에 보존했다. 이 결과는
Qt software renderer의 실제 geometry 변환 경로를 확인하지만, 화면이 없는 offscreen 검증이라
사용자 GUI/GPU 가시 표시를 대체하지 않는다.

같은 단일 SHP startup의 별도 반복에서는 첫 geometry publication 전 `GoGIS: No visible layers`가
한 번 출력됐지만, 227 ms부터 source vertices가 publish되고 Qt scene graph로 변환되기 시작했다.
로그는 9.55초 시점의 478,614 source / 844,962 expanded vertices에서 끝나며, 해당 파일에는
scene-graph safety error, `Render incomplete`, panic은 없었다. 이 실행은 새 2.5M source cap에
도달하지 못했고 종료 원인도 로그에 남지 않아 cap 경계 검증으로 보지 않는다. 초기의 단일
`No visible layers`가 왜 발생했는지는 아직 특정되지 않았으며, 실제 사용자 화면에서 반복되는
메시지와 동일 원인이라고 단정하지 않는다. 로그는 `/private/tmp/gogis-cadastre-cap-boundary.log`다.

후속 조사에서 대용량 SHP preview 경로의 visibility 불일치를 찾았다. GDAL geometry-prefix
reader는 `core.Layer.Visible`을 기본값 false로 반환하는데, `buildDataRuntime`의 render-side
`LayerVisibility`는 모든 layer를 무조건 visible로 만들고 layer-tree snapshot만 false로 만들었다.
QML이 preview layer tree를 동기화하면 preview의 유일한 layer가 숨겨져 `No visible layers`를
낼 수 있었다. Preview layer는 임시 첫 화면이므로 명시적으로 visible로 만들고, 일반 runtime
생성에서도 render visibility와 layer-tree visibility를 `Layer.Visible`에 맞춰 초기화하도록
수정했다. 회귀 테스트는 preview의 두 visibility 상태 일치와 일반 hidden-layer 상태를 각각
확인한다.

수정 후 race Go/Qt/native/render 테스트, QML 21/21 및 Qt native 빌드가 통과했다. 최신
실행파일로 세종 연속지적도 SHP를 offscreen/basic renderer에서 20초 실행해 226,689 expanded
vertices까지 publish/scenegraph 변환을 관찰했다. 이 구간에는 `No visible layers`, scene-graph
safety error 또는 panic이 없었고, 진단 구간 후 프로세스를 중단했다. 로그는
`/private/tmp/gogis-cadastre-visibility-fix.log`에 있다. 두 SHP workspace의 별도 offscreen 실행은
20초 동안 initial demo geometry 이후 데이터 publication이 없어 종료했으므로, workspace loader
전체 동작은 이 실행만으로 판정하지 않는다. 사용자 화면과 GPU 검증은 계속 필요하다.

2026-10-02 넓은 축척의 부분 표시를 위해 viewport chunk 요청을 중심 거리 순으로 우선하고,
줌 bucket에 따라 read-only cell을 1/32 → 1/64 → 1/128로 세분했다. zoom bucket 1 이하에서는
GDAL geometry-only window에서 결정적 feature 표본만 렌더하고 hit-test/속성/라벨 geometry를
보유하지 않으며, zoom bucket 2 이상에서 전체 피처와 선택을 복구한다. bucket -2 이하에서는
복잡한 선·폴리곤을 GEOS topology-preserving 방식으로 단순화하고 fill mesh도 단순화 geometry로
생성한다. 상태 표시줄은 근사 개요 모드와 확대 후 정밀 선택 가능성을 알린다.

세종 연속지적도 208,015피처와 도근점 77,352피처의 결합 초기 viewport 통합은 ready로 끝났다:
2,450 chunks, 2,084,067 source vertices, 64.4 s, 오류/안전 상한 초과 없음. 첫 non-empty
publication은 100 ms / 597 vertices였다. 개요 모드 retained hit features/payload는 0이며 peak
RSS는 542 MiB. 동일 뷰 hide/show는 2,450 cache hits / 0 chunk rebuild / 4 ms였다. 확대 정밀도
통합은 두 실 SHP에서 zoom bucket 2를 포함해 레이어당 128회 이동, peak RSS 80 MiB로 통과했다.
이는 Qt scenegraph/GPU 프레임 시간이나 사용자 창의 실제 픽셀 표시 검증이 아니며, 전체 완료
시간은 여전히 약 64초다. visible UI에서 표본 개요가 적절하게 보이는지, 실 GPU에서 첫 표시와
줌인 전환이 매끄러운지는 사용자 세션에서 확인해야 한다.

2026-10-02 전체범위의 지적 경계가 과도하게 빠지는 문제를 줄이기 위해 fitted extent 대비
Douglas-Peucker 허용치를 기존 약 1픽셀에서 약 0.45픽셀로 낮췄다. 단순화 허용치는 줌인할수록
절반씩 줄고, viewport 전체 피처 표본 예산은 유지한다. 세종시 연속지적도 단일 SHP 전체범위
실데이터 검증은 1,225/1,225 chunk 완료, 1,755,205 expanded vertices, 2.717초였으며
2,500,000 vertex safety limit 미만이었다. 이는 경계 밀도를 더 보존하면서 안전 한도에
844,795 vertices 여유를 남긴다. 후속 실제 Qt/macOS 화면 검증에서 전체범위는 `Approximate
overview ready` 상태로 보였고 화면에서 연속지적 경계의 촘촘한 분포를 확인했다. 전체범위
렌더는 첫 publication 58 ms / 24,228 vertices, 1,225/1,225 chunk 완료 2.622초 /
1,703,697 vertices였다. 한 단계 휠 확대(화면 표기 축척 약 1:22,326)는 361/361 chunk,
1,037,491 vertices, 1.092초에 완료되어 세부 경계가 화면을 채우는 것을 확인했다. 이후
전체범위 복귀도 1,225/1,225 chunk, 2.622초 내 완료됐다. 다만 UI 계측 RSS는 확대 직후
약 2.1 GiB, 전체범위 재표시 후 약 1.8 GiB까지 관찰됐다가 로그창을 닫은 뒤 781 MiB로
내려왔다. 화면 밀도 개선은 확인했으나 장시간 대용량 세션의 메모리 상한/회수는 별도 최적화
과제로 남긴다. 재현 로그는 해당 앱의 Logs 창에서 확인 가능하다.

2026-10-02 사용자가 전체범위 경계 대비를 더 강화해달라고 요청하여 기본 폴리곤 스타일을
짙은 녹색(`#356b53`), 0.65 mm 외곽선, 0.12 채움 불투명도로 조정했다. 실제 Qt/macOS 창에서
동일 세종 SHP 전체범위를 다시 열어 경계선 대비가 눈에 띄게 강해진 것을 확인했다. style 변경
후 정점 수는 동일하게 1,703,697이며, 첫 publication 50 ms, 1,225/1,225 chunk 완료
3.068초로 geometry workload는 증가하지 않았다. 테스트 중 UI RSS는 관측 시점에 1.3–1.9 GiB
범위였으므로, 가시성 개선과 별개로 실제 저메모리 환경/복수 레이어 메모리 검증은 추가로 필요하다.

2026-10-02 지적도근점과 연속지적도를 함께 여는 상황을 재현했다. 도근점은 11,971개이며
extent는 `212159.77, 43257.02 - 2287874.9, 459421.8`로, 연속지적도 extent
`211407.24, 423223.66 - 236805.50, 459484.82` 바깥에 크게 떨어진 점 2개가 있었다.
원본을 수정하거나 제거하지 않고 전체 데이터 bounds와 초기 fit bounds를 분리해, 초기 fit은
폴리곤 bounds를 우선하고 전체 bounds는 계속 보존한다.

결합 viewport의 폴리곤 외곽선이 2,500,000 vertex 안전 예산을 넘기는 것도 Go/native 경로에서
직접 확인했다: 135개 청크에서 내부 필지선까지 그리면 3,110,956 vertices였다. 넓은 축척의
폴리곤 개요는 제한된 GDAL spatial window의 모든 피처를 GEOS로 coverage dissolve한 뒤 외부
coverage boundary만 그리도록 변경했다. 첫 실파일 통합 테스트에서는 연속지적도를 먼저 열고
도근점을 추가한 뒤 기존 viewport의 지상 축척과 중심을 보존해 70개 청크를 10.4초에 만들었고
합계는 280,124 vertices였다. 따라서 2,500,000 vertex 예산을 지켰다.
인접한 두 사각형의 GEOS 회귀 테스트는 공유 내부 경계가 제거되고 외곽선 길이만 보존되는 것을
확인한다. 정확한 필지 경계와 선택은 확대 시 원본 geometry로 복귀한다.

이 통합 테스트는 실제 파일을 읽고 전체 viewport 청크의 Go/native render payload를 만들지만,
Qt scenegraph/GPU 창의 픽셀 출력까지 검증한 것은 아니다. 현재 Codex 실행은 `no screens
available`로 GUI를 열지 못하므로, 다음 확인은 새 `desktop-native` 빌드를 사용자 macOS 화면에서
두 SHP 동시 로드하여 전체 세종 외곽선, outlier로 인한 초기 축척 변화 여부, 로그의 `Render
incomplete` 부재를 확인하는 것이다.

2026-10-02 결합 화면의 `lod=6` 로그를 조사하면서 `replaceWithLoadedMode`의 공간상태 전달에서
`mapExtent`만 새 runtime으로 교체되고 `mapFitExtent`는 이전 값(초기 로드에서는 0 extent)에
남을 수 있음을 확인했다. 그 결과 point outlier 추가 뒤에도 LOD가 폴리곤 fit 축척을 반영하지
못하고, 강제 개요의 단순화 허용치도 유효하지 않을 수 있었다. `adoptLoadedSpatialStateLocked`가
전체/fit extent를 함께 전달하도록 수정하고 회귀 테스트를 추가했다. 실제 두 SHP 렌더 payload
테스트는 계속 통과했지만, 수정된 빌드의 로그에서 LOD가 올바르게 바뀌고 실제 화면에서 전체
외곽선이 정상적으로 보이는지는 아직 사용자 화면 확인이 필요하다.

후속 재현에서 기존 축척 보존 방식이 outlier로 바뀐 전체 bounds의 긴 축을 기준으로 하여,
세로가 긴 필지 fit bounds 전체를 viewport에 담지 못하는 경우를 확인했다. read-only 레이어 추가
시에는 전체 bounds를 유지하되 화면은 point 제외 fit bounds에 다시 맞추고, chunk 크기는 raw
zoom에 따른 안전한 세분도를 유지하도록 수정했다. 실자료 테스트에서 viewport 270개 청크가
21.2초에 완료됐고 75,146 vertices를 만들었다. 폴리곤 overview 정점 envelope가 실제 fit bounds
전체를 포함하는지 검사해 통과했다. 추가로 EPSG:5186 개요 dissolve에만 0.1 coordinate-unit
precision grid를 적용했다. 세종 SHP 전체 overview 정점은 280,124에서 37,792로 줄었고 fit
envelope 검증도 통과했다. 4cm 인접 경계 틈을 메우는 GEOS 테스트와 geographic CRS의 도 단위
grid 테스트를 추가했다. precision grid만 적용한 기존 축척 viewport에서는 70개 청크, 37,792
vertices였다. 필지 fit bounds 전체를 다시 맞춘 viewport에서는 270개 청크, 75,146 vertices로
완료됐고 외곽 envelope 포함 검사도 통과했다. 두 결과는 다른 viewport 조건의 측정치다. 이는
실제 Qt 화면 픽셀 모양을 증명하지 않으므로, 새 desktop-native
빌드에서 두 레이어를 로드해 외곽선과 `ready` 상태를 확인해야 한다.

2026-10-03 레이어 수명주기와 축척 연속성 변경은 네이티브 Go 테스트,
QML 테스트 27개, `desktop-native` 빌드까지 통과했다. 테스트는 같은 GUI 프레임의
복수 파일 선택 이벤트, 로드 중 대기열과 실패 후 다음 요청, 취소 시 대기열 삭제,
기존 세계좌표 중심·미터/픽셀 보존, EPSG:5186의 가로·세로 축척 일치를 포함한다.
실제 macOS 창은 이 실행 환경의 화면 자동화에서 보이지 않아 아직 육안 검증하지 못했다.
사용자 화면에서 세종 연속지적도와 도근점을 빠르게 연달아 추가하고, 두 레이어가
모두 남는지와 화면 중심·축척이 유지되는지 확인해야 한다. 우클릭 메뉴 바깥 클릭
닫기 및 레이어 제거 후 원본 SHP가 그대로 있는지도 확인한다.
같은 날 두 실제 SHP를 읽는 확대 렌더 회귀를 다시 실행했다. zoom 194.44에서
96청크·연속지적도 2,303,588 vertices, zoom 840에서 84청크·294,778 vertices로
모두 정점 안전 상한 아래에서 연속지적도가 표시 대상에 남았다. 이는 실제 화면의
축척·픽셀 출력 검증을 대체하지 않는다.
빠른 파일 선택 대기열, 취소, 미적용 metadata의 화면 중심 보존 및 레이어 제거
회귀는 `go test -race -tags 'qt native'` 표적 실행도 통과했다.
후속으로 오프스크린 Qt 창에 실제 `GoGIS.MapCanvas`를 생성하는 브리지 통합
테스트를 추가했다. 한 GUI 프레임에 QML 요청 두 건을 기록했을 때 C++ 장면 그래프
동기화가 두 요청을 모두 순서대로 캡처하고 Go가 한 번만 소비하는지 확인한다.
`go test -tags 'qt native' ./...` 전체와 QML 테스트 27개가 통과했다. 다만 이
통합 테스트도 사용자의 실제 창에서 렌더링된 지도 픽셀과 메뉴 조작을 확인하지는 않는다.
속성창은 별도 Qt Quick Test 오프스크린/Basic 스타일로 한국어 레이블·심볼 설정
페이지를 캡처해 배치를 확인했다. 레이블 배치·회전·숫자 입력의 시작 열을 통일하고
남아 있던 영어 배치 선택지와 Lua 설명을 번역한 뒤, QML 레이아웃/번역 회귀
27개와 `desktop-native` 빌드를 다시 통과했다. 이 캡처는 macOS 네이티브
컨트롤 스타일의 실제 사용자 화면을 대체하지 않는다.
