# 사용자 개입 후속 확인 목록

자동으로 검증할 수 없는 CAD 호환성, 대상 OS 설치 동작, 네트워크가 필요한
취약점 조회를 한곳에 모은다. 자동 검증 결과와 실제 사용자 환경의 결과를
혼동하지 않도록 각 항목에 환경과 증거를 기록한다.

## 자동 검증 현황 (2026-10-01, macOS ARM64)

`GOCACHE=/private/tmp/gogis-go-cache ./scripts/verify.sh` 통과: 일반 Go 테스트와
vet, race 테스트, native 태그 테스트/race 테스트, native 빌드, Qt native 테스트,
QML 테스트 12개, `git diff --check`. 테스트 중 `Sans Serif` 대체 폰트 관련 Qt
경고 1건이 있었지만 실패 테스트는 없었다. 별도의 키/토큰 형태 secret 패턴 검색은
일치 항목이 없었다(휴리스틱 검색이며 secret scanner 전체 검사를 대체하지 않는다).
`go mod verify`도 통과했으나, Go 취약점 DB는 DNS 차단으로 조회하지 못했으므로
아래 온라인 검증은 여전히 남아 있다. `govulncheck`는 PATH에 설치되어 있지 않았지만
로컬 module cache의 v1.8.0 source로 `/private/tmp/govulncheck`를 빌드했다. 최초 설치 방식은
`proxy.golang.org` DNS 조회가 차단됐고, 캐시된 실행 파일로 재시도한 portable/native/qt,native
분석도 모두 `vuln.go.dev/index/modules.json.gz` 조회 단계에서 막혔다. 따라서 취약점 결과는
없음이 아니라 미검증이다.

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
feature를 result slice에 보관하지 않고 요청 전체를 오류 처리한다. 다만 WKB serialization 전
OGR feature 및 단일 WKB 크기를 알 수 있는 GDAL Go 바인딩이 없어, 단일 거대 피처 순간 할당은
사전 제한하지 못한다. WKB 출력 버퍼의 C `malloc` 실패는 exporter에 null 포인터를 넘기지
않고 GDAL 오류로 반환하지만, 이는 OGR geometry 자체의 입력 시 native 메모리 사용량을
제한하지 않는다. 저메모리 상태에서 GDAL 오류 문자열의 `malloc`/`realloc` 실패도 null write로
이어지지 않도록 처리했고, raster band/layer 목록 및 color table 복사 버퍼의 C 할당 실패와
크기 산술 오버플로 검사도 추가했다. 이들 실패 경로를 실제 시스템 OOM으로 강제 주입한 것은
아니며, 성공 경로 빌드와 native 테스트 통과만 확인했다.
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
감소했다. 기본 경로는 속도를 우선해 per-feature scan으로 유지했다. 두 방식 모두
GeoJSON에 spatial index를 만들지 않으므로 각 viewport query가 최대 백만 피처를 다시
스캔할 수 있다. 결과는 1M synthetic Point dataset 하나의 Mac benchmark이지 SHP,
복잡한 polygon, 대화형 Qt frame rate의 대표값이 아니다.
benchmark에서 모든 feature를 Go에 상주시킨 snapshot은 0개였고, `runtime.GC()` 후
active runtime의 Go `HeapAlloc`은 약 1.13 MiB였다. 5.26 GiB peak는 Go retained heap과
viewport payload로 설명되지 않는 GDAL/OGR 및 기타 native memory 사용을 보여준다. 이
측정은 OOM 안전성을 보장하지 않는다. `ps`는 sandbox 권한상 차단되고
`/usr/bin/time -l`도 제한됐지만, 프로세스 자체의 `getrusage` 측정은 가능했다.

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

자동 사전검증 및 GDAL 재읽기는 통과했지만 ARES 실제 앱 결과는 아직 없다.
검증 호스트는 macOS 27.2이며 ARES 앱을 찾지 못했다. Graebert가 게시한 ARES
Commander 2027 SP1 macOS 지원 목록은 14, 15, 26이므로 현재 호스트에서 성공을
가정하지 않는다. 공식 시스템 요구사항과 배포 OS는
[ARES 검증 절차](ares-commander.md)를 참조한다.

지원되는 Windows/macOS/Linux ARES Commander 2027에서 다음 두 파일을 각각 연다.

- `testdata/ares/sample-utf8.dxf` — SHA-256 `75e9325afdd626a505f5bcf5ef8ca02f6b8d919c55700c155d6eff8e9f0b720a`
- `testdata/ares/sample-cp949.dxf` — SHA-256 `63a7484b44cd9f55d4251f2f58f0c1711212bdf484512dfaf4e94f544f0c5a06`

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
