# ADR 0004: 대량 도형 import 성능 최적화 1차

- 상태: 1차 최적화 적용
- 기준일: 2026-09-29
- 범위: GDAL feature snapshot과 WKT 기반 render source 생성

## 관찰된 경로

현재 native import는 `GDAL feature → WKT 문자열 → core.Feature → WKT parser →
정규화 render source` 순서다. GDAL 바인딩에 좌표 배열을 직접 반환하는 공용
API가 없으므로 이번 단계에서는 core 경계를 깨지 않고 할당과 선형 탐색을
줄였다.

## 적용한 최적화

- GDAL `Layer.FeatureCount()`가 제공하는 경우 `core.Layer.Features`를 최종
  용량으로 미리 할당한다. count를 제공하지 않는 streaming/filter driver는
  기존 동작으로 fallback한다.
- `NewLayerSources`의 중복 layer name 검사를 prefix 선형 탐색에서 map 기반
  O(n) 검사로 변경했다.
- WKT 숫자 파서를 rune 순회와 `strings.Builder` 조합에서 ASCII byte scanner로
  변경했다. 임시 문자열 builder를 제거하면서 지수 표기와 부호를 유지한다.
- WKT coordinate scanner는 임시 `[]float64` 숫자 배열을 만들지 않고 x/y pair를
  바로 Point slice에 기록한다. 단일-part `Parts` compatibility view는 기존
  semantics를 유지한다.
- 일반적인 정수·소수 WKT token은 작은 decimal parser로 처리하고, 지수 표기,
  malformed token, overflow는 `strconv.ParseFloat`로 fallback한다. GIS 좌표의
  일반 경로를 빠르게 하면서 scientific notation과 오류 semantics를 유지한다.
- 단일 part feature에서는 `HitFeature.Parts[0]`와 호환용 `Vertices`가 같은
  normalized slice를 공유하게 해 좌표 복사를 제거했다. MultiLineString,
  MultiPolygon, GeometryCollection 등 여러 part는 기존처럼 평탄화 view를
  별도로 만든다.
- `parseLayerPoints`가 소유한 좌표 slice를 `newLayerSource`에서 제자리
  normalize해 raw point 복사와 normalized part slice 생성을 제거했다.
- 정규화 단계의 Liang–Barsky clipping은 반복 boundary 배열을 만들지 않고
  boundary helper를 직접 호출해 동일한 visible segment semantics를 유지한다.
- WKT geometry type dispatch는 매 feature마다 `strings.ToUpper` 결과를 만들지
  않고 ASCII case-insensitive prefix/suffix scan을 사용한다. 100K LineString
  기준 parse 단계가 약 12.1ms에서 9.1ms로, 전체 WKT LayerSource가 약
  16.8ms에서 12.8ms로 감소했고 allocation 수는 변하지 않았다.
- 동일한 비어 있지 않은 `LINESTRING`만 포함한 WKT layer는 layer 단위 fast path로
  전환해 feature마다 geometry type dispatch를 반복하지 않는다. 최신 100K
  benchmark에서 parse 단계가 약 6.81ms에서 6.34ms, 전체 source 생성이
  약 11.40ms에서 11.08ms로 줄었고 allocation 수는 유지됐다. `POINT`는
  별도 layer fast path가 측정상 유의미한 개선을 만들지 않아 일반 경로를 유지한다.
- chunk 분할에서 수평·수직 segment는 Liang–Barsky clipping을 호출하지 않고
  cell 경계를 직접 계산하도록 했다. 10K axis-aligned polygon source 생성이
  약 7.63ms에서 6.62ms로 줄었고, vertex 수와 allocation semantics는 유지된다.
  대각선 segment는 기존 clipping 경로를 사용한다.
- generic polygon/multipart chunk slice는 1.5배 성장 대신 2배 성장으로 바꿨다.
  10K axis-aligned polygon에서 중간 복사 횟수가 줄어 source 생성이 약
  6.62ms에서 6.16ms, 20.2MB/400 allocations에서 17.7MB/256 allocations로
  감소했다. sparse chunk의 과예약을 피하기 위해 exact hint 경로는 그대로
  우선 적용한다.
- homogeneous WKT multipart/collection parser는 layer-level type 검증 뒤에
  다시 수행하던 `TrimSpace` scan을 제거했다. 10K WKT MultiPolygon은 약
  8.62ms/21.2MB/309 allocations에서 8.47ms/20.0MB/200 allocations로,
  GeometryCollection은 약 6.14ms/13.3MB/253 allocations에서
  5.93ms/11.3MB/163 allocations로 감소했다.
- WKB render parser는 feature마다 모든 specialized parser의 header를 순서대로
  재검사하지 않고 base geometry type을 한 번 읽어 직접 dispatch한다. 최신
  benchmark에서 100K WKB LINESTRING source가 약 8.7ms에서 7.09ms,
  10K WKB MULTILINESTRING이 약 6.24ms에서 5.82ms로 줄었다. specialized
  parser가 malformed/extended 입력을 처리하지 못하면 기존 generic WKB
  fallback을 그대로 사용한다.
- GDAL reader가 geometry를 WKT 문자열 대신 `core.WKBGeometry`로 보관하도록
  변경했다. render 경계는 WKB를 `render.Point`로 직접 decode하고, GEOS·PROJ·
  DXF·저장처럼 WKT가 필요한 경계에서만 `core.ToWKT`를 호출한다.
- GDAL reader에 `OpenGeometryOnly` 경계를 추가해 렌더 전용 로딩에서는
  feature property map과 field schema를 만들지 않도록 했다. 편집·속성표처럼
  속성이 필요한 기존 `Open`/`OpenAll` 경로는 그대로 유지한다.
- 다중 레이어에도 같은 정책을 적용할 수 있도록 `OpenAllGeometryOnly`를
  추가했다. 현재 desktop 기본 로딩은 저장/편집 semantics를 보존하기 위해
  `OpenAll`을 계속 사용하며, read-only render session에서 전환할 수 있다.
- desktop native에 `--read-only` 모드를 연결했다. 이 모드는
  `OpenAllGeometryOnly`와 원본 GDAL attribute page를 사용하고 편집/저장을
  차단한다. 기본 모드는 기존 편집 semantics를 유지한다.
- `FeatureReader.OpenFeature`를 추가해 geometry-only 로딩 후 선택된 feature의
  속성만 순차 ID 기준으로 다시 읽을 수 있게 했다. 데이터 원본은 다시 열지만
  선택 전까지 전체 속성 map을 보존하지 않는다.
- Qt attribute table은 같은 레이어를 반복 선택할 때 기존 JSON payload를
  재사용하도록 캐시했다. 레이어 전환·편집·데이터 교체 때만 전체 행을 다시
  직렬화한다.
- Qt read path에서 전체 `Project()` deep clone을 제거하고 선택 layer만
  조회하도록 했다. attribute table은 geometry 없는 properties-only snapshot을
  사용하며, feature 존재·이름 조회는 직접 조회 API를 사용한다.
- attribute table payload를 200행 단위 페이지로 제한하고 QML Previous/Next
  요청을 Go bridge로 전달하도록 했다. 큰 레이어도 초기 JSON payload와 QML
  ListModel이 전체 행을 동시에 만들지 않는다.
- `presentation.AttributeTable`은 schema에 선언된 key만 있는 일반 page에서
  undeclared-field 수집 map을 만들지 않고, 실제 미등록 key가 발견될 때만
  lazy allocate한다. 200행 complete-schema benchmark는 약 27.0us/70.7KB/
  402 alloc으로 측정되고, undeclared-field 경로는 약 34.0us/71.1KB/405 alloc으로
  별도 비용을 유지한다.
- property filter는 expected 문자열의 bool/int/uint/float 파싱을 feature마다
  반복하지 않고 filter 시작 시 `propertyMatcher`로 한 번만 수행한다. 10K
  numeric property filter benchmark는 약 84.2us/148B/3 alloc으로 측정되며,
  string·typed numeric 비교 semantics와 public clone 경계는 유지한다.
- 파일 기반 Qt runtime의 페이지 요청은 `ProjectService` snapshot 대신
  GDAL `OpenAttributePage`를 사용한다. GDAL은 앞선 feature를 ID 유지 목적상
  스캔하지만, 그 feature의 geometry와 properties는 materialize하지 않는다.
- render source chunk builder는 immutable source-owned vertex slice를 직접
  반환하고, `BatchStore.Apply`에서만 소유권 경계 복사를 수행한다. 기존
  builder와 BatchStore의 이중 vertex copy를 제거했다.
- 단순 WKB Point/LineString은 `[][]Point` 외부 wrapper를 만들지 않고 flat
  point slice로 유지한다. 복합 WKB와 기존 WKT의 multi-part 구조는 그대로
  보존해 hit-test semantics를 바꾸지 않는다.
- desktop render loop는 chunk 결과마다 전체 batch를 Qt로 복사하지 않고
  약 16ms frame budget으로 결과를 묶어 publish한다. 마지막 batch는 요청 종료
  시 강제로 publish해 완료된 geometry가 누락되지 않도록 한다.
- Qt scene-graph bridge는 repaint마다 shared vertex vector를 임시 snapshot으로
  복사하지 않고 QSG geometry buffer에 직접 변환한다. Go→C++ 경계의 XY scratch
  buffer도 최대 크기를 재사용해 대형 batch publish의 반복 allocation을 줄인다.
- `BatchStore`는 viewport 요청 시점의 chunk 순서를 보존해 `Current()`에서
  매번 map key를 정렬하지 않도록 했다. 반환 vertex slice의 복사 semantics는
  유지하면서 flatten capacity를 먼저 계산한다.
- `BatchStore`는 ordered key와 함께 ordered vertex slice/index를 유지해
  `CurrentInto()` flatten 시 chunk마다 map lookup을 반복하지 않는다. Apply와
  generation 전환 때 index를 갱신하며, 반환 destination copy semantics는
  유지한다.
- render publish 경로는 `BatchStore.CurrentInto()`로 flatten destination을
  request 수명 동안 재사용한다. 기존 `Current()`의 독립 복사 API는 유지하고,
  synchronous renderer adapter에는 반복 allocation 없는 경로를 사용한다.
- read-only desktop runtime은 geometry-only GDAL snapshot을 `ProjectService`에
  다시 복제하지 않고 layer명/CRS metadata만 보관한다. geometry와 WKB는 render
  source가 소유하고, attribute/selection 값은 원본 GDAL lazy reader에서
  필요할 때 읽어 대용량 import의 중복 메모리를 제거한다.
- hit-test uniform-grid index는 초기 렌더 준비 단계에서 만들지 않고 첫
  사용자 선택 시 lazy-build한다. 렌더가 먼저 표시되어야 하는 대용량 입력에서
  초기 CPU·메모리 비용을 선택 동작 시점으로 이동한다.
- layer tree처럼 이름만 필요한 UI read path에는 `ProjectService.LayerNames()`를
  사용한다. 전체 `Project()` deep clone을 호출하지 않아 geometry/properties
  snapshot 복사를 피한다.
- native Qt 시작 시 대용량 입력의 GDAL snapshot을 GUI 이벤트 루프 전에
  동기 실행하지 않는다. 가벼운 초기 runtime으로 QML을 먼저 띄운 뒤 백그라운드
  로더가 완성된 runtime을 원자적으로 교체한다. 파일 열기 실패도 이벤트 루프를
  막지 않고 상태 메시지로 전달한다.
- 파일 열기 요청은 cancellable context와 generation으로 관리한다. 새 파일이
  요청되면 이전 GDAL read/CRS transform을 취소하고, 늦게 끝난 결과는 현재
  runtime을 교체하지 않는다. 초기 command-line load와 drag-and-drop이 같은
  비동기 경로를 사용한다.
- Lua layer listing과 label 생성 command도 필요한 범위에 맞춰 metadata 또는
  단일 layer snapshot API를 사용한다. 전체 project clone이 필요한 merge/filter/
  spatial 연산과는 구분한다.
- `ProjectService.BeginEdit()`는 project/layer header만 분리하고 feature slice와
  property map은 실제 mutation 시 copy-on-write한다. rollback 시 committed
  project가 공유 데이터를 통해 오염되지 않도록 SetFeatureProperty/AddFeature가
  대상 layer/feature를 먼저 분리한다.
- `ProjectService.SaveLayer()`는 저장 대상 layer만 writer 경계 전에 detached
  clone한다. 전체 project snapshot을 만든 뒤 target layer를 다시 clone하던
  중복 snapshot을 제거하며, writer가 받은 layer를 수정해도 committed project는
  보호된다.
- public `GenerateLabels()`는 계속 detached 결과를 반환하지만, 이미
  `ProjectService.Layer()`에서 detached된 layer를 처리하는 내부 경로는 두 번째
  geometry/property clone을 건너뛴다. 최종 `AddLayer()` 경계 clone은 유지한다.
- label 대표점 좌표 파서는 숫자별 `strings.Builder`와 임시 float 배열을
  제거하고 WKT byte scan 중 `labelPoint` 쌍을 직접 만든다. 10K feature 기준
  public 경로는 3.00ms/5.03MB/80,004 alloc에서 2.03ms/4.87MB/60,004 alloc으로,
  내부 owned 경로는 2.03ms/1.11MB/50,001 alloc에서 1.22ms/0.95MB/30,001
  alloc으로 감소했다. 지수 표기와 malformed/odd coordinate 오류 테스트도
  추가했다.
- 표준 2D WKT `POINT` label은 대표점용 좌표 slice를 만들지 않고 두 숫자를
  직접 읽는다. 10K feature 내부 owned benchmark가 최신 측정에서 약
  0.93ms/1.21MB/10,003 alloc에서 약 0.79ms/1.05MB/3 alloc으로 줄었고,
  malformed/추가 좌표는 기존 오류 semantics를 유지한다.
- WKT LINESTRING/POLYGON 대표점 계산도 최대 16개 좌표쌍을 stack scratch에서
  처리하도록 바꿨다. 10K 3점 line은 약 1.61ms/1.05MB/3 alloc, 10K 5점
  polygon은 약 2.07ms/1.05MB/3 alloc으로 측정되며, 더 큰 geometry는 기존
  heap 확장 경로를 사용한다.
- `LabelProjectLayer()`도 committed layer를 직접 읽고 feature header 배열만
  복사한 뒤 label을 계산한다. source geometry/property clone을 피하면서도
  label 필드 변경이 원본에 반영되지 않도록 했으며, 10K feature 기준
  snapshot baseline의 2.00ms/5.28MB/60,005 alloc을 1.23ms/1.35MB/30,002
  alloc으로 줄였다.
- label 결과의 `core.Label`은 feature마다 별도 heap 객체를 만들지 않고 결과
  layer 소유의 label arena에 배치한 뒤 feature pointer가 arena 원소를 가리킨다.
  10K feature 내부 owned benchmark가 약 20,002 alloc에서 10,003 alloc으로,
  WKB POINT public 경로도 50,002 alloc에서 40,003 alloc으로 감소했으며,
  반환 layer가 arena를 계속 소유하므로 label pointer lifetime은 유지된다.
- `FilterLayerByProperty()`는 source 전체를 먼저 clone하지 않고 schema와 일치한
  feature만 detached clone한다. `FilterProjectLayer()`도 전체 project snapshot
  대신 named layer snapshot만 사용한다.
- `FilterProjectLayer()` 내부 경로는 committed named layer를 직접 읽고 일치
  feature만 얕게 결과에 담은 뒤 `AddLayer()`에서 한 번 소유권 복사한다.
  10K feature 중 1개 일치 기준 내부 필터 단계는 약 95us/144B/2 alloc이며,
  전체 layer clone baseline의 약 813us/3.92MB/30,005 alloc 비용을 피한다.
- `ApplySpatialOperation()`은 전체 project snapshot 대신 named input layer만
  detached clone한다. binary operation에서 같은 layer를 양쪽에 지정하면 이미
  만든 snapshot을 재사용한다.
- Lua DXF export와 `MergeProjectLayers()`도 전체 project snapshot 대신 실제로
  export/merge할 named layer만 detached snapshot한다. 결과 layer 생성 시의
  detached clone 경계는 유지한다.
- `MergeProjectLayers()`의 내부 경로는 committed layer를 읽기 전용으로 검증한
  뒤 결과 feature를 얕게 조립하고 `AddLayer()`에서 한 번만 소유권 복사한다.
  공개 `MergeLayers()`의 detached 결과 계약은 유지한다. 10K feature 두 개
  병합 benchmark는 0.664ms/2.80MB/15,064 alloc에서 0.256ms/1.04MB/64 alloc으로
  줄었다.
- GDAL spatial window에 geometry-only 변형을 추가한다. viewport 로딩은
  geometry를 먼저 가져오고 attribute reader가 필요한 시점에 properties를
  조회할 수 있어야 window loading의 메모리 이점이 유지된다.
- GDAL reader의 feature별 closure/defer 해제를 명시적 `Close()`로 바꾸는
  실험은 10K GeoJSON 반복 benchmark에서 full/geometry-only 모두 안정적인
  개선을 만들지 못했다. 현재 병목은 Go loop보다 godal/OGR native feature와
  geometry 접근에 있으므로, 자원 수명 보장과 가독성을 위해 기존 구조를
  유지한다.
- GDAL geometry-only microbenchmark에서 10K GeoJSON 기준 `NextFeature`만 약
  16.1ms, `Feature.Geometry` 포함 약 16.7ms, `Geometry.WKB`까지 포함 약
  18.6~19.8ms로 측정됐다. 전체 reader의 약 45ms 중 WKB serialization은
  일부이고, 나머지는 dataset open/GeoJSON parsing/OGR feature iteration이다.
  go-godal에 좌표 bulk extraction API가 없는 상태에서 임의의 native pointer
  보관이나 FID 가정을 도입하지 않고 현재 WKB ownership 경계를 유지한다.
- 2026-09-30 재측정에서 동일한 10K GeoJSON geometry-only 전체 import는
  `BenchmarkReaderOpenGeometryOnlyGeoJSON10KPoints` 기준 약 60.3ms/회,
  1.32MB/70,013 alloc이었다. 1% 정도만 포함하는 bounds를 사용한
  `OpenWindowGeometryOnly`도 약 73.0ms/회, 134KB/7,032 alloc으로 더 느렸다.
  현재 경로는 OGR SQL result set 생성과 GeoJSON 재파싱 비용이 spatial filter
  절감보다 커지는 입력 형식이 있으므로, window API를 무조건 초기 로딩에
  연결하지 않는다. SHP/GeoPackage처럼 드라이버 공간 인덱스를 활용할 수 있는
  실제 파일에서 별도 benchmark를 확보한 뒤 driver별 정책으로 연결한다.
- 위 결과에 따라 `.geojson`/`.json` 입력에 한해 OGRSQL 대신 한 번의
  `NextFeature` 순회에서 GDAL geometry envelope를 먼저 검사하는 경로를
  추가했다. 같은 benchmark에서 window import는 약 58.6ms/회, 878KB/45,026
  alloc으로 줄었다. 이 최적화는 feature ID의 read-order 계약과 WKB 소유권을
  유지하며, SHP/GeoPackage의 SQL spatial-filter 경로에는 적용하지 않는다.
- 10K point GeoPackage benchmark에서는 전체 geometry import가 약 22.0ms/회,
  1.32MB/70,013 alloc, window import가 약 5.6ms/회, 134KB/7,034 alloc으로
  측정됐다. 따라서 GeoPackage에서는 기존 OGRSQL spatial filter를 유지하는
  것이 유리하다. 이 결과는 포맷별 파서/공간 인덱스 비용이 다르다는 근거이며,
  단일 window 구현을 모든 드라이버에 강제하지 않는 기준으로 삼는다.
- 10K point Shapefile benchmark에서도 전체 geometry import 약 15.4ms/회,
  1.32MB/70,013 alloc 대비 window import 약 11.4ms/회, 134KB/7,034 alloc으로
  측정됐다. 현재 테스트 파일에는 별도 `.qix` 공간 인덱스를 만들지 않았으므로,
  인덱스가 있는 실제 업무 SHP에서는 추가 이득을 기대할 수 있지만 별도 검증이
  필요하다.
- GDAL 드라이버 registry는 process-global인데 reader/writer/session 호출마다
  `GDALAllRegister()`를 반복하고 있었다. 이를 `sync.Once`로 감싸 초기화 경계를
  한 번으로 고정했다. dataset 자체의 수명·mutex 정책과 분리되므로 반복 open의
  plugin-discovery 비용만 줄이고, dataset 동시성 계약은 변경하지 않는다.
- 반복적인 viewport window 요청을 위해 `GeometrySession`을 추가했다. 10K point
  GeoPackage window benchmark에서 standalone open 경로는 약 6.2ms/회,
  retained dataset session은 약 2.5ms/회로 측정됐다. allocation은 각각
  약 134KB/7,034과 133KB/7,027로 거의 같으므로 핵심 이득은 dataset open과
  driver parsing 재초기화 제거다. OGR layer reading state가 mutable하므로
  session 호출은 mutex로 직렬화하고, `Close`는 반복 호출에 안전하게 했다.
- `GeometrySession`은 동일한 layer를 반복하는 viewport 요청에서 `LayerByName`
  호출과 layer handle 해석도 재사용한다. GeoJSON 10K point window session
  benchmark가 약 19.9ms에서 약 16.9ms로 줄었고, layer 이름이 바뀌면 다시
  해석하여 multi-layer session semantics를 보존한다.
- `AttributeSession.OpenFeature()`는 같은 layer에서 증가하는 feature ID를
  요청할 때 OGR reading cursor를 이어서 사용하도록 바꿨다. 역방향 ID, layer
  변경, attribute page 요청, 취소/읽기 오류에서는 cursor를 무효화하고 다음
  요청을 처음부터 재시작한다. GeoPackage 10K feature를 1번부터 순차 조회하는
  benchmark가 약 10.7s/회, 401MB/50,075,017 alloc에서 약 11.3ms/회,
  1.00MB/80,001 alloc으로 줄었다. 이는 feature 선택·속성 조회 경로의
  O(n²) 재스캔을 제거한 결과이며, 반환 feature ID 계약은 유지한다.
- `AttributeSession`도 동일한 layer의 반복 feature 조회에서 `LayerByName`
  결과를 재사용한다. GeoJSON 10K feature 선택 benchmark가 최신 Apple M3에서
  약 2.81µs/975B/19 alloc에서 약 2.43µs/966B/17 alloc으로 줄었고, 요청
  layer가 바뀌면 새 handle을 해석한다.
- attribute page 결과는 요청 `limit`만큼 feature slice capacity를 선할당한다.
  10K GeoJSON을 200행씩 50페이지 순회하는 benchmark에서 약
  10.0MB/120,138 alloc에서 약 9.2MB/119,638 alloc으로 줄였으며, GDAL
  parsing 시간이 지배적인 경로라 시간 차이는 측정 오차 범위로 취급한다.
- 동일 layer의 retained JSON attribute session은 첫 page에서 계산한 field
  schema를 캐시해 이후 page마다 field name sort를 반복하지 않는다. capacity
  선할당 이후 benchmark가 약 22.76ms에서 약 21.77ms로 줄었고, schema는
  반환 layer에 복사해 caller mutation이 session cache를 오염시키지 않도록 했다.
- 선택 ID가 순차적이지 않은 일반 클릭 패턴도 retained session에서 측정했다.
  10K GeoJSON random-cycle lookup은 약 2.96us/feature인 반면 standalone
  dataset open 경로는 약 128ms/feature였다. GeoPackage 10K sequential session
  lookup은 약 7.4ms/회였다. 따라서 현재 session cursor 비용은 추가 feature-ID
  인덱스를 도입할 수준의 병목이 아니며, dataset/session 수명 최적화를 우선한다.
- GeoJSON 속성 페이지도 같은 원리로 session 내부 cursor를 이어서 사용한다.
  SQL `LIMIT/OFFSET`를 매 페이지 다시 생성하던 10K feature 50페이지 benchmark가
  약 406ms/회에서 약 26.4ms/회로 줄었다. 페이지가 순차적이지 않거나 feature
  선택이 끼어들면 cursor를 reset하므로 랜덤 페이지 semantics는 유지한다.
- 레이블 대표점 계산도 `WKBGeometry -> WKT 문자열 -> 좌표 재파싱`을 거치지
  않고 WKB parts를 직접 사용하도록 바꿨다. 10K WKB point label benchmark가
  약 4.38ms/회, 100,005 alloc에서 약 2.24ms/회, 80,005 alloc으로 줄었다.
  기존 WKT 경로와 polygon ring 계산 semantics는 유지한다.
- 레이블 속성값이 이미 문자열인 일반 경로에서는 `fmt.Sprint`를 거치지 않고
  직접 trim하도록 했다. 10K WKT point label benchmark가 최신 측정에서 약
  2.84ms/회, 50,002 alloc으로 줄었고, WKB 경로도 약 2.20ms/회,
  70,002 alloc으로 줄었다. 문자열 이외의 값과 `fmt.Stringer`는 기존 출력
  semantics를 유지한다.
- `core.Layer.Clone()`도 레이어의 모든 feature label을 feature별 heap 객체로
  복제하지 않고 layer 수명의 label arena에 배치한다. `Feature.Clone()`의
  독립 복제 계약과 properties/geometry detached semantics는 유지한다. 10K
  labeled feature benchmark는 약 182.8us/1.05MB/2 alloc으로 측정됐다.
- `Scheduler`의 mixed cache/miss 요청에서 매번 생성하던 cached-key와 missing-key
  임시 slice를 요청 완료 후 반환하는 pool로 재사용한다. 결과 channel과 chunk
  ownership은 그대로 유지하면서 Apple M3 benchmark가 약
  953us/2.23MB/1,447 alloc에서 약 255us/875KB/31 alloc으로 줄었다. 전체
  cache-hit 경로는 별도 임시 버퍼를 만들지 않는다.
- `LayerVisibility.FilterChunkKeysInPlace()`는 all-visible 입력의 즉시 반환을
  유지하면서 첫 숨김 키 이후 suffix를 write-index compaction한다. 기존 in-place
  ownership과 layer tree 순서를 유지하며 10K 교차 키 benchmark가 약 81.1us에서
  77.3us로, all-visible benchmark가 약 68.4us에서 66.5us로 줄고 allocation은
  계속 0이다.
- `core.WKTGeometry.GeometryType()`의 표준 ASCII geometry token은 매번
  `strings.ToUpper` 문자열을 만들지 않고 canonical 상수를 반환하도록 했다.
  표준 `LINESTRING` 10만 회 benchmark가 약 2.41ms/1.60MB/100,000 alloc에서
  약 1.82ms/0B/0 alloc으로 줄었고, 비표준 token은 기존 uppercase fallback을
  유지한다.
- DXF export는 feature의 각 code/value마다 CP949 encoder를 새로 만들지 않고
  export 호출 단위의 encoder를 재사용한다. 10만 문자열 변환 benchmark가 약
  10.43ms/7.2MB/400,000 alloc에서 약 6.25ms/3.2MB/200,000 alloc으로 줄었고,
  UTF-8 및 CP949의 오류·출력 semantics는 유지한다.
- DXF WKT dispatch도 전체 좌표 문자열을 `strings.ToUpper`로 복사하지 않고
  geometry type prefix와 `EMPTY` suffix만 ASCII case-insensitive scan한다.
  10만 좌표 LineString write benchmark는 약 6.07ms/5.3MB/100,026 alloc이며,
  숫자 parsing/formatting과 geometry 출력 semantics는 그대로 유지한다.
- DXF group code line 출력은 `fmt.Fprintf` 대신 `bufio.Writer.WriteByte`로
  정수를 직접 기록한다. 10만 code benchmark가 약 3.00ms/8.7KB/0 alloc에서
  약 0.69ms/2.0KB/0 alloc으로 줄었으며, 음수 code도 기존 정수 출력 semantics에
  맞게 처리한다.
- DXF 숫자 parser는 고정 capacity 8에서 시작해 반복 확장하던 slice를 WKT
  문자열 길이로 보수적으로 추정한다. 10만 좌표 benchmark가 약
  2.81ms/4.10MB/25 alloc에서 약 2.65ms/1.01MB/1 alloc으로 줄었고, 숫자
token 오류·scientific notation semantics는 유지한다.
- DXF `geometryGroups`와 render WKT `wktGroups`/`wktComponents`는 입력 길이
  기반 capacity hint로 결과 substring header slice의 반복 확장을 제거했다.
  100K component benchmark가 각각 약 1.20ms/4.34MB/25 alloc에서 약
  0.61ms/1.11MB/1 alloc으로 줄었다. substring은 원본 입력을 계속
  참조하므로 geometry parsing semantics는 변경하지 않는다.
- DXF WKT export에 숫자 전용 writer 경로를 추가해 `FormatFloat` 결과 문자열을
  매 좌표마다 생성하지 않고 stack buffer를 직접 출력하도록 했다. 기존
  callback benchmark는 약 6.02ms/2.21MB/100,002 alloc이지만 fast path는
  약 2.76ms/1.01MB/2 alloc으로 측정되며, 실제 `Exporter.Export`의 WKT
  geometry 경로에서 이 fast path를 사용한다.
- DXF export의 표준 WKB Point/LineString/Polygon 및 multi/ring geometry는
  WKB parts를 직접 entity로 기록해 WKB→WKT→숫자 재파싱을 피한다. Geometry
  Collection 등 직접 매핑하지 않는 타입은 기존 WKT 경계로 fallback하고,
  empty parts는 skip한다. WKB Point benchmark가 약 200.6ns/96B/4 alloc에서
  약 79.4ns/40B/2 alloc으로 줄었다.
- label 대표점의 WKT geometry type 경로에서 이미 uppercase를 반환하는
  `GeometryType()` 결과에 대한 중복 `strings.ToUpper`를 제거했다. 10K WKT
  line benchmark가 약 1.61ms에서 1.48ms로, polygon benchmark가 약 2.07ms에서
  1.96ms로 줄었고 allocation은 1.05MB/3 alloc으로 유지된다.
- render source parser가 확인한 단순 WKT `LINESTRING` 여부를 parsed metadata로
  전달해 normalize 단계에서 feature마다 원본 WKT `TrimSpace`/prefix scan을
  반복하지 않도록 했다. line flag는 parsed feature struct에 내장하지 않고
  별도 bool arena에 두어 구조체 padding 증가를 피했다. 100K line normalize
  benchmark가 약 3.99ms에서 3.15ms로 줄고 allocation 수는 36으로 유지되며,
  parse memory는 약 8.80MB에서 8.11MB로, 전체 source memory는 약 29.18MB에서
  28.48MB로 줄었다. 전체 source 생성 시간은 약 10.74ms 수준이다.
- 같은 WKT dispatch에서 `EMPTY` suffix와 `LINESTRING` prefix 판정 결과를 한
  feature 안에서 재사용해 parser의 중복 byte scan도 제거했다. 기존 오류 처리와
  geometry type 분기는 변경하지 않는다.
- WKT dispatch는 표준 type token의 첫 byte로 POINT/POLYGON과 LINESTRING을
  먼저 분기해 서로 무관한 prefix scan을 건너뛴다. 100K line parse benchmark가
  약 7.40ms에서 7.14ms로, 전체 source 생성이 약 10.74ms에서 10.54ms로
  줄었고 fallback geometry의 기존 처리 semantics는 유지한다.
- 표준 WKT dispatch는 generic coordinate parser와 분리된 `wktPointsIntoStandard`
  fast path를 사용해 type prefix 앞의 숫자 검증 scan을 생략한다. generic
  parser의 SRID/numeric-prefix validation은 그대로 유지하며, 100K line parse가
  약 7.14ms에서 6.45ms로, 전체 source 생성이 약 10.54ms에서 9.86ms로
  줄었다.
- DXF `geometryGroups`와 `geometryComponents`는 괄호·comma만 해석하므로 rune
  range 대신 byte scan을 사용한다. 50K component benchmark는 약
  1.37ms/4.34MB/25 alloc이며, nested grouping과 unbalanced-parentheses 오류
  semantics는 유지한다.
- render의 `wktGroups`와 `wktComponents`도 동일하게 ASCII 괄호·comma byte
  scan으로 통일했다. 50K MULTILINESTRING component grouping benchmark는 약
  1.49ms/4.34MB/25 alloc이며, multipart/geometry collection nesting과 오류
  semantics를 유지한다.
- 표준 2D WKB POINT/LINESTRING label 대표점은 일반 `Parts()` decoder 대신
  binary offset을 직접 읽도록 했다. 10K WKB POINT label benchmark가 약
  1.53ms/5.28MB/70,002 alloc에서 약 1.18ms/4.88MB/50,002 alloc으로 줄었고,
  line midpoint semantics와 확장/multipart WKB fallback은 유지한다.
- 표준 2D WKB POLYGON도 ring 좌표를 임시 `Parts()` slice로 materialize하지
  않고 직접 순회해 centroid를 계산한다. 단일 외곽 ring 10K label benchmark는
  Apple M3에서 1.49ms/5.60MB/50,002 alloc으로 측정됐고, degenerate·확장·malformed
  입력은 기존 polygon fallback으로 남겨 validation과 semantics를 보존한다.
- writable `ProjectService`의 `HasFeature`와 `FeatureProperty`가 선택마다
  layer 전체를 선형 순회하던 경로를, committed project 포인터에 연결된
  layer별 `featureID -> index` map으로 바꿨다. commit 시 index를 무효화하고
  다음 조회에서 재구축하므로 rollback과 property 변경의 snapshot 경계를
  유지한다. 10K feature에서 warm index 조회는 각각 약 54ns/52ns, 0 alloc이고,
  선형 baseline은 약 4.36us/4.27us로 측정됐다. 첫 조회의 index 구축 비용은
  반복 선택 비용과 분리해 benchmark warm-up에서 제외했다.
- read-only desktop load가 geometry snapshot을 만든 뒤 같은 source를
  attribute session으로 다시 열던 중복 open을 제거했다. `AttributeSession`이
  retained dataset으로 `OpenAllGeometryOnly`까지 수행하도록 하고 runtime이
  그 session을 이후 page/feature 조회에 계속 사용한다. 10K GeoJSON 기준
  기존 두 번 open 경로 약 129ms/회에서 단일 session 경로 약 22.5ms/회로
  줄었다. 실패·runtime 교체 시 session close ownership도 명시했다.
- JSON attribute session은 layer별 `FeatureCount` 결과를 cache해 순차 page마다
  전체 count를 다시 질의하지 않도록 했다. 10K GeoJSON 50페이지 benchmark가
  약 25.0ms/회에서 약 21.2ms/회로 줄었고, page total과 random offset 시
  cursor reset semantics는 유지한다.
- render scheduler는 chunk마다 goroutine을 생성하고 semaphore로 제한하던
  구조를 고정 4-worker 실행기로 바꾼다. 신규 key는 작업 channel을 거치지
  않고 atomic index로 분배해 queue 송수신 비용을 줄이며, 결과는 계속
  completion order로 전달하고 cancellation semantics는 유지한다.
- scheduler result channel은 대형 viewport에서 모든 결과를 한 번에 buffer하지
  않고 최대 32개로 제한한다. runtime은 결과를 계속 drain하고 새 viewport 시
  context를 취소하므로 bounded backpressure가 가능하며, peak memory를 줄인다.
- planner/visibility가 이미 중복 없는 chunk key를 생성하는 경로에는
  `Scheduler.RequestUnique()`를 사용한다. 일반 `Request()`의 방어적 dedup
  semantics는 유지하고, 모두 visible인 경우 visibility slice도 재사용한다.
- 모든 요청 chunk가 cache hit인 경우에는 cache snapshot을 한 번 수집한 뒤
  worker/jobs queue를 만들지 않는다. 작은 결과는 즉시 닫고, 큰 viewport는
  단일 sender goroutine만 사용한다. cache 무효화와 generation 경쟁은 snapshot
  경계로 격리하며 cancellation 통계도 기존 semantics를 유지한다.
- cache hit와 miss가 섞인 요청도 snapshot 단계에서 두 그룹을 분리한다. cached
  key를 worker queue에 다시 넣어 cache map을 재조회하지 않고, cached 결과를
  먼저 publish한 뒤 missing key만 fixed worker queue로 보낸다. all-cache 경로의
  compact `[]Chunk` snapshot backing storage는 `sync.Pool`로 재사용한다.
- 레이어 토글 시 Qt refresh가 이미 소유한 chunk key slice를 새로 복사하지
  않도록 `FilterChunkKeysInPlace` 경로를 추가했다. 10K key 중 절반을 숨기는
  benchmark가 약 110.5µs/401KB/1 allocation에서 약 77.6µs/0B/0 allocation으로
  줄었고, 모든 레이어가 visible인 경우에는 slice를 다시 쓰지 않는 fast path로
  약 65µs/0B/0 allocation을 측정했다. 기존 `FilterChunkKeys`의 입력 불변
  semantics는 유지한다.
- uniform-grid hit test는 일반 클릭의 작은 candidate 집합을 stack buffer에
  수용하고, dense cell이나 큰 tolerance에서만 heap으로 확장한다.
- hit index의 cell membership은 map 안에서 feature index slice를 개별적으로
  키우지 않고, cell별 linked range와 `int32` flat index/next 배열로 보관한다.
  일반적인 한두 cell membership capacity를 선할당해 한 번의 feature/cell
  순회로 구성하면서 대용량 선택 경로의 slice allocator pressure와 retained
  memory를 줄이고 query semantics는 동일하게 유지한다.
- 일반 viewport click의 candidate stack을 32개에서 256개로 늘려 benchmark의
  common tolerance가 heap으로 spill하지 않도록 했다. 256개를 넘는 dense
  cell/tolerance는 기존 동적 확장 경로를 사용한다.
- `HitIndex`는 생성 후 feature slice를 읽기 전용으로만 사용하므로, 호출자가
  불변성을 보장하는 계약 아래 feature 배열을 다시 복사하지 않는다. Qt 선택
  경로도 refresh가 feature slice를 교체하는 snapshot semantics를 사용해 클릭
  때마다 전체 feature 배열을 복사하지 않는다.
- Qt 선택 경로는 visibility 변경 시 만든 snapshot map을 재사용한다. 매 클릭
  `LayerVisibility` mutex를 획득하거나 visible-layer map을 새로 만들지 않는다.
  `HitTestScreenVisibleFunc`와 map 기반 API는 다른 호출자 호환성을 위해
  유지한다.
- Qt scene graph는 vertex batch generation과 canvas 크기를 추적한다. source
  geometry가 바뀌지 않은 repaint에서는 QSG vertex buffer 재할당·좌표 변환을
  건너뛰고, batch 또는 크기가 바뀔 때만 갱신한다.
- Qt bridge의 shared vertex storage는 `QPointF` 중간 표현 대신 float XY를
  직접 보관한다. Go→C++ publish와 QSG 변환 사이의 불필요한 타입 변환을
  제거하고, vertex count 기준으로만 QSG buffer를 관리한다.
- C++ shared float XY vector는 batch publish마다 새 vector를 만들지 않고
  기존 capacity를 `resize`로 재사용한다. batch가 커질 때만 capacity가
  증가하며, 이후 축소·동일 크기 publish에서는 heap allocation을 피한다.
- Qt bridge는 numeric-only `render.Vertex` layout을 동기적으로 C++에서 읽어
  XY만 shared vector에 복사한다. Go에서 별도 XY scratch를 채운 뒤 C++가 다시
  읽는 이중 순회를 제거하며, C++는 호출 후 Go pointer를 보관하지 않는다.
- `BatchStore.ApplyImmutable()`를 추가해 source/scheduler가 소유하고 변경하지
  않는 immutable chunk는 BatchStore가 vertex slice를 다시 복사하지 않도록 했다.
  외부 호출자와 mutable 결과에는 기존 `Apply()`의 defensive copy를 유지하고,
  desktop render publish 경로만 immutable fast path를 사용한다.
- `BatchStore.BeginGeneration`의 visible-key membership map은 viewport마다
  새로 만들지 않고 epoch marker를 갱신해 capacity를 재사용한다. ordered key
  slice도 기존 backing array를 재사용하며, 중복 visible key와 같은 generation
  재호출 semantics는 테스트로 고정했다.
- Qt runtime은 최근 8개 attribute page payload를 bounded cache로 유지한다.
  편집이나 파일 교체 때 cache를 비우며, 반복적인 Previous/Next 이동에서
  GDAL 재조회와 JSON 재생성을 피한다.
- read-only Qt runtime의 GDAL attribute/feature 조회는 하나의 serialized
  `AttributeSession`에서 dataset을 재사용한다. OGR layer reading state가
  mutable하므로 session 호출은 mutex로 직렬화하고, runtime 교체·취소 시
  dataset을 명시적으로 닫는다. writable runtime은 저장 후 stale dataset을
  보지 않도록 기존 per-call open 경로를 유지한다.
- read-only selection 상태에서 같은 layer/feature 이름을 다시 표시하는 경우
  마지막 feature name을 1-entry cache로 재사용한다. 새 파일/runtime 교체 때
  cache를 무효화하며, writable service property 경로에는 적용하지 않는다.
- Qt render progress status는 chunk completion마다 bridge를 호출하지 않고
  16ms frame cadence로 throttle한다. 초기 Loading, 마지막 Ready/Cancelled
  상태는 강제로 publish해 사용자 피드백과 terminal state를 보존한다.
- `ChunkPlanner.VisibleKeysInto()`를 추가해 viewport refresh가 레이어별 임시
  key slice를 만들지 않고 caller-owned backing array를 채우도록 했다. 기존
  독립 slice를 반환하는 `VisibleKeys()` API는 유지하며, native Qt runtime은
  visible layer snapshot을 한 번만 읽고 이 경로를 사용한다.
- PROJ 변환도 WKB 입력에서는 WKT 문자열과 정규식 숫자 파싱을 거치지 않고
  binary XY 좌표를 직접 변환하도록 했다. Z/M ordinate와 EWKB SRID metadata는
  보존하고, WKT 입력은 기존 호환 경로를 유지한다.
- PROJ WKT 변환도 전체 숫자 인덱스를 정규식으로 먼저 만들지 않고 byte scan으로
  XY 쌍을 즉시 읽고 변환한다. 10K point 기준 `10.03ms/3.31MB/110,011 alloc`
  에서 `3.74ms/2.72MB/60,001 alloc`으로 감소했으며 EPSG:4326 축 순서 semantics는
  유지한다.
- PROJ 변환 loop에서 source/target CRS의 위경도 축 여부도 feature/좌표마다
  문자열 비교하지 않고 transform 시작 시 한 번 계산한다. 10K point 기준
  WKB는 약 2.15ms에서 2.02ms로, WKT는 약 3.74ms에서 3.58ms로 감소했고
  allocation 수는 변하지 않았다.
- WKB POINT만으로 구성된 레이어는 feature마다 PROJ를 호출하지 않고 전체 XY를
  하나의 `ForwardFlatCoords` 호출로 변환한다. 복합 geometry와 혼합 WKT/WKB는
  기존 일반 경로를 유지한다. Apple M3 10K point benchmark가 약 2.02ms/
  881KB/20,004 alloc에서 약 0.94ms/1.29MB/20,006 alloc으로 줄었다. batch
  metadata는 byte-order와 좌표 offset만 보관해 초기 구현의 1.61MB peak를
  줄였다. 입력 WKB 버퍼는 복사 후 교체하므로 원본 불변성과 EPSG:4326 축
  순서를 유지한다.
- 표준 2D WKT `POINT`만으로 구성된 레이어도 WKB와 같은 batch PROJ 호출을
  사용한다. parser가 모든 feature를 확인해 fast path가 안전할 때만 적용하고,
  `LINESTRING`, Z/M, 복합 geometry, malformed formatting은 기존 delimiter 보존
  경로로 fallback한다. Apple M3 10K WKT POINT benchmark가 약
  3.76ms/3.13MB/60,003 alloc에서 약 2.00ms/1.77MB/20,007 alloc으로 줄었다.
- 표준 2D 단순 WKT `LINESTRING` 레이어도 전체 XY를 한 번에 변환한다. 출력은
  canonical `LINESTRING`으로 만들고, 복합 geometry나 차원/비정상 formatting은
  기존 경로로 fallback한다. Apple M3 10K 2점 line benchmark가 generic 경로의
  약 6.48ms/4.16MB/74,656 alloc에서 약 3.84ms/2.89MB/21,006 alloc으로
  줄었다. 좌표 parser도 layer-wide batch slice에 직접 append하며, 짧은 line은
  stack output buffer를 사용하고 긴 line만 heap buffer를 사용해 batch 좌표
  buffer의 memory trade-off를 완화했다.
- 표준 2D 단일 ring WKT `POLYGON`도 같은 batch 경로를 사용한다. hole,
  multipart, Z/M geometry는 nested structure를 감지해 기존 delimiter 보존
  경로로 fallback한다. Apple M3 10K 5점 polygon benchmark가 generic 경로의
  약 15.00ms/9.22MB/137,412 alloc에서 약 8.70ms/6.57MB/25,361 alloc으로
  줄었다. 출력은 canonical 단일 ring WKT이며 입력 geometry는 변형하지 않는다.
- desktop multi-layer CRS 정렬에서는 동일한 source/target CRS layer를 묶어
  하나의 PROJ pipeline을 공유한다. 4개 layer/총 10K WKB point benchmark가
  layer별 pipeline 생성 약 1.08ms/20,024 alloc에서 shared pipeline 약
  0.95ms/20,016 alloc으로 줄었고, 서로 다른 source CRS는 별도 그룹으로
  처리해 좌표계 semantics를 유지한다.
- PROJ 변환용 layer snapshot은 geometry를 먼저 `Clone()`하지 않고 properties와
  label만 detached copy한다. 변환 결과가 모든 geometry를 새 값으로 교체하므로
  WKB 입력의 선행 binary copy와 `MapWKBXY`의 결과 copy가 중복되지 않도록 했다.
- GEOS operator의 결과 layer 준비도 같은 원칙을 적용한다. operation마다 기존
  geometry를 즉시 새 WKT 결과로 교체하므로 입력 geometry 선행 clone을 제거하고,
  properties/label만 detached copy한다. 10K point clone benchmark는 약
  0.880ms/4.56MB/40,003 alloc에서 약 0.713ms/4.41MB/20,004 alloc으로 줄었다.
- GEOS 입력이 `core.WKBGeometry`이면 WKT 왕복 없이 `go-geos`의 WKB reader를
  직접 사용한다. 단일 point read benchmark는 약 580ns에서 271ns로 줄었고,
  WKT 입력과 결과 WKT serialization 계약은 유지한다.
- GEOS operation 결과도 WKT 문자열 대신 `core.WKBGeometry`로 저장한다.
  downstream에서 WKT가 필요할 때만 `core.ToWKT` 경계에서 변환하며, GEOS
  serializer benchmark는 polygon 기준 WKT 약 500ns/64B에서 WKB 약
  376ns/128B로 줄었다. WKB의 추가 64B는 binary ownership 비용으로 명시하고,
  render/GDAL/PROJ의 direct WKB 경로와 일관성을 우선한다.
- GDAL writer도 WKB feature를 WKT로 왕복하지 않고 `NewGeometryFromWKB`로
  직접 생성하며, geometry type 검증은 WKB header만 읽는다. 단일 point
  `newOGRGeometry` benchmark는 WKT 약 352ns/40B에서 WKB 약 198ns/48B로
  감소했다. WKT 입력은 기존 경로를 유지한다.
- DXF exporter의 WKT numeric parser도 정규식 결과 문자열 slice 대신 byte scan과
  `ParseFloat`를 사용한다. 100K coordinate 기준 regex baseline 약
  54.9ms/13.0MB/약 119 alloc에서 약 2.9ms/4.10MB/25 alloc으로 감소했으며,
  scientific notation과 기존 geometry export 테스트를 유지한다.
- 일반 WKT `POINT`/`LINESTRING`은 parser 내부에서 불필요한 단일 원소
  `[][]Point` wrapper를 만들지 않고 flat representation으로 유지한다. 레이어
  단위 point arena에 좌표를 모아 feature별 좌표 slice allocation을 제거하고,
  LINESTRING의 기존 `Parts` compatibility view는 source 생성 시 공유 backing
  array에서 제공한다. POINT는 flat `Vertices`만으로 hit-test semantics를
  충족하므로 `Parts` wrapper를 생략한다. 100K WKT POINT source benchmark가
  약 9.79ms/42.20MB/502 alloc에서 약 9.24ms/39.00MB/501 alloc으로 줄었다.
- WKT point arena는 WKT geometry를 실제로 만났을 때만 지연 할당한다. WKB-only
  layer가 WKT 전용 좌표 backing storage를 선점하지 않도록 해 binary import의
  peak memory를 줄인다.
- WKB-only layer에서는 WKT `Parts` compatibility arena도 지연 할당한다. 100K
  WKB LineString source benchmark가 약 29.98MB/361 alloc에서 약
  27.58MB/360 alloc으로 줄었고, WKT compatibility view의 allocation semantics는
  그대로 유지한다.
- parsed feature metadata도 단순 geometry에서 `parts == nil`을 single-part
  sentinel로 사용하도록 compact화했다. 100K WKB LineString source benchmark가
  추가로 약 27.58MB에서 25.98MB로 줄었고, WKT 100K LineString도 약 29.98MB에서
  28.38MB로 줄었다. 빈 geometry는 non-nil empty parts로 구분해 기존 semantics를
  보존한다.
- 단순 WKB Point/LineString도 동일한 layer-owned point arena에 직접 decode한다.
  binary geometry의 feature별 `[]Point` allocation을 제거하고, 복합 WKB는 기존
  component slice 경로를 유지한다.
- 표준 2D WKB POINT/LINESTRING은 endian과 고정 좌표 layout을 직접 읽는 parser
  fast path를 추가했다. 100K WKB LineString source benchmark가 약 5.4ms에서
  5.0ms로 줄었고, Z/M/EWKB·복합 geometry 및 malformed 입력은 generic parser로
  fallback해 validation semantics를 유지한다.
- 표준 2D WKB POLYGON도 ring별 좌표를 layer-owned arena에 직접 decode하고
  단일 ring은 flat representation으로, hole/multipart만 ring slice header를
  유지하도록 확장했다. 10K polygon source benchmark가 generic baseline 약
  5.05ms/27.15MB/20,343 alloc에서 약 4.89ms/26.91MB/343 alloc으로 줄었다.
  첫 polygon의 ring point 수를 homogeneous layer capacity hint로 사용하며,
  확장·malformed·trailing-byte 입력은 generic parser로 fallback한다.
- WKT POLYGON도 단일 ring에 한해 같은 flat arena 경로를 사용하도록 했다.
  10K polygon source benchmark가 generic baseline 약 6.43ms/28.75MB/50,343
  alloc에서 약 5.54ms/28.85MB/347 alloc으로 줄었고, hole/multipart는 기존
  `wktGroups` 기반 parts semantics를 유지한다.
- point arena의 초기 capacity는 첫 geometry type을 기준으로 Point layer에는
  feature당 1점, LineString 및 일반 입력에는 feature당 최대 2점을 예약한다.
  혼합 geometry는 append로 안전하게 확장해 correctness를 유지한다.
- WKT source normalization은 양 끝점이 같은 chunk cell에 있으면 Liang–Barsky
  clipping을 건너뛰고 직접 vertex를 기록한다. cell 경계를 넘는 선분에는 기존
  clipping 경로를 적용해 visible geometry semantics를 유지한다.
- WKT POINT marker도 반경 전체가 같은 chunk cell 안에 있는 일반 case에서는
  두 cross segment를 직접 append하고, cell 경계에 걸리는 marker만 기존 clipping
  경로로 보낸다. 100K point source benchmark가 약 9.24ms에서 약 8.23ms로
  줄었고, memory/allocations는 동일하다.
- WKT 숫자 scanner는 정상적인 geometry type prefix에 숫자가 없을 때 첫 `(`
  이후만 검사하여 type 문자열을 매번 순회하지 않는다. prefix에 숫자가 포함된
  malformed 입력은 전체 문자열을 기존 방식으로 검사하므로 오류 검출을
  유지하고, `LINESTRING 1 (2 3)` 회귀 테스트로 이를 고정했다. Apple M3의
  최신 100K WKT benchmark는 `LayerSource100KPoints` 약 8.46ms,
  `LayerSource100KLines` 약 11.32ms로 측정됐다.
- render extent와 hit-index bounds 계산은 좌표마다 `math.Min/Max` 호출 대신
  직접 비교를 사용한다. NaN/비정상 geometry는 기존 parser 단계의 오류·empty
  처리 경계를 벗어나지 않도록 별도 geometry semantics는 변경하지 않는다.

## 측정 결과

Apple M3, `go test -run '^$' -bench BenchmarkLayerSourceScale -benchtime=200ms
-benchmem ./internal/render` 기준이다. 합성 LineString 입력에서 render
source 생성 비용은 다음과 같이 측정됐다.

| feature 수 | 기존 | 최적화 후 | 할당량 변화 |
| ---: | ---: | ---: | ---: |
| 10K | 약 3.56 ms | 약 2.06 ms | 3.79 MB → 2.59 MB |
| 100K | 약 34.1 ms | 약 20.8 ms | 37.97 MB → 25.97 MB |
| 1M | 약 360 ms | 약 232 ms | 414.95 MB → 294.95 MB |

100K 단일 part line 기준 allocation count는 500,359에서 200,359로 감소했다.

최신 WKT 100K line benchmark에서 coordinate parsing은 약 16.1ms에서 15.4ms로,
전체 source 생성은 약 20.3ms에서 19.3ms로 개선됐다. `Parts` compatibility
wrapper 때문에 전체 allocation count는 200,359로 유지되며, 2점 line 기준
메모리는 약 29.18MB 수준을 유지한다.

decimal fast path 적용 후 최신 WKT 100K line benchmark는 coordinate parsing
약 12.1ms, 전체 source 생성 약 16.2ms로 추가 개선됐다. 이후 일반 decimal
token의 선행 exponent scan도 제거해 최신 parsing은 약 11.0ms, source 생성은
약 15.3ms로 측정됐다. 메모리와 allocation
count는 각각 약 29.18MB, 200,359로 유지됐고, 지수 표기 회귀 테스트와 malformed
token 오류 테스트를 추가했다.

100K line normalization benchmark에서는 clipping boundary 배열 제거 후 약
4.19ms에서 4.09ms로 측정됐다. 이 경로는 geometry semantics와 36 allocations를
그대로 유지하면서 clipping 계산의 작은 CPU 비용을 줄인다.

동일한 100K line benchmark에서 직접 WKB decode 경로는 WKT 경로의 약 20.8ms
대비 약 9.5ms로 측정됐다. 이후 단순 WKB의 외부 part wrapper 할당을 제거한
현재 경로는 약 12.6ms, 26.8MB, 100,359 allocations로 측정됐다. WKB 경로는
WKT 문자열 생성·문자열 숫자 파싱 비용을 제거하며, 단순 feature의 allocation도
줄인다.


이 수치는 GDAL I/O를 포함하지 않으며, 실행 환경·Go 버전·CPU 부하에 따라
변할 수 있다. WKT 파서의 scientific notation 회귀 테스트도 추가했다.

Apple M3의 10K Point CRS 변환 benchmark에서는 직접 WKB 경로가 WKT 경로의
약 9.91ms 대비 약 2.46ms로 측정됐다. 메모리는 3.48MB에서 1.36MB로,
allocation count는 120,012에서 40,004로 감소했다.

변환 전용 shallow geometry snapshot을 적용한 최신 10K Point benchmark에서는
WKB 경로가 약 2.28ms, 0.88MB, 20,004 allocations로 추가 개선됐다. WKT 경로도
geometry clone 중복이 제거되어 약 9.90ms, 3.32MB, 110,012 allocations로
측정됐다. properties와 label은 계속 detached copy하므로 입력 feature의
mutable metadata ownership은 유지한다.

WKB POINT 변환의 feature별 binary clone은 하나의 정확한 layer-owned byte
arena로 합쳤다. 10K WKB POINT benchmark가 약 0.93ms, 1.29MB, 20,006
allocations에서 약 0.84ms, 1.26MB, 10,007 allocations로 줄었고, 각
feature slice의 범위와 입력 WKB 비변경 semantics는 유지한다.

PROJ production package에서 test assertion만 사용하던 WKT number regexp를
test 파일로 이동해 native package initialization의 불필요한 regexp compile과
약 5K object allocation을 제거했다.

표준 2D WKB LINESTRING도 flat coordinate batch 변환을 추가했다. 10K line
기준 generic `MapWKBXY` 경로의 약 3.62ms/1.12MB/20,004 allocations에서
약 1.45ms/2.04MB/10,010 allocations로 줄었다. 하나의 WKB arena를 사용해
입력은 변경하지 않으며, 좌표 metadata는 고정 layout으로 보관한다.

표준 2D WKB POLYGON의 단일 ring도 동일한 batch 경로를 추가했다. 10K
polygon 기준 generic 경로의 약 8.62ms/1.60MB/20,004 allocations에서
약 2.90ms/3.34MB/10,011 allocations로 줄었다. hole/multipart와 Z/M
geometry는 layout 검증에서 제외되어 기존 generic mapper로 fallback하며,
NaN XY는 변환하지 않고 보존한다.

표준 2D WKB MULTILINESTRING도 nested line header를 검증한 뒤 하나의
coordinate batch로 변환하도록 확장했다. 10K multi line 기준 generic 경로의
약 7.39ms/4.12MB/20,015 allocations에서 약 2.51ms/3.15MB/10,011
allocations로 줄었고, component 구조와 NaN 보존 semantics를 유지한다.

표준 2D WKB MULTIPOLYGON도 각 단일-ring polygon header를 검증하는 batch
경로를 추가했다. 10K multi polygon 기준 generic 경로의 약 17.18ms/5.49MB/
20,016 allocations에서 약 5.38ms/5.46MB/10,012 allocations로 줄었고,
hole/multipart ring과 확장 geometry는 generic fallback을 유지한다.

표준 2D WKB MULTIPOINT도 nested POINT header를 검증하는 batch 경로를
추가했다. 10K multi point 기준 generic 경로의 약 4.08ms/4.30MB/20,017
allocations에서 약 1.49ms/2.41MB/10,011 allocations로 줄었고, 입력 비변경과
NaN 보존 semantics를 유지한다.

DXF WKB Point/Line/Polygon 출력도 exporter의 numeric writer를 사용하도록
연결했다. 10K WKB LINE benchmark는 기존 FormatFloat 경로의 약
1.79ms/1.04MB/60,000 allocations에서 약 0.48ms/560KB/20,000
allocations로 줄었다. WKT와 마찬가지로 숫자는 ASCII stack buffer로 직접
기록하고, 기존 test callback wrapper는 유지한다.

Apple M3의 10K Point GeoJSON reader benchmark에서는 geometry-only 경로가
전체 속성 경로의 약 64.8ms 대비 약 53.4ms로 측정됐다. 메모리는 10.1MB에서
1.32MB로, allocation count는 179,502에서 70,013으로 감소했다.

같은 fixture에서 200행 attribute page는 페이지 위치를 0~49로 순환하는
benchmark 기준 약 29.5ms, 0.20MB, 2,384 allocations였다. OGRSQL
`LIMIT/OFFSET` result set을 우선 사용해 Go에서 앞선 feature를 하나씩
소비하는 비용을 줄였고, 현재 구현은
순차 ID 안정성을 위해 앞선 feature를 스캔하므로 뒤쪽 페이지의 시간이 늘 수
있다. 이 비용을 줄이려면 GDAL FID/driver-native random access를 별도 검증해야
하며, 임의의 FID를 application ID로 가정하지 않는다.

GeoJSON에서는 OGRSQL `fid` 컬럼을 조회할 수 있음을 테스트로 확인했다.
그러나 현재 core의 feature ID는 순차 application ID이고, GDAL driver별 FID
시작값·안정성·재생성 규칙이 다를 수 있으므로 둘을 동일시하지 않는다. 향후
FID 기반 random access를 도입하려면 source별 capability와 명시적 FID 매핑을
먼저 도입해야 한다.

read-only retained GDAL attribute session을 같은 10K GeoJSON fixture에 적용한
200행 page benchmark는 약 9~10ms, 0.20MB, 2,409 allocations로 측정됐다.
dataset open/driver 초기화 반복을 제거했으며, page 결과의 property materialize
semantics와 sequential application ID는 유지한다. session은 writable runtime에는
사용하지 않아 저장 직후 stale dataset을 재사용하지 않는다.

같은 session dataset에서 OGRSQL `LIMIT/OFFSET` page와 직접
`NextFeature` offset scan도 비교했다. SQL 경로는 약 8~10ms/약 2,411
allocations, 직접 scan은 약 10.2~10.5ms/6,495 allocations였다. 따라서 SQL
pushdown 경로를 유지하고, driver가 SQL을 거부하는 경우에만 기존 순차 fallback을
사용한다.

같은 fixture에서 feature 선택 benchmark는 standalone `OpenFeature`의 약
27~28ms에서 retained session의 약 0.6~1.2ms로 감소했다. 두 경로 모두 sequential
application ID를 위해 layer를 순회하지만, session 경로는 매 선택마다 반복되던
dataset open/driver 초기화를 제거한다. godal v0.0.18의 `Layer`에는 읽기용 FID
조회 API가 없고 `Feature`에는 `SetFID`만 노출되므로, driver-independent random
access를 위해 FID를 application ID로 추정하지 않는다.

100K line source의 chunk build benchmark는 기존 약 6.6µs/1 allocation에서
약 11ns/0 allocation으로 측정됐다. source construction 이후 vertex slice가
변경되지 않는다는 불변성에 기반한 최적화다.

최신 Apple M3 재측정에서 100K WKT LineString의 `parseLayerPoints`는 약
7.4ms/9.6MB/2 allocations, 전체 `NewLayerSource`는 약
10.7~11.0ms/30.0MB/약 361 allocations였다. 이전 feature별 좌표 slice와
`Parts` wrapper allocation을 합친 경로 대비 parser allocation은 약 20만 회에서
2회로, source 생성 allocation은 약 20만 회에서 361회로 줄었다.

100K WKT Point source는 geometry-aware arena capacity 적용 후 약
9.1ms/43.0MB/502 allocations로 측정됐다. 기존 두 점 고정 capacity의 약
44.6MB 대비 point coordinate arena의 과예약을 줄였으며, point marker의
정규화·chunk semantics는 유지한다.

동일 입력의 normalization 단계는 같은 cell fast path와 직접 extent 비교 적용 후
약 3.2ms/22.7MB/약 35 allocations로 측정됐다. 100K WKB LineString source는
약 5.7ms/30.0MB/361 allocations로 측정됐다. WKT 전용 arena 지연 할당과 WKB
point arena를 적용해 binary 경로의 feature별 좌표 allocation을 제거했다.

10K chunk batch benchmark에서는 `BatchStore.Current()`가 정렬 기반 기준 구현의
약 1.58ms, 884KB, 5 allocations에서 약 0.35ms, 483KB, 1 allocation으로
개선됐다. 이 수치는 Apple M3에서 동일한 10K chunk와 4 vertex payload를
반복 flatten한 결과이며, 반환 slice 복사는 유지한다.

동일 batch의 destination 재사용 benchmark에서는 `CurrentInto()`가 약 0.19ms,
159B, 0 allocations로 측정됐다. 최신 `Current()`는 약 0.20ms, 483KB,
1 allocation이었다. `BatchStore`가 chunk vertex 총량을 유지해 capacity
계산용 두 번째 순회를 제거한 결과다.

ordered vertex slice/index를 추가한 최신 10K chunk benchmark에서는
`Current()`가 약 0.039ms, 483KB, 1 allocation, `CurrentInto()`가 약 0.023ms,
0 allocations로 측정됐다. 기존 key-to-map lookup 경로의 약 0.20ms/0.19ms
대비 flatten CPU 시간이 줄었다.

Qt bridge의 100K vertex publish benchmark는 Go scratch 변환 없이 C++가 XY를
복사하는 현재 경로에서 약 0.296ms, 0B, 0 allocations로 측정됐다. 이후 C++가
Go와 동일한 12-byte vertex layout을 보관하고 `memcpy`하도록 바꿔 XY 추출
loop를 제거했으며, 같은 benchmark가 약 0.018ms, 0B, 0 allocations로 줄었다.
C++ vector capacity는 반복 publish에서 재사용되고 scene graph 단계에서만 XY를
viewport 좌표로 변환한다.

4,096 vertex chunk apply benchmark에서는 기존 defensive copy 경로가 약
2.23µs, 49,152B, 1 allocation이었고, immutable render publish 경로는 약
41ns, 0B, 0 allocations였다. 이 경로는 chunk vertex slice를 이후 수정하지
않는다는 scheduler/source 계약을 전제로 하며, 계약 밖의 호출자는 `Apply()`를
사용해야 한다.

2,704 chunk viewport generation 갱신 benchmark에서는 visibility map과 ordered
key backing storage를 재사용하는 경로가 약 94.8µs, 0B, 0 allocations였다.

같은 ordered visible key 집합을 generation만 갱신하는 refresh에서는 key 순서를
확인한 뒤 visibility map 재구축을 건너뛰도록 했다. 동일 2,704 key benchmark가
약 159µs에서 약 5.8µs로 줄었고, key 집합이 바뀌는 pan/zoom 경로는 기존 filtering을
그대로 사용한다.

Apple M3의 0.01 chunk size viewport planner benchmark에서 독립 slice 경로는
약 15.5~16.0µs/114.7KB/1 allocation이었고, `VisibleKeysInto` 재사용 경로는
약 5.6µs/0B/0 allocations였다. 이 경로는 Qt refresh에서 레이어별 key 조립
중간 allocation도 줄인다.

10만 line의 hit index 생성은 Apple M3에서 linked-flat cell index 기준 약
2.9ms, 2.48MB, 83 allocations였다. 기존 map-of-slices 기준 약 2.9ms,
4.1MB, 50,682 allocations와 비슷한 build CPU를 유지하면서, hit index는
startup이 아닌 첫 선택 시점에 lazy-build되고 이후 indexed query는 약
1.2µs/0 allocation으로 유지된다.

hit index의 cell map은 100K line 기준 약 10,201개 cell을 만들므로 feature 수의
1/8을 초기 capacity hint로 예약했다. 그 결과 linked-flat index build가 약
2.87ms/2.48MB/83 alloc에서 약 2.52ms/2.04MB/36 alloc으로 줄었다.

1만 feature project benchmark에서는 전체 `Project()` snapshot이 약 0.78ms,
3.92MB, 30,003 allocations였고, 동일 서비스의 `LayerNames()`는 약 18ns,
40B, 2 allocations였다. layer tree와 초기 active layer 선택은 후자의
metadata 경로로 전환했다.

10K feature `BeginEdit()` benchmark는 기존 deep clone 기준 약 0.76ms,
3.92MB, 30,003 allocations에서 header-only copy-on-write 시작 기준 약
57ns, 208B, 3 allocations로 개선됐다. 실제 property/feature mutation 시에만
대상 데이터가 분리되며 rollback isolation 테스트를 유지한다.

10K feature save benchmark는 전체 project clone 후 target clone baseline의 약
1.56ms, 7.84MB, 60,003 allocations에서 target layer 단일 clone 경로의 약
0.744ms, 3.92MB, 30,001 allocations로 개선됐다.

10K feature label benchmark는 public detached 경로의 약 2.52ms, 5.03MB,
80,004 allocations에서 이미 detached layer를 재사용하는 내부 경로의 약
1.73ms, 1.11MB, 50,001 allocations로 개선됐다.

10K feature에서 한 건만 일치하는 filter benchmark는 source 전체 clone baseline의
약 0.812ms, 3.92MB, 30,005 allocations에서 결과 feature만 materialize하는
경로의 약 0.095ms, 496B, 5 allocations로 개선됐다.

4개 layer, 총 10K feature의 spatial input benchmark는 전체 project clone 기준
약 0.767ms, 3.95MB, 30,006 allocations에서 대상 layer snapshot 기준 약
0.199ms, 0.99MB, 7,502 allocations로 개선됐다.

10K Point GeoJSON에서 전체 geometry-only reader는 약 50.6ms, 1.32MB,
70,013 allocations였고, 동일한 0.1도 spatial window reader는 약 73.6ms,
0.13MB, 7,031 allocations였다. GeoJSON처럼 spatial index가 없는 드라이버는
시간이 늘 수 있지만 result set materialization과 보관 메모리는 약 90% 줄었다.
따라서 runtime window 연결 시 드라이버 capability/파일 크기/메모리 압박을
조건으로 삼고, 무조건 window query로 대체하지 않는다.

2,704 chunk cached request benchmark에서는 per-chunk goroutine 기준 약
1.46ms, 2.71MB, 7,327 allocations에서 fixed worker queue가 약 1.38ms,
2.53MB, 38 allocations로 개선됐다. 시간보다 goroutine/GC 부담 감소가 주된
효과다.

동일 benchmark에서 defensive dedup 경로는 약 1.37ms, 2.53MB, 34 allocations,
planner가 key uniqueness를 보장하는 `RequestUnique` 경로는 약 1.28ms,
2.18MB, 24 allocations였다.

result channel을 32개 bounded buffer로 제한한 최신 `RequestUnique` benchmark는
약 1.32ms, 1.78MB, 24 allocations였다. 약 3%의 backpressure 비용으로 request
당 약 0.4MB peak allocation을 줄인다. buffer 1은 추가 시간 손실만 있어 적용하지
않았다.

모든 chunk가 cache hit인 fast path는 2,704 chunk 기준 약 0.42ms, 1.98MB,
19 allocations로 측정됐다. 기존 fixed worker queue의 약 1.50ms, 1.78MB,
24 allocations보다 약 3.5배 빠르다.

runtime처럼 결과 channel을 즉시 drain하는 cache-hit benchmark에서는 snapshot
backing pool 적용 후 약 0.236ms/210KB/4 allocations에서 약
0.222ms/5.3KB/4 allocations로 줄었다. cache snapshot ordering과 generation
경쟁 semantics는 유지한다.

cache hit/miss가 절반씩 섞인 viewport는 cached/missing 분리 후 약 1.11ms,
2.29MB, 1,443 allocations로 측정됐다. 이후 missing key 분배를 atomic index
기반 4-worker 실행기로 바꾸자 최신 Apple M3 측정은 약 0.89ms/2.24MB/1,447
allocations가 됐다. 결과 채널 snapshot metadata 증가는 유지하되, worker queue
송수신 비용은 제거했다.

100K line lazy hit-index 생성은 feature 배열 재복사와 단일 geometry wrapper
생성을 제거한 현재 경로에서 약 4.28ms, 4.07MB, 50,682 allocations로
측정됐다. 기존 기준 약 5.8ms, 11.3MB 대비 메모리 피크와 생성 시간이 줄었다.

100K line indexed hit-test는 candidate stack buffer 적용 전 약 1.25µs,
2,480B, 5 allocations에서 256-entry stack 적용 후 약 1.10µs, 0B,
0 allocations로 개선됐다. visible variant도 약 1.97µs, 0B, 0 allocations로
측정됐다.

10K `MULTILINESTRING` WKT source import은 약 4.88ms, 16.92MB,
60,230 allocations로 측정됐다. 그룹 문자열을 먼저 만들고 각 그룹을
재파싱하던 경로를 직접 arena에 파싱하는 경로로 바꿔 약 4.84ms,
16.92MB, 60,230 allocations가 됐다. 이 변경은 임시 substring 수명을
줄이지만 현재 workload에서는 allocation 수가 지배적이므로, 추가적인
multipart 전용 arena 구조는 실제 파일 benchmark가 확보될 때까지 보류한다.

100K POINT source allocation profile에서는 `newLayerSource`의 chunk vertex
slice 성장만 약 13.3MB를 차지했다. 단순 point 레이어에 대해 셀별 vertex
capacity를 선계산하고 좌표 정규화를 한 번만 수행하도록 바꾼 결과는 약
24.2MB/203 allocations로, 기존 약 39.1MB/502 allocations에서 줄었다.
최근 Apple M3 측정 시간은 약 8ms이며 point 표시와 hit-test용 좌표 의미는
변경하지 않는다.

100K 표준 LINESTRING source도 동일한 profile에서 chunk vertex slice 성장
비용이 약 12.2MB로 확인됐다. 단일 flat line geometry만 segment별 셀 vertex
수를 선계산해 선할당한 결과 WKT line은 약 28.5MB/362 allocations에서
20.2MB/43 allocations로 줄었고, 약 11ms 수준을 유지했다. WKB
LINESTRING도 약 17.8MB/42 allocations로 측정됐다. POLYGON과 multipart는
이 선계산 경로에서 제외해 clipping 사전 계산으로 인한 시간 회귀를 피한다.

후속 측정에서 단순한 2-part `MULTILINESTRING`은 각 part가 2개 이상의 정점을
가진 선분인 경우 동일한 셀별 hint와 layer-owned 정점 arena를 사용하도록
확장했다. 이어 homogeneous WKT multipart layer를 전용 parser로 직접 처리해
일반 interface/fallback 경계를 제거했다. Apple M3의 10K feature benchmark는
전체 source 기준 약 7.14ms/6.33MB/40,025 allocations에서 약
6.59ms/5.70MB/26 allocations로, parsing 단계는 약
1.57ms/2.26MB/40,003 allocations에서 약 1.10ms/1.62MB/4 allocations로
감소했다. 복잡한 multipart, 혼합 geometry, hole, 곡선 및 비선형 geometry는
기존 일반 경로를 유지한다.

같은 전략을 표준 2D WKT `MULTIPOINT`와 단순 ring만 포함한
`MULTIPOLYGON`에도 적용했다. 10K feature 기준 `MULTIPOINT`는 약
4.14ms/10.37MB/50,153 allocations에서 약 3.69ms/9.42MB/155 allocations로,
`MULTIPOLYGON`은 약 10.7ms/23.79MB/100,307 allocations에서 약
9.05ms/21.16MB/309 allocations로 감소했다. 괄호 그룹은 layer-owned point와
part arena에 직접 기록하며, 복잡한 혼합 geometry와 malformed/EMPTY 입력은
기존 일반 parser 경로를 사용한다.

단순 `GEOMETRYCOLLECTION`에서 `POINT`/`LINESTRING` component만 포함하는
경우에도 top-level component를 직접 스캔하도록 확장했다. Apple M3의 10K
feature benchmark는 약 6.6ms/15.45MB/90,252 allocations에서 약
6.0ms/13.29MB/253 allocations로 감소했다. polygon, multipart, nested
collection component와 EMPTY/malformed 입력은 기존 재귀 경로를 유지한다.

이후 같은 direct component parser를 단순 `POLYGON` component까지 확장했다.
10K `GEOMETRYCOLLECTION (POLYGON, POINT)` benchmark는 약
9.7ms/21.03MB/110,281 allocations에서 약 8.7ms/17.43MB/282 allocations로
감소했다. `MULTIPOINT`, `MULTILINESTRING`, `MULTIPOLYGON` component도 nested
ring/part parser를 공유하며, nested collection과 EMPTY/malformed component는
fallback한다.

표준 2D WKB `GEOMETRYCOLLECTION`에서 Point/LineString child만 포함하는
경우도 동일하게 직접 decode하도록 추가했다. 10K feature source benchmark는
약 5.3ms/14.33MB/60,252 allocations에서 약 4.4ms/13.28MB/253 allocations로
감소했다. child 구조가 extended, nested 또는 malformed이면 기존 WKB cursor
fallback을 사용한다. multipart part arena를 feature 수에 맞춰 선할당해
후속 확장 비용도 제거했다.

이 direct parser는 표준 2D `POLYGON` child의 외부 ring과 hole ring까지
확장했다. 단일 ring WKB `GEOMETRYCOLLECTION (POLYGON, POINT)` 10K feature
benchmark는 약 6.4ms/20.35MB/303 allocations에서 point-arena와 part-arena
capacity hint를 추가한 뒤 약 6.1ms/17.26MB/281 allocations로 줄었다. hole
ring을 포함한 collection은 약 9.2ms/26.09MB/314 allocations로 측정된다. extended·nested·
malformed child는 기존 cursor fallback을 계속 사용한다.

같은 direct arena 전략을 표준 2D WKB `MULTILINESTRING`에도 적용했다. 10K
feature benchmark는 약 5.9ms/5.70MB/26 allocations이며, 각 child line의
part slice는 공유 point arena를 참조한다. child type이 다르거나 WKB가
extended·malformed이면 기존 generic parser로 되돌아간다.

표준 2D WKB `MULTIPOINT`도 child Point의 endian과 좌표를 직접 decode하도록
확장했다. 10K feature benchmark는 약 3.4ms/9.42MB/155 allocations로
측정되며, Point가 아닌 child나 extended·malformed 입력은 generic fallback을
사용한다.

표준 2D WKB `MULTIPOLYGON`도 child polygon의 외부 ring과 hole ring을 직접
decode하도록 확장했다. 10K feature benchmark는 약 7.6ms/21.05MB/308
allocations이며, child geometry가 polygon이 아니거나 extended·nested·malformed
입력이면 generic fallback을 사용한다.

10만 line의 hit-index 구축은 약 2.5ms/2.04MB/36 allocations, indexed hit-test는
약 1.2µs/0B/0 allocations로 측정됐다. visibility predicate를 포함해도 약
2.1–2.4µs/0B/0 allocations이며, 데스크톱 runtime은 hit-index를 click마다
재생성하지 않고 feature snapshot 교체 시 한 번만 만든다. 따라서 현재
상호작용 경로의 병목으로 분류하지 않는다.

큰 tolerance가 index cell 대부분을 덮는 dense query는 후보 수집·정렬만으로 약
1.6ms/4.10MB/19 allocations가 발생했다. 이 경우에는 후보 목록을 만들지 않고
전체 feature를 직접 검사하도록 분기해 약 1.0ms/0B/0 allocations로 줄였으며,
일반적인 작은 tolerance의 약 1.2µs/0B/0 allocations 경로는 유지했다.

hit-index는 feature bounding box 전체가 아니라 각 part의 실제 segment가
통과하는 cell만 등록하도록 바꿨다. 긴 segment는 grid traversal, 짧은
segment는 기존 clipping 경로를 사용한다. 100개 긴 대각선 feature benchmark에서
구축 비용이 약 19.3ms/46.5MB/140 allocations에서 0.52ms/1.19MB/44
allocations로 감소했고, 일반 100K 짧은 line benchmark는 약 2.5ms/2.04MB/36
allocations로 유지됐다.

축 정렬 선분은 clipping 없이 교차하는 cell을 직접 열거하고, 32개 cell을
넘는 긴 선분은 cell span으로 압축하는 경로를 추가했다. 100개 긴 수평선
benchmark는 약 68.4ms/157.2MB/8,257 allocations에서 약
1.6µs/12.9KB/14 allocations로 감소했다. hit-test에서는 span과 query cell
범위가 교차할 때만 feature 후보로 복원하며, 짧은 선분·대각선의 기존 cell
정확성 경로는 유지한다.

10K WKB POINT layer clone도 feature별 WKB byte 복사를 layer-owned arena로
묶었다. per-feature baseline 약 0.95ms/4.24MB/40,001 allocations에서 약
0.85ms/4.21MB/30,002 allocations로 줄었고, feature별 WKB slice는 서로
분리된 범위를 유지해 source와 다른 feature의 mutation isolation을 보장한다.
`Feature.Clone()`의 독립 복제 계약은 그대로 유지한다.

네이티브 GDAL retained session을 다시 측정한 결과, 10K GeoJSON POINT의
geometry-only 전체 로드는 dataset을 매번 여는 경로의 약 51ms/1.32MB/70,013
allocations에서 약 19ms/1.32MB/70,010 allocations로 줄었다. 동일한 0.1도
window 요청은 dataset 재오픈 경로 약 62ms/0.88MB/45,026 allocations에서
retained geometry session 약 20ms/0.88MB/45,021 allocations로 줄었다.
따라서 현재 native import의 주요 잔여 비용은 Go-side slice가 아니라
feature별 GDAL wrapper와 WKB 추출이며, session 재사용은 이미 UI의 read-only
초기 로딩과 attribute 조회에 연결되어 있다. 다음 최적화는 이 경계를 바꾸기
전에 Windows native 빌드에서 동일 수치를 재현하는 것이다.

속성 페이지도 같은 session 효과를 보였다. 200행 GeoJSON page는 dataset
재오픈 경로 약 27ms/184KB/2,378 allocations에서 retained session 약
0.43ms/184KB/2,392 allocations로 줄었고, 50페이지 연속 요청은 약
21–25ms/9.21MB/119,638 allocations였다. 단일 feature 조회는 약
2.7µs/966B/17 allocations로 측정됐다. 따라서 현재 UI의 순차 페이지와
payload cache 경로는 충분히 작으며, 임의 page jump의 cursor 재스캔 비용은
Windows의 SHP/GeoPackage 인덱스 benchmark로 별도 확인한다.

세부 분해 benchmark에서도 같은 결론을 확인했다. 10K GeoJSON POINT에서
`godal.Layer.NextFeature()`만 약 15.6ms/10,000 allocations, 여기에
`Feature.Geometry()`를 추가하면 약 18.6ms/20,000 allocations, 다시
`Geometry.WKB()`까지 추가하면 약 20.1ms/60,000 allocations였다. 현재
`github.com/airbusgeo/godal` 버전에는 이 경로를 대체할 Arrow/batch feature
API가 없으므로, core 모델에 native handle을 누수시키는 우회보다 retained
dataset/session과 viewport window를 유지하는 쪽을 선택한다.

POLYGON/multipart처럼 정확한 셀별 hint를 만들지 않는 일반 geometry는
chunk vertex slice 성장률을 기본 append의 2배에서 1.5배로 낮췄다. 10K
WKT POLYGON 기준 약 28.9MB/348 allocations에서 약 22.1MB/404
allocations, 약 7.6ms로 측정됐다. 이후 첫 표준 WKT ring의 좌표 수를
point arena capacity hint로 사용해 약 20.2MB/400 allocations, 약 7.6ms로
추가 감소했다. allocation 수는 소폭 늘지만 메모리
피크와 시간이 함께 개선되어 적용한다.

실제 수치는 Go 버전과 cell 분포에 따라 달라지며, 아래 benchmark로 회귀를
감시한다.

동일 조건의 visible hit-test는 미리 만든 map 경로 약 2.01µs였고, 단순
predicate는 약 1.25µs였지만 실제 mutex 기반 `IsVisible` predicate는 약
2.28µs였다. 따라서 Qt는 predicate를 매 candidate에 호출하지 않고 변경 시
생성한 map snapshot을 재사용한다. hit-test 내부의 1,536B/2 allocations는
유지되며, 클릭당 map 생성과 visibility mutex 호출은 제거된다.

`ProjectService.HasFeature`와 `FeatureProperty`의 첫 조회는 요청한 레이어가
아니라 프로젝트의 모든 레이어를 feature ID map으로 만드는 구조였다. 4개
레이어·총 10K feature에서 첫 `layer-0` 조회를 측정하니 약
25.5µs/74KB/12 allocations가 들었고, 요청한 레이어만 지연 생성하도록
바꿨다. 이후 반복 조회는 약 9ns/0B/0 allocations이며, 다른 레이어의
인덱스는 실제로 요청될 때까지 생성하지 않는다. 편집 commit/rollback 시
전체 인덱스 무효화 계약은 유지한다.

같은 ID 인덱스를 edit draft의 기존 feature 수정에도 재사용했다. 10K feature
레이어에서 하나의 draft 안에서 마지막 feature를 반복 수정하는 경로는
`SetFeatureProperty`가 매번 선형 탐색하고 property map을 복제하던 구조에서
약 33.4ns/8B/0 allocations로 측정된다. draft 중 새로 추가된 feature나 인덱스에 없는 항목은
기존 선형 fallback을 유지해 편집 동작의 정확성을 보존한다.

`MergeProjectLayers`의 10K feature 경로에서는 결과 feature slice와 중복 ID
검사용 map을 입력 feature 총량으로 선할당했다. 병합 결과의 ownership clone
계약은 유지하면서 detached benchmark가 약 0.69ms/2.80MB/15,064 allocations에서
약 0.52ms/2.11MB/15,020 allocations로 감소했다. 대부분의 남은 allocations는
feature별 property/geometry ownership clone 비용이다.
후속 안전성 점검에서 단일 대형 레이어를 복제하는 동안 context 취소를 관찰하지 않던 점을
보완했다. `MergeLayers`는 256 feature마다 취소를 확인하고, 여러 레이어의 합계가 platform
`int` 범위를 넘으면 결과 slice/map 할당 전에 오류를 반환한다. 경계 합계와 레이어 중간 취소
회귀 테스트가 통과했다.

attribute-table JSON publication 경로는 이미 detached인 page layer의 property
map을 presentation model에서 다시 복제하고 있었다. read-only 소유권 경계를
명시한 `AttributeTableOwned`를 Qt 경로에 사용해 200행 complete-schema
benchmark를 약 26.4µs/70.7KB/402 allocations에서 약
8.4µs/3.5KB/2 allocations로 줄였다. 기존 `AttributeTable` public API는
계속 map을 복제해 detached 계약을 유지한다.

writable project의 Qt attribute page도 같은 중복 복사를 가지고 있었다.
`LayerAttributePageOwned`를 추가해 read-only page에서 service-owned property
map을 재사용하고, Qt는 이를 `AttributeTableOwned`로 전달한다. 10K feature
중 200행 page benchmark가 약 32.6µs/75.5KB/402 allocations에서 약
3.2µs/8.3KB/2 allocations로 감소했다. 일반 `LayerAttributePage`의 detached
계약은 그대로 유지한다.

viewport refresh는 취소된 이전 scheduler 요청이 key slice를 계속 참조할 수
있어 일반 scratch slice를 즉시 재사용할 수 없다. `Scheduler`에 요청 channel이
닫힌 뒤 반환하는 key-buffer pool을 추가하고 Qt runtime이 그 소유권 경계를
따르도록 했다. 새 slice를 매번 만드는 planner benchmark 약
15.6µs/114.7KB/1 allocation에서 pooled refresh 경로 약
6.1µs/0B/0 allocations로 줄였으며, pointer-backed buffer를 사용해
slice-header boxing allocation도 피한다. 취소 중인 요청과 다음 refresh
사이의 data race를 피하고, request가 끝난 뒤에만 buffer를 재사용한다.

미캐시 chunk 1,000개를 4개 worker로 빌드하는 `runMissing` benchmark에서는
worker들이 공유 atomic counter로 다음 작업을 가져가던 경로가 약
138µs/172KB/1,010 allocations였다. chunk 비용이 대체로 균일한 초기 렌더
경로에서는 worker별 연속 구간을 미리 나누도록 바꿔 약 128µs/172KB/1,013
allocations로 측정됐다. chunk별 취소 확인은 유지하고, 향후 빌드 비용 편차가
큰 드라이버가 추가되면 이 정적 분할을 block 단위 work-stealing으로 재검토한다.

GDAL retained geometry session의 10K GeoJSON viewport window는 Apple M3에서
약 17.3ms/877KB/45,021 allocations였다. native boundary를 단계별로 분해하면
`NextFeature`만 약 16.6ms/80KB/10,000 allocations,
`Feature.Geometry()`까지 약 17.0ms/240KB/20,000 allocations,
`Geometry.WKB()`까지 약 19.0ms/680KB/60,000 allocations였다. 즉 현재
window 경로의 주비용은 Go-side bounds 비교보다 GDAL feature/geometry/WKB
wrapper 호출이다. 이 수치는 GDAL 빌드와 데이터 포맷에 민감하므로, Windows
이식 후 같은 benchmark를 먼저 재현하고 나서 native spatial filter API 또는
포맷별 인덱스 사용을 선택한다.

포맷별 retained geometry window도 비교했다. 10K point dataset의 작은 window에서
GeoPackage는 매번 dataset을 reopen하는 경로 약 8.28ms/133KB/7,030
allocations에서 retained session 약 0.85ms/133KB/7,025 allocations로
줄었고, Shapefile reopen은 약 6.68ms/133KB/7,030 allocations였다. 따라서
indexed 포맷에서는 `GeometrySession` 유지가 이미 주요 병목을 해결한다. 반면
GeoJSON은 retained session도 약 17ms이며, 해당 포맷은 현재 godal v0.0.18의
layer-level spatial filter API가 없어 Windows 재현 전 native 우회 최적화를
적용하지 않는다.

DXF CP949 text encoding은 기존 `Encoder.Bytes` 호출이 값마다 output buffer를
할당했다. Exporter가 encoder의 reusable source/destination buffer를 소유하도록
바꿔 100K회 반복 benchmark를 약 6.46ms/3.20MB/200,000 allocations에서
약 2.39ms/0B/0 allocations로 줄였다. encoder는 Export 호출 단위로 생성되므로
기존과 같은 비동시 사용 경계를 유지하고, CP949 표현 불가 문자는 동일하게
오류 처리한다.

DXF TEXT label의 좌표도 기존 문자열 float formatting 경로에서 exporter-owned
`writeFloat` 경로로 연결했다. 100K label benchmark가 약
11.76ms/1.59MB/299,980 allocations에서 약 0.66ms/0B/0 allocations로
감소했으며, 공개되지 않은 기존 `writeText` helper는 호환성을 위해 그대로
문자열 경로를 유지한다.

DXF의 표준 2D WKB POINT/LINESTRING은 `core.WKBGeometry.Parts()`가 만드는
part/point wrapper를 거치지 않고 byte order와 고정 layout을 직접 순회하도록
했다. 10K 반복 LINESTRING benchmark가 약 512µs/560KB/20,000 allocations에서
약 171µs/0B/0 allocations로 감소했다. Z/M, EWKB metadata, collection과
malformed layout은 기존 decoder로 fallback하고, EMPTY LINESTRING도 기존
skip semantics를 유지한다.

같은 direct parser를 표준 2D POLYGON의 ring에도 확장했다. 10K 반복
POLYGON benchmark가 기존 `Parts()` 경로 약 3.61ms/320KB/100,000 allocations에서
약 0.42ms/0B/0 allocations로 감소했다. ring count와 byte length를 먼저
검증한 뒤 출력하므로 malformed geometry에서 부분 출력이 발생하지 않으며,
여러 ring도 직접 순회한다. MULTIPOLYGON/Z/M은 기존 fallback 경계를 유지한다.
NaN 좌표의 빈 WKB POINT는 출력하지 않는 기존 동작을 검증했다.

실제 파일에 10K개의 2점 WKB LINESTRING을 쓰는 end-to-end benchmark에서는
기존 약 5.4~5.5ms/1.28MB/40,006 allocations가 측정됐다. 각 좌표를 쓸 때
임시 float buffer가 heap으로 escape하던 비용을 Export 호출 내 버퍼 재사용으로
제거하고, UTF-8 출력은 `bufio.Writer.WriteString`으로 직접 기록했다. 재측정은
약 5.2~5.6ms/4.3KB/6 allocations였다. 이 변경은 주로 메모리·GC 압력을
낮추며 파일 쓰기 시간 자체의 개선은 측정 오차 범위다.

Qt 부분 렌더링의 CPU 경로를 분해하면, `BatchStore.CurrentInto`는 10K chunk의
4만 vertex에서 약 23µs, Qt bridge의 10만 vertex 복사는 약 17µs였다. 현재
크기에서는 이 복사가 주요 병목은 아니지만, chunk가 마지막 publish 직후
끝나면 종료 단계에서 같은 전체 배치를 다시 복사했다. refresh의 dirty flag로
중복 publish를 건너뛰고, 모든 레이어가 숨겨지면 이전 builder를 취소한다.
취소된 이전 요청이나 교체된 runtime은 최신 canvas에 게시하지 않도록
generation/context/runtime을 확인한다. 빈 배치도 Qt로 전달해 숨긴 레이어의
이전 vertex가 화면에 남지 않게 한다.

같은 retained `AttributeSession`의 GeoPackage 10K feature에서 200행씩 50페이지를
순차 요청하면, 기존 페이지마다 `FeatureCount`와 `OGRSQL LIMIT/OFFSET`를 실행하는
경로가 약 7.5ms/페이지 묶음이었다. 세션의 mutable layer cursor와 schema/total
cache를 적용하되, 실제 연속 페이지에만 cursor를 사용하고 page jump는
OGRSQL로 분기하는 hybrid 경로로 약 5.7ms까지 줄였다. 임의 page jump도
SQL baseline 약 1.38ms에서 hybrid 약 1.19ms로 개선됐다. cursor는 첫 페이지나
직전 페이지에 인접한 요청에서만 유지하므로 random access의 의미와 포맷별
인덱스 활용을 보존한다.

데스크톱의 `--layer` 경로는 종전에는 `OpenAllGeometryOnly`/`OpenAll`로
전체 레이어를 materialize한 후 이름으로 필터링했다. 3개 레이어에 각 3,000개
포인트가 있는 GeoPackage를 retained `AttributeSession`으로 읽는 Apple M3
benchmark에서 전체 geometry 경로는 약 7.04ms/1.20MB/63,028 allocations,
선택 레이어만 읽는 경로는 약 2.77ms/0.40MB/21,008 allocations였다.
read-only는 같은 세션에서 선택 레이어만 읽고 속성 조회용 dataset을 유지하며,
편집 모드도 `Reader.Open`으로 선택 레이어만 읽는다. 이 수치는 GDAL read 단계의
반복 측정이며 실제 앱 시작 전체 지연이나 Windows 성능을 대표하지 않는다.

데스크톱 10K GeoJSON read-only 로더를 같은 파일로 분해 측정하면 GDAL snapshot이
약 57.3ms/1.32MB, render source 생성이 약 0.81ms/2.54MB, 전체 로더가 약
57.7ms/4.59MB였다. 이 입력의 CPU 병목은 GDAL/GeoJSON 파싱과 feature 순회이며
render source의 미세 최적화로 시작 시간을 크게 줄일 수 없다. 다만 로더는
`LayerSource.Features`를 hit-test용 배열에 전부 복사해 중복 header storage를
유지하고 있었다. `NewLayerSourcesWithFeatures`가 레이어별 view와 전체 view를
하나의 불변 backing array로 반환하도록 바꾼 후 전체 로더는 약 58.3ms/3.87MB로
측정됐다. 약 0.72MB의 할당 감소가 확인됐고 CPU 차이는 측정 오차 범위다.
slice capacity를 레이어 끝에서 제한해 append가 다음 레이어 view를 덮지 못하게
하며, UI는 생성 후 feature storage를 변경하지 않는다.

GeoJSON 10K 초기 로딩을 GDAL 경계에서 추가로 분해하면 `godal.Open`만 약
31.5ms/144B/4 allocations이고, `FeatureCount`를 함께 실행해도 약
30.7ms/166B/7 allocations로 차이가 없다. retained dataset에서 10K개를
`NextFeature`로 순회하는 하한은 약 16.7ms, geometry wrapper 포함 약
17.1ms, WKB 생성 포함 약 19.2ms였다. 전체 geometry-only 세션 읽기는
약 19.3ms/1.32MB이다. 따라서 사전 count를 생략하는 변경은 기대 효과가
없고 slice 재할당을 되살릴 수 있어 적용하지 않는다. 남은 주요 지연은
GeoJSON의 GDAL dataset open/파싱과 native feature iteration 경계다.
같은 파일을 Go 표준 `encoding/json`으로 Point 좌표와 feature 목록만 읽는
탐색적 하한 benchmark는 약 21.4ms/2.63MB였다. 이는 CRS, geometry 변종,
GDAL attribute session, render source를 포함하지 않아 전체 로더와 동등하지
않다. 현재 UI는 로드 직후 첫 속성 페이지도 게시하므로 순수 Go parser를
추가해도 GDAL dataset open을 완전히 피하지 못한다. 포맷별 fast path는
Windows 재현과 함께 속성 페이지 지연 로딩·호환성 테스트를 설계한 뒤 판단한다.
GDAL `VectorOnly()` open 옵션도 같은 10K GeoJSON 파일에서 기본 open 약
24.1~24.5ms 대비 약 24.0~24.3ms로 유의한 차이가 없었다. 파일 내부 JSON
파싱이 지배적인 것으로 해석하며, driver filtering만으로 해결하려는 변경은
적용하지 않는다. 앞 절의 31ms 값과 차이는 별도 benchmark 실행의 캐시·온도
영향이므로 같은 실행 안의 쌍 비교를 판단 기준으로 삼는다.

속성 테이블의 큰 page jump도 UI 응답을 지연할 수 있다. GeoJSON 10K에서
9,800행 위치의 200행을 읽는 retained OGRSQL 경로는 약 16.0~16.1ms,
cursor를 처음부터 직접 순회하는 경로는 약 16.4~16.7ms였다. SQL이 allocation도
약 185KB/2,415개로 direct cursor 약 263KB/12,203개보다 적어 기존 random
page 정책을 유지한다. 초기 geometry read 직후 첫 200행 page는 약
4.1ms/1.83MB였고, count 결과를 재사용해도 개선되지 않아 count cache 변경은
적용하지 않았다. 이 조회는 종전 Qt viewport polling goroutine에서 동기 실행되어
page jump 동안 pan/zoom 처리를 막았다. 조회를 별도 goroutine으로 넘기고 요청
세대 및 context 취소로 오래된 결과가 새 페이지나 새 dataset을 덮지 않게 했다.
GDAL/OGRSQL 호출 자체는 취소 불가능할 수 있으므로 진행 중인 호출은 끝나야
하지만 UI polling은 이를 기다리지 않는다.

read-only 지도 클릭에서도 feature 이름을 GDAL `OpenFeature`로 동기 조회했다.
GeoJSON 10K의 마지막 feature를 비인접 상태에서 읽는 retained session benchmark는
약 16.6ms/81KB/10,017 allocations였다. 이 조회를 선택 표시 이후 비동기로
실행하고, 새 클릭·선택 해제·dataset 교체 시 이전 결과를 취소/폐기한다.
선택한 feature의 이름 캐시는 mutex로 보호하고, 캐시된 이름은 즉시 표시한다.
GDAL 호출 자체는 여전히 순차 feature scan 비용을 가지지만 viewport polling
goroutine의 pan/zoom 처리를 막지 않는다.

전체 snapshot을 기다리지 않고 실제 데이터 preview를 게시하기 위한 native
경계로 retained `AttributeSession.Inspect`와 `OpenGeometryPrefix`/
`OpenAllGeometryPrefix`를 추가했다. overview는 원본 CRS의 bounds와 feature
count를 제공하고, prefix는 이후 전체 읽기와 같은 read-order feature ID를
사용한다. GeoPackage 10K point benchmark에서 overview+첫 1,000개 geometry는
약 1.27ms/133KB/7,017 allocations였고, retained 전체 10K geometry는
약 7.03ms/1.32MB/70,010 allocations였다. GDAL open+Bounds는 같은 파일에서
약 2.54ms로 open-only 약 2.11ms보다 약 0.43ms 더 걸렸다.

Qt read-only 로더는 이제 50,000개 이상의 feature가 있고 모든 대상 레이어의
bounds·count가 유효하며 표시 CRS가 같은 경우, 레이어당 최대 2,000개를 먼저
보여준다. 많은 소형 레이어에서 prefix 총량이 전체 feature의 절반을 넘으면
미리보기가 중복 full read가 되는 것을 피하기 위해 생략한다. 전체 extent로
prefix와 full snapshot을 정규화해 지도 위치가 교체
중 이동하지 않으며, preview는 GDAL attribute session을 소유하지 않는다.
혼합 CRS나 표시 CRS 변환이 필요한 경우에는 잘못된 bounds를 추정하지 않고
기존 전체 로드 경로를 사용한다. 같은 Apple M3에서 50K GeoJSON point의
전체 로드는 약 182~184ms/18.9MB, preview 경로는 첫 preview 생성까지 약
104ms, 전체 완료까지 약 200~201ms/19.7MB였다(각 5회, 2회 반복).
첫 preview 준비는 약 43% 빨랐지만 전체 완료 시간은 약 9% 늘었다.
이는 실제 Qt 첫 paint 시각이 아닌 Go loader callback 시각이며, 큰 실제
파일 및 Windows에서 화면 표시 시간과 메모리를 별도로 확인해야 한다.
preview→full 전환 중 viewport polling이 교체 가능한 scheduler와 visibility를
락 없이 참조하던 경로도 함께 수정했다. 레이어 표시 상태는 hit-test가 보유한
이전 map snapshot을 변경하지 않도록 copy-on-write로 갱신한다. Qt race
테스트에서 병렬 scheduler 교체·viewport 상태 변경을 반복 검증했다.

동일 viewport/key의 캐시 재방문은 이전에는 변경되지 않은 chunk를 다시
flatten하고 Qt bridge에 복사해 scene graph geometry를 반복 갱신했다.
`BatchStore`에 visible order/immutable chunk backing 기준 content revision을
추가하고, 마지막 게시 revision과 같으면 flatten·C++ 복사를 생략한다.
Apple M3의 100K vertex 반복 캐시 마이크로벤치마크에서 기존 flatten은
약 24.6~26.3µs, revision-skip은 약 51~54ns였다. `GOGIS_PERF=1`로
작은 샘플을 오프스크린 실행하면 시작 직후 동일 34 vertex의 게시·scenegraph
반영이 여러 번 발생하던 것이 각각 한 번으로 줄었다. 이 수치는 반복 게시
경로만의 비용이며 대용량 파일의 실제 첫 화면 시간 개선율은 아직 아니다.
Qt bridge 단에서 `memcmp`로 동일 vertex 배열을 판별하는 대안은 100K
vertex 반복 호출을 기존 약 25~36µs에서 약 40~56µs로 악화시켜 제거했다.

실제 Qt scene graph 경로를 확인하기 위해 재배포 가능한 sample 선형 feature를
GDAL SQLite recursive query로 50,000회 반복한 임시 GeoPackage를 만들었다.
Apple M3 오프스크린 software backend 단일 실행에서 `GOGIS_PERF=1` 계측은
미리보기 52,000 vertices를 17.2ms에 게시하고 33.2ms에 scene graph에
반영했으며, 전체 1,300,000 vertices는 92.8ms 게시·102.7ms 반영을
기록했다. 같은 파일을 `GOGIS_DISABLE_PREVIEW=1`로 실행한 기준값은
전체 데이터가 91.9ms 게시·101.8ms scene graph 반영이었다. 즉 이 입력에서
첫 실제 데이터 geometry의 scene graph 도달은 약 68.6ms 앞당겨졌다.
추가 교차 3회 측정에서 미리보기 첫 scene graph 반영은 29.4~41.6ms,
미리보기 없는 전체 데이터는 92.5~108.3ms로, 같은 순서의 쌍 비교에서
약 59.5~78.9ms 앞섰다. 미리보기 포함 전체 완료는 95.6~105.3ms,
미사용은 92.5~108.3ms로 총 완료 시간 차이는 실행 변동 범위 안이었다.
startup demo 배치는 입력 파일 데이터가 아니므로 첫 데이터 표시 시간에서
제외했다. 오프스크린 scene graph 반영은 화면 present 완료가 아니며,
Windows 실제 파일의 지연과 메모리는 별도로 검증해야 한다.

## 다음 단계

실제 대용량 파일의 다음 과제는 초기 전체 geometry snapshot을 줄이는 것이다.
이미 있는 원본 CRS 기준 `OpenWindow`/retained geometry session을 UI의 비동기
viewport 로딩·캐시에 연결하고, 포맷별 spatial filter/인덱스 효과를 확인해야
한다. Windows 이식 후 동일 benchmark와 실제 파일의 시작·pan 지연을 재측정한다.

## 극단적 축소에서의 viewport 작업 상한

대용량 SHP 표시 크래시 보고서를 검토했다. macOS crash report 상단 분류는
`EXC_CRASH (SIGABRT)`이며 crashed thread는 `CVDisplayLink`의 `runtime.raise_trampoline`
경로다. 같은 report의 별도 QSGRenderThread는 snapshot 시점에 `updatePaintNode` 내부
vertex-conversion lambda를 실행 중이지만, 그 thread는 crash thread가 아니며 해당 report에
그 thread의 fault address/register state는 없다. 따라서 renderer null write나 OOM으로
귀속할 수 없고, 첨부 실행 파일과 현재 소스의 binary identity도 검증되지 않았다.
별도로 코드 점검 중 `ChunkPlanner`가 zoom 0.0001에서 0.25 단위 chunk를 데이터
extent와 무관하게 열거하면 약 1.6 billion keys까지 만들 수 있는 결정적 메모리
위험을 확인했다. 실제 layer source의 좌표는 unit square로 정규화되므로,
데이터 렌더러는 `[-margin, 1/chunkSize+margin]`에 해당하는 tile로 검색을
제한한다. 일반 planner에도 유효하지 않은 viewport 거부와 최대 65,536개 key
상한을 두어 정수 overflow 및 비정상 할당을 막는다. 극단 축소·범위 밖 이동·NaN
viewport 회귀 테스트를 추가했다. 이는 보고된 SIGABRT의 확정 원인이라는 뜻은
아니며, 실제 세종 SHP 재현 및 국가 규모 자료 검증은 별도 확인이 필요하다.

로컬 세종 도근점/연속지적도 SHP를 native read-only 로더에 함께 넣는 일회성
재현 테스트에서는 219,986개 피처가 로드됐고 약 6.40초, Go 누적 할당 3,931 MiB,
GC 후 heap 488 MiB가 관찰됐다. Go heap profile에서 `attachPolygonFillGeometry`가
누적 할당의 약 85%를 차지했다. 폴리곤 fill mesh를 만들 때 레이어 색을 매 정점마다
다시 디코드하던 일을 1회 계산으로 바꾸고, 타일별 정점 slice를 예상 크기로
예약했으며, 단순 convex 단일 외곽 ring은 GEOS 호출 없이 fan triangulation을
사용한다. 같은 데이터 재실행은 약 5.73초, 누적 할당 2,128 MiB, GC 후 heap
485 MiB로 측정됐다. 이어 simple Polygon의 닫힌 ring에 대해 삼각형 수 추정치를
`3*(pointCount-3)`로 좁혀 fill buffer를 예약했다. 최종 동일 데이터 재실행은
약 5.73초, 누적 할당 2,093 MiB, GC 후 heap 449 MiB, 해당 테스트 프로세스의
max RSS 1,091 MiB였다(초기 실행의 3,931 MiB/488 MiB/max RSS 1,544 MiB 대비
누적 할당 약 47%, post-GC heap 약 8% 감소). 10K convex polygon 재현 benchmark는
buffer estimate 조정 전 3.02 MB/op·약 257 ns/feature에서 조정 후 1.31 MB/op·약
270 ns/feature로 바뀌었고 allocations/op은 56으로 같았다. 이는 개발 호스트의
일회성 데이터 재현과 synthetic benchmark이며, macOS SIGABRT의 원인을 입증하거나
Qt 화면 렌더와 국가 규모 데이터의 메모리 안전성을 보증하지 않는다. 복잡한 ring,
holes, multipart는 기존 GEOS constrained triangulation 경로를 유지한다.

이 synthetic 회귀 기준은 `go test -tags 'qt native' ./cmd/gis-desktop -run '^$'
-bench '^BenchmarkAttachPolygonFill10KConvex$' -benchmem`으로 재측정할 수 있다.

## Read-only desktop viewport의 GDAL window 연결

알려진 CRS 및 유효한 bounds를 가진 read-only sources는 이제 전체 geometry snapshot
대신 renderer chunk마다 원본 CRS의 retained `AttributeSession.OpenWindowWithLimits`를
호출한다. 표시 CRS와 다르면 PROJ densified bounds로 질의하고 반환 geometry를 표시 CRS로
변환한다. 시작 시 ProjectService에는 metadata/schema만 두고, 화면에 필요한 chunk만
렌더링한다. spatial result ID가 source 전체 ordinal과 같다고 가정하지 않고 chunk별
새 runtime ID를 배정한다. 선택 피처 이름은 해당 window 결과에서 보관한 문자열로
응답한다. viewport 변경 때 비가시 scheduler cache, hit geometry, feature-name 및
label cache를 제거한다.

방어 상한은 chunk 하나당 20,000 피처/32 MiB, 현재 viewport hit-test snapshot 전체
100,000 피처/128 MiB다. GDAL reader는 각 geometry를 WKB로 바꾸고 속성을 매핑한 뒤,
누적 WKB/property 추정값을 확인해 상한 안에 있는 feature만 result slice에 보관한다.
렌더러가 조회 완료 후 별도 계산하는 레이블 payload도 chunk 예산에 포함한다.
일반 렌더는 scheduler worker 4개를 유지하지만 window-backed read-only renderer는
worker를 최대 2개로 제한해 chunk별 일시 payload 예산이 기본 worker pool에서 네 배로
중첩되지 않도록 한다. 추가 확인에서 이 worker 상한이 viewport `Request`마다 적용되어 이전
요청의 취소 지연 builder와 새 요청의 builder가 동시에 실행될 수 있음을 발견했다. worker permit을
Scheduler 단위로 공유해 겹친 요청 전체가 설정된 상한을 넘지 않도록 하고, 취소 지연 builder를
포함한 두 요청의 동시 실행 테스트를 추가했다. runtime 교체는 새 Scheduler를 만들기 때문에,
read-only chunk의 조회/PROJ 변환/GEOS 작업은 앱 차원의 2-slot context semaphore도 공유한다.
이 gate는 이전 runtime의 취소 지연 작업과 새 runtime의 작업이 합쳐져 설정을 초과하지 않게 한다.
이 동시성 제한은 per-window heap budget이나 총 RSS 상한과 같지는 않다.
또한 read-only polygon window는 GEOS constrained triangulation 전에 총 입력 coordinate 수를
250,000개로 제한한다. 상한 초과 시 chunk 오류를 돌려 triangle slice와 fill mesh를 만들지
않는다. 이 count는 source decode 이후 적용되며 단일 WKB 변환의 순간 메모리 또는 GEOS가
좌표당 사용하는 native memory를 byte 단위로 제한하는 것은 아니다.
상한 초과 시 데이터 일부를 조용히 생략하지 않고 해당 chunk 렌더 요청에 오류를
반환하며 확대 후 재요청하도록 상태를 표시한다. 이 상한은 WKB/property byte 수나 Qt
GPU 메모리를 추정하는 경계일 뿐 정확한 RSS 상한이 아니다. 특히 단일 피처는 OGR이 읽고
WKB로 직렬화한 뒤 예산을 검사하므로, 지나치게 큰 한 피처의 순간 할당을 사전 차단하지는
못한다. GeoJSON streaming scan은 범위 filter를 위해 파일 전체를 읽을 수 있어
처리시간 역시 파일 크기에 비례한다. CRS 또는 bounds가 없어 window query가 불가능한
read-only layer는 전체 geometry fallback을 하지 않고 `--source-crs` 지정 또는 dataset
metadata 수정 안내와 함께 로드를 거부한다. editable 경로는 사용자가 명시적으로 선택한
경우 기존 전체 로드를 유지한다. 전국 자료와 실제 GUI pan/zoom 및 RSS 검증 전까지 이 변경을
OOM 안전성 보증으로 해석하지 않는다.

### 1M GeoJSON viewport 전략 비교와 남은 RSS 위험

2026-10-01 Apple M3에서 GDAL GeoJSON의 window 전략을 동일한 synthetic
1,000,000 Point FeatureCollection으로 비교했다. 파일 open, `FeatureCount`,
`Bounds`, 이후 동일한 1/16 extent window query를 수행하고 독립 프로세스의
peak RSS를 측정했다. 두 전략 모두 window에서 3,969 feature를 반환했다.

| 전략 | 전체 시간 | Go allocations | RSS 이후 open/metadata | 프로세스 peak RSS |
| --- | ---: | ---: | ---: | ---: |
| `NextFeature` + envelope 검사 | 2.99 s | 76.2 MB / 4.05M allocs | 2,625 MiB | 5,244 MiB |
| OGR SQL `SpatialFilter` | 4.55 s | 3.88 MB / 59.6K allocs | 2,625 MiB | 6,905 MiB |

따라서 SQL 경로의 낮은 Go heap allocation은 native GDAL/OGR RSS를 대변하지
않으며, 이 벤치에서는 더 느리고 peak RSS도 약 1.6 GiB 높았다. JSON 입력의
현재 envelope scan 경로는 유지하지만, 1M GeoJSON을 안전하게 로드한다고 볼 수는
없다. 특히 GDAL open/metadata 단계부터 RSS가 2.6 GiB, window scan 중 peak가
5.1 GiB를 넘었다. 기존 per-window feature/byte 한도는 결과 slice 크기만 제한하고
OGR 내부의 전체 파일 파싱·feature iteration 메모리는 제한하지 못한다.

실행 가능한 회귀 benchmark는
`go test -tags native ./drivers/gdal -run '^$' -bench
'^BenchmarkGDALGeoJSONWindow(Envelope|SpatialFilter)1M$' -benchmem -benchtime=1x`
이며, 두 case는 별도 프로세스에서 실행해야 process peak RSS가 서로 오염되지
않는다. 이 결과는 다음 단계에서 표준 GeoJSON의 진짜 streaming/indexed reader 또는
프로세스 수준의 제한/격리 전략을 구현·검증해야 함을 보여준다. 파일 크기 기반의
임의 거부만으로 이를 해결했다고 간주하지 않는다.

#### 데스크톱 read-only의 bounded streaming/index

위 결과를 반영해 표준 `.geojson`/`.json` FeatureCollection의 GDAL 세션 경로를
증분 JSON parser로 교체했다. object 경계를 직접 스캔하면서 단일 feature가 64 MiB를
넘으면 추가 버퍼 할당 전에 중단한다. feature 한 건의 geometry bbox는 타입별 좌표
배열로 계산하고, 속성 map은 화면 window 또는 요청한 속성 페이지에 포함되는 경우만
생성한다. 각 top-level metadata key/value는 bounded decoder로 최대 1 MiB까지만 읽고,
초과/손상된 FeatureCollection이 GDAL fallback을 타지 않고 명시적으로 거부되게 한다.
선택된 feature의 properties JSON은 `map[string]any`로 decode하기 전에 8 MiB로
제한해 단일 attribute payload의 메모리 확장을 묶는다. 첫 metadata scan은 feature의 byte
offset/length와 bbox를 in-memory index로 보관해 이후 viewport query가 matching feature만
`ReadAt`한다. 속성 페이지/선택 feature도
index를 사용한다. source 파일 크기 또는 mtime이 index 이후 달라지면 오래된 offset을
사용하지 않고 reload 오류를 반환한다. 최대 1M feature까지 index를 보관하고, 그보다
큰 데이터는 memory index를 버리고 bounded sequential scan으로 fallback한다. 따라서 1M
feature까지는 viewport index를 유지하고, 그 초과 시 index 메모리를 제한한다. 이 fallback은
메모리 증가를 제한하지만 pan/zoom 성능은 느려질 수 있다.

Apple M3에서 동일한 synthetic 1M Point FeatureCollection의 실제 데스크톱 read-only
loader benchmark를 `-benchtime=1x`로 재측정했다. index 적용 전 streaming scan은 약
3.91 s/op, 첫 window 1.26 s, peak RSS 64.5 MiB였고, index 적용 후 두 회차는 약
1.45–1.46 s/op, 첫 window 23.5–26.1 ms, peak RSS 272–294 MiB였다. 후자의 retained Go heap은 약 61.4 MiB,
visible window는 3,969 feature/0.177 MiB였다. GDAL 전체 FeatureCollection 경로의 약
5.2 GiB peak와 비교하면 RSS가 크게 낮아졌고, 첫 window query도 빨라졌다. 대신 메모리
index가 약 61 MiB의 retained heap을 사용하고 전체 프로세스 peak는 상승한다. 2M 초과
fallback, 복잡한 polygon, 실제 세종 SHP, 전국 데이터, 장시간 반복 pan/zoom 및 Windows/
Linux에서의 RSS는 별도 검증 대상이다. 이 결과는 합성 Point 입력의 desktop read-only
경로 증거이며 모든 GDAL `Reader.Open`/editable-large 전체 snapshot이 안전하다는 뜻은
아니다. 재현 명령은 `go test -tags 'qt native' ./cmd/gis-desktop -run '^$' -bench
'^BenchmarkDesktopReadOnlyLoadGeoJSON1M$' -benchmem -benchtime=1x -count=1`이다.

후속 메모리 점검에서 feature index 항목의 필드 정렬을 조정해 크기를 최대 48 byte로
제한했다(이전 배치에서는 56 byte). 이는 index의 이론상 retained payload를 항목당 8 byte
줄이지만, GC heap/RSS 개선 폭은 재벤치마크 전까지 측정값으로 간주하지 않는다.

이후 같은 Apple M3 1M benchmark를 한 차례 재실행한 결과는 1.211 s/op, 첫 window 28.30 ms,
process peak RSS 226.0 MiB, retained heap 49.22 MiB/op, 273,039,024 B/op 및 1,079,819
allocs/op였다. 직전 측정보다 retained heap은 약 12 MiB 낮지만 단일 run과 변동 가능한 RSS라
성능 개선의 확정치로 일반화하지 않는다. 재현 명령은 위와 같으며 `CGO_CXXFLAGS=-std=c++17`
을 지정해야 Qt native 의존성이 빌드된다.

properties 사전 제한 이후의 추가 1M Point run은 1.225 s/op, 첫 window 26.82 ms, peak RSS
225.5 MiB, retained heap 49.22 MiB/op였다. 이는 상한 검증 추가로 기존 1M 경로의 처리시간이나
메모리가 유의하게 나빠지지 않았음을 확인하는 단일 재실행이다.

Top-level metadata bounded parser 변경 후 1M Point run은 1.217 s/op, 첫 window 28.62 ms,
peak RSS 225.3 MiB, retained heap 49.23 MiB/op였다. 단일 실행이므로 앞선 결과와의 미세한
차이는 parser 비용 또는 실제 성능 변화로 해석하지 않는다. 전체 `scripts/verify.sh`도 통과했다.

Native WKB preflight 추가 후 `-count=3` 1M benchmark는 1.198–1.207 s/op, 첫 window
14.86–29.61 ms, retained heap 49.23 MiB/op였다. Peak RSS 225.4–281.7 MiB는 run마다 크게
흔들려 유의미한 RSS 회귀 판정에는 사용할 수 없다. 앞선 1.217 s 1회 결과와 비교하면 처리
시간의 지속적인 악화는 관찰되지 않았지만, first-window latency 변동이 커 반복 측정이 더
필요하다. 직전의 1.73–2.31 s 단일 runs는 재현되지 않아 시스템 변동성 outlier로 분류한다.

WKB 렌더 좌표의 유한성 검증을 extent 집계 경로에 추가한 뒤 Apple M3에서 동일한 1M
GeoJSON benchmark를 다시 실행했다. 한 번의 결과는 1.225 s/op, 첫 window 28.55 ms,
process peak RSS 212.9 MiB, retained heap 49.23 MiB/op, 272,899,504 B/op 및 1,079,862
allocs/op이었다. 직전 기준과 비슷한 범위이며 단일 실행이므로 미세한 차이를 회귀/개선으로
판정하지 않는다. 3,969 visible feature와 0.1772 MiB payload가 생성됐다.

GDAL의 64-bit `GIntBig` feature count를 바인딩에서 32-bit C `int`로 좁히던 변환도
제거했다. Go `int` 범위를 벗어나는 값이나 음수 오류 센티널은 이제 잘못된 layer size로
전달하지 않고 오류가 된다. 이는 32-bit 빌드 및 극대형 layer의 정책 판단을 방어하며,
일반 1M dataset의 동작 경로는 바꾸지 않는다.

### 화면 줌에 따른 read-only 청크 세분화

실제 세종시 SHP로 수행한 window-loader 회귀 테스트에서 연속지적도는 0.25
정규화 단위의 기본 청크와 bucket 1–3에서도 일부 창이 20,000 피처 제한을
넘었다. 고정 청크 크기라면 더 확대해도 같은 공간창을 재조회하므로 이 자료를
안전하게 볼 방법이 없었다. 이에 read-only 렌더링은 zoom bucket `b`에 대해
`0.25 / 2^min(max(b, 0), 10)` 청크 크기를 사용한다. 타일 크기는 chunk key의
zoom bucket에서 재구성되어 비동기 빌더에서도 planner의 가변 상태에 의존하지 않는다.
공용 source builder와 폴리곤 fill tessellation도 임의 청크 크기를 처리하고, 기본
4×4 그리드로 고정됐던 경계 클램프를 제거했다. 청크마다 20,000 feature/32 MiB,
viewport 전체 100,000 feature/128 MiB 상한은 유지하며 초과 데이터는 생략하지 않는다.

macOS 개발 호스트의 opt-in 회귀 테스트에서 연속지적도는 bucket 0–3의 일부 창에서
상한에 걸렸지만 bucket 4의 첫 유효 창에서 8,962 피처와 735,647 vertex를 생성했다.
도근점은 bucket 0에서 11,969 피처 청크를 읽었다. 이는 실제 SHP의 window-query와
Go geometry/render-source 경로 검증이며, 사용자 GUI의 패닝/줌 반응성, Qt/GPU 메모리,
최대 RSS 또는 전국 자료의 안전성을 증명하지 않는다. 대형 단일 피처, 예상치보다 큰
GEOS triangulation 출력, CRS 없는 입력 및 전체 범위가 한 화면에 들어오는 초기 줌은
별도 검증 대상으로 남긴다.

후속으로 read-only window의 렌더 source는 요청된 chunk 하나에 대해서만 line/point
vertices를 생성하며, 선분은 해당 셀에 직접 clip한다. 폴리곤 fill도 요청 셀의 삼각형
부분만 보관한다. hit-test 용 feature geometry는 선택과 식별을 위해 window 전체를
유지하므로 그 비용은 앞의 feature/byte budget에 계속 포함된다. 실제 세종 테스트의
첫 bucket-4 셀에는 반환 피처가 모두 해당 셀에 모여 있어 vertex 총량은 여전히 컸다.
이 변경은 청크 밖 정점의 중복 생성과 장거리 선의 전 셀 순회를 줄이는 것이며, 단일
복잡 geometry의 GEOS triangulation 자체에 대한 별도 상한은 아직 없다.

2026-10-01 Apple M3에서 desktop read-only loader를 `-benchmem -benchtime=1s
-count=1`로 재측정했다. 10K synthetic GeoJSON load는 19.65 ms, 50K load는
88.78 ms였다. 50K read-only preview 경로는 198.2 ms/op, 첫 preview callback까지
113.1 ms, 647 KB/op 및 14,159 allocs/op이었다. 처음에는 preview용 세션을 닫고
windowed runtime이 같은 파일을 다시 열었다. preview와 viewport runtime이 하나의
`AttributeSession`을 소유권 이전 방식으로 공유하도록 바꾸어 재개방/재파싱을
제거했다. 동일 호스트의 후속 `-benchtime=1s -count=3` 결과는 89.4–93.5 ms/op,
첫 callback까지 88.8–92.9 ms, 647 KB/op, 14,152–14,152 allocs/op이다. 단일 회차
초기값과 반복 3회 후속값의 비교이므로 방향성만 참고하며, 실제 SHP의 공간 인덱스,
Qt frame presentation, 세종 데이터 GUI 반응성과는 구분한다. GUI pan/zoom과 peak
RSS 측정은 사용자 후속 검증 목록에 남아 있다.

## PROJ visualization axis-order 회귀 및 성능 확인

EPSG:5179/5186의 authority axis order는 Northing/Easting이지만 core/WKT와
GDAL/지도 좌표 계약은 XY=Easting/Northing이다. Geometry 변환은 기존에 source/target이
EPSG:4326인지 여부만 수동 보정하고 projected target axis는 그대로 반환해, 해당 두
projected CRS에서 geometry 좌표가 bounds의 visualization-normalized 좌표와
뒤바뀌었다. `Transform`/`TransformLayers`/`TransformBounds`가 공유하는 pipeline을
`NormalizeForVisualization`으로 만들고 WKT/WKB 모두 XY visualization order로
변환하도록 통일했다. EPSG:4326 `POINT (127 37)` 회귀는 EPSG:5179
`(955511.809285, 1889174.174347)`, EPSG:5186 `(200000, 489012.955691)`을
검사하며, degenerate point bounds가 geometry 좌표와 2 cm 안에서 일치하는지도
검증한다. 이 tolerance는 현재 PROJ operation의 수치 회귀 기준이며 datum의 실제
정확도 보증은 아니다. 독립 QGIS 비교는 사용자 후속 확인으로 남겼다.

수정 후 Apple M3 native WKB 10K 변환 benchmark를 `-benchmem -benchtime=1s
-count=3`으로 실행했다:

| Geometry | Time / 10K | Bytes / 10K | Allocations / 10K |
| --- | ---: | ---: | ---: |
| Point | 0.768–0.771 ms | 1.264 MB | 10,010 |
| 2-point LineString | 1.230–1.237 ms | 2.063 MB | 10,013 |
| 5-point Polygon | 2.447–2.505 ms | 4.074 MB | 10,017 |

이 값은 axis-order 수정 후 성능 기준이다. 이전 기록의 WKB point 약 0.84 ms와
비교해 눈에 띄는 저하는 관찰되지 않았지만, benchmark run 조건이 완전히 같지 않으므로
이를 수정에 따른 성능 향상으로 해석하지 않는다. `go test -tags native
./drivers/proj` 및 저장소 전체 검증이 통과했다.

## Materialized snapshot 상한

viewport 기반 read-only FeatureCollection 로딩은 streaming/index 경로를 사용하지만,
공용 `Reader.Open`/`OpenAll` API와 편집용 snapshot은 core layer 전체를 메모리에
보관한다. 이 경로들이 대용량 파일에서 OOM으로 프로세스를 종료시키지 않도록
feature 100,000개 또는 추정 payload 128 MiB 중 먼저 도달하는 지점에서 중단한다.
GeoJSON FeatureCollection은 먼저 bounded streaming scan으로 개수와 구조를 확인한
뒤 snapshot 허용량 안에서만 materialize하며, 일반 GDAL layer도 FeatureCount를
사전 확인하고 읽기 중 누적량을 제한한다. 이 제한은 read-only viewport session의
별도 가상화/index budget에는 적용하지 않는다. 한도를 넘는 데이터는 일반 snapshot
API에서 명시적으로 오류를 내므로, UI/호출자가 viewport session 또는 indexed source를
사용하도록 안내한다. GeoJSON snapshot은 materialization 전 단일 feature 원문도
8 MiB로 제한한다. 이는 좌표/property 파싱 전에 큰 JSON object가 메모리를 급증시키는
것을 막기 위한 snapshot 전용 제한이며, viewport streaming 경로는 기존 64 MiB per-feature
scanner 한도를 유지한다. property payload 예산은 중첩 object/array의 key, container 및
값 비용까지 재귀적으로 포함한다. `TestReaderOpenRejectsOversizedGeoJSONSnapshot`은
100,001개 feature 입력, `TestGeoJSONSnapshotRejectsOversizedSingleFeatureBeforeGeometryDecode`
는 단일 oversized feature가 materialize 전에 거부되는지 확인한다.

추가 검토에서 같은 snapshot 한도가 일반 `Reader` 진입점에는 적용되지만, 재사용
`AttributeSession.OpenAllGeometryOnly` 및 `OpenGeometryOnly`의 GeoJSON 경로는 제한 없는
window scan을 사용하고 GDAL 단일-layer 경로는 unbounded reader를 호출하던 누락을 찾았다.
세션 geometry-only API를 모두 기존 100,000 feature/128 MiB snapshot 경계에 연결했다.
100,001-point GeoJSON 회귀 테스트가 두 세션 API 모두 snapshot-limit 오류를 반환하는 것을
확인했으며 `go test -tags native ./drivers/gdal`이 통과했다. 실제 viewport rendering은
세션 snapshot 대신 bounded spatial-window API를 사용하므로 이 변경은 대용량 자료를
전체 materialize하지 않고, 비-windowed 전체 snapshot을 명확히 거부한다.

Spatial window 입력 검증도 finite 좌표를 요구하도록 강화했다. 기존 순서 비교는 NaN을
거부하지 않아 envelope 교차 판정에서 비교 결과가 모두 false가 되고, 의도한 공간 필터가
사실상 전체 feature를 통과시키는 입력이 될 수 있었다. Reader와 재사용 세션은 데이터셋을
열거나 읽기 전에 NaN/±Inf 및 역전된 bounds를 거부하며, 회귀 테스트에서 NaN이 source open
보다 먼저 실패하는 것을 확인한다.

WKB materialization 직전에는 로컬 godal 패치의 `WKBWithMaxSize`를 사용해 정확한 OGR WKB
크기를 native 코드에서 계산하고, 출력용 `malloc` 전에 8 MiB 초과 geometry를 거부한다.
기존 `WKB()` API는 그대로 두고 bounded export API를 추가했으며, 일반 GDAL reader와 GeoJSON
stream 경로가 모두 이 상한을 공유한다. 회귀 테스트는 8 MiB보다 큰 LineString을 native
geometry로 만든 뒤 export 결과 buffer 없이 상한 오류를 받는지 확인한다. 로컬 패치는
`third_party/godal`에 upstream v0.0.18 Apache-2.0 저작권/라이선스와 비교용 README를 유지한다.

또한 `Reader.OpenWindow`, `AttributeSession.OpenWindow`, `GeometrySession.OpenWindow`의 기본
호출에 100,000 feature/128 MiB 예산을 연결했다. 이전에는 기본 convenience API가 두 제한을
모두 해제해 넓은 window가 백만 feature를 한 번에 보관할 수 있었다. 한도 초과는 truncation
대신 오류이며, `OpenWindowWithLimits`로 명시한 0 값은 기존처럼 해당 예산을 해제한다.
100,001-point 회귀 fixture로 AttributeSession과 Reader geometry-only 기본 경로 모두 오류를
내는지 검증한다.

대용량 입력과 별개로 속성 페이지 API가 호출자 지정 `limit`을 slice capacity로
사용하던 점도 allocation DoS/정수 overflow 경로가 될 수 있었다. GDAL reader/session과
project attribute API는 페이지 크기를 최대 1,000행으로 제한하며, desktop page index를
offset으로 바꾸기 전에 곱셈 overflow를 검사한다. 페이지 끝 계산은 `offset+limit`을
먼저 수행하지 않도록 고쳤다. 일반/최대/초과 limit 및 `MaxInt` page offset 회귀 테스트를
추가했다.

Apple M3에서 별도 1회 실행한 synthetic 1,000,000-point FeatureCollection desktop
read-only benchmark는 약 1.49 s load, 260 MiB process peak RSS, 61.36 MiB retained
Go heap, 67.8 ms first viewport window(3,969 features)를 기록했다. 같은 데이터의
streaming-only window query는 약 1.40 s, 50.4 MiB peak RSS였다. 이는 synthetic point
fixture의 한 번 실행한 수치이며 복잡한 실측 cadastral geometry, Qt frame latency,
대한민국 전역 데이터의 상한 보증으로 일반화하지 않는다.

## Qt scene-graph buffer 실패 방어

첨부 crash report 상단은 `EXC_CRASH (SIGABRT)`이며 실제 crashed thread는
`CVDisplayLink`다. 해당 thread의 native stack은 `runtime.raise_trampoline`에서
`raise(SIGABRT)`로 끝나고 Go panic 원인 문구나 OOM/jetsam 표시는 없다. 같은 report의
별도 `QSGRenderThread` stack은 snapshot 시점에
`GoGISMapCanvas::updatePaintNode`의 vertex-conversion lambda 안에 있지만, 그 thread는
crashed thread로 표시되지 않았고 crash register state/fault address도 제공되지 않는다.
그러므로 이 자료만으로 renderer의 null write, invalid-memory access 또는 OOM을 결론 내릴
수 없다. 첨부 실행 파일과 현재 checkout binary identity도 확인되지 않았다.

별도 코드 검토에서는 renderer의 geometry allocation 경로에 명시적 정점 상한과 allocation
반환 pointer 검사가 부족한 것을 확인해 예방 방어를 추가했다. 이 수정은 합리적인 안전성
강화지만 SIGABRT 원인을 특정하거나 해당 crash를 재현·해결했다는 증거는 아니다.

Go→C++ 입력 batch도 최대 4,194,304 source vertices(약 80 MiB native copy)로
제한해 Qt scene graph가 변환하기도 전에 C++ vector가 임의로 커지지 않도록 했다.
초과 batch는 이전 native batch를 해제하고 오류 상태를 전달한다. scene-graph output은
최대 8,388,608 vertex(최대 96 MiB `ColoredPoint2D` vertex storage)로 제한하고,
Qt API의 `int` 표현한도도 유지한다. 할당 이후 null pointer를
검사하고, 각 write에서 index를 output capacity와 비교하며, 생성 개수와 사전 계산치가
다르면 geometry를 비우고 오류 상태를 보고한다. 초과 시 전체 프로세스가 거대한
scene-graph allocation을 시도하지 않고 해당 render batch를 거부한다. `build.sh
all-native`, `go test -tags 'qt native' ./cmd/gis-desktop`, QML tests 및 실제 세종
read-only source integration은 통과했지만, native bridge vertex-boundary test만으로 C++
allocator failure injection이 되지는 않으며 사용자 GUI의
동일 scene-graph crash 재현은 아직 수행하지 않았다.

입력 feature/WKB 예산 뒤에 남아 있던 Go 렌더 확장도 chunk당 최대 2,097,152 `render.Vertex`
(약 40 MiB)를 넘지 않도록 제한했다. line/point source builder는 정확한 preallocation hint가
상한을 넘으면 초과 chunk를 만들지 않고 그 chunk build 시 명시적 오류를 반환한다.
polygon fill은 capacity estimate, 실제 triangle append, stroke+fill 결합 결과에서 같은
상한을 확인하므로 부분 mesh를 BatchStore/Qt로 publish하지 않는다. WKB LineString 회귀는
한 chunk 안에 1,048,578개 좌표를 넣어 cap을 넘겼을 때 bounded error가 나는지 검사한다.
실제 세종 read-only 테스트의 735,647 vertex chunk는 새 상한 안에서 여전히 통과했다.

chunk별 상한만으로는 편집 가능한 전체 데이터에서 화면 밖 모든 cell의 `fillByCell` mesh가
누적될 수 있어 충분하지 않다. materialized 프로젝트 전체의 polygon fill mesh 합계에도
8,388,608 `render.Vertex` capacity slot(명목상 약 160 MiB) 상한을 두고, capacity estimate의
합을 slice 할당 전에 검사하며 실제 triangle append 때 각 backing array의 capacity 증가도
누적 검증한다. 길이뿐 아니라 두 배 growth에 따른 spare capacity도 예산에 포함한다. 각
`LayerSource`가 자기 fill 정점 길이와 capacity를 보유하므로 레이어 재빌드 시 기존 다른
레이어의 사용량도 프로젝트 budget에서 차감한다. budget 초과 시 전체 mesh 준비를 명시적 오류로 중단하며 부분 mesh는 source로
publish하지 않는다. 작은 예산으로 두 번째 레이어가 잔여량을 넘는 테스트가 통과했다. 이
제한은 retained Go mesh를 제한하지만, GEOS가 하나의 feature를 triangulate하는 동안 만드는
임시 native/result geometry의 정확한 byte 상한은 아니다.

단일 feature의 GEOS triangulation 진입에도 complexity gate를 추가했다. WKB 입력은 최대
8 MiB, WKB structural decoder budget은 좌표·ring·child 합계 1,000,000 elements이며,
triangulation 대상 WKB는 coordinate tuple 최대 250,000개로 추가 제한한다. WKT 입력은
1,000,000 bytes까지 허용하고, 반환하는 Go `Triangle` 결과도 최대 250,000개다. WKB
`PointCount`는 이전에 좌표만 세어 작은 payload의 빈 ring/child 집합이 비용 제한을
우회하던 점을 수정해 같은 구조 요소 budget을 검증한다. 과한 입력은 GEOS parse/triangulation
전에 명시적으로 거부한다. 이 상한은 입력 복잡도와 Go 결과 보유량을 제한하는 것이며 GEOS
native allocator의 정확한 byte quota 또는 전체 프로세스 RSS 상한은 아니다. 회귀 테스트는
초과 WKB/WKT와 1,000,001 empty ring 입력이 bounded error를 내는지 확인한다.
새 project-accounting 경로의 동일 10K convex synthetic benchmark는 Apple M3에서 1회
실행당 약 2.97 ms, 297 ns/feature, 1.32 MB/op, 62 allocs/op이었다. 직전 기록의
1.31 MB/op·270 ns/feature·56 allocs/op와 비교하면 계수 bookkeeping 비용이 보이지만,
각 결과는 단일 실행이며 변동과 코드 경로 차이를 포함하므로 회귀 크기로 확정하지 않는다.

## 반복 viewport 이동 시 렌더 캐시 회수

반복 패닝 경로를 다시 살펴보니 viewport 밖 chunk는 map에서 지우고 있었지만 Go map의
bucket 배열은 그대로 남을 수 있었고, `orderedVerts`의 잘린 backing array도 이전 chunk의
vertex slice를 계속 참조할 수 있었다. 방문한 공간 셀 수가 누적되면 현재 화면에 보이지
않는 geometry가 메모리에 남을 수 있는 구조였다. viewport 갱신 때 보이는 키만으로
`BatchStore`의 map/index를 재구성하고, 이전 ordered-slice backing arrays의 모든 포인터를
clear한다. Scheduler cache도 같은 방식으로 보이는 chunk만 새 map에 담아 교체한다.
명시적인 빈 viewport는 모든 chunk와 geometry 참조를 비운다. 기존 variadic API에서
visible 인수를 생략하는 동작은 호환성을 위해 그대로 유지하고, 화면 갱신 호출부는
빈 목록과 생략을 구분하는 명시적 API를 사용한다.

회귀 테스트는 100개의 서로 다른 viewport key로 이동하면서 BatchStore/Scheduler의
논리적 엔트리 수가 현재 화면 범위에 머무는지, truncate한 slice 뒤의 geometry 참조가
clear되는지, 빈 viewport에서 모두 비워지는지 확인한다. `go test -race ./internal/render`
및 전체 `scripts/verify.sh`가 통과했다. 실제 세종 SHP 통합 테스트도 재실행해 연속지적도
첫 유효 bucket-4 창(8,962 features, 735,647 vertices), 도근점(1 feature, 2 vertices),
결합 프로세스 peak RSS 155 MiB를 확인했다. RSS는 단일 테스트 프로세스 측정이며 GUI/GPU
전체 메모리나 전국 자료의 상한 보증은 아니다.

## Flatten 전 viewport vertex 총량 상한

chunk별 vertex cap과 C++ input cap만으로는 충분하지 않았다. 여러 청크 각각은 허용량
이내여도 `BatchStore.CurrentIntoVersion`이 화면 전체 vertex를 하나의 Go slice로 먼저
평탄화하므로 native bridge의 4,194,304 vertex 검사에 도달하기 전에 큰 임시 allocation이
가능했다. `render.MaxBatchVertices`를 Go/native 공통 계약으로 두고 BatchStore가 chunk
적용 시 교체를 반영한 viewport 총량을 먼저 검사한다. 초과 청크는 기존 batch를 변경하지
않고 명시적 오류를 반환하며, Qt 렌더 진행 상태에도 incomplete/error로 전달한다.
같은 키의 chunk 교체는 이전 vertex 수를 차감한 뒤 검사해 정상적인 갱신을 허용한다.

회귀 테스트는 작은 테스트 한도를 사용해 기존 chunk를 보존한 채 aggregate 초과를 거부하고,
동일 키 교체는 한도 안에서 처리되는지 확인한다. native bridge는 Go 상수 경계를 직접
사용한다. C++ scene-graph input guard도 동일한 4,194,304 값으로 유지한다.

추가로 layer마다 따로 적용되던 65,536 chunk planner cap은 다중 layer 화면에서 합산
상한이 아니어서, 매우 많은 visible layer가 key slice/dedup map을 크게 만들 수 있었다.
`VisibleKeysIntoLimit`가 레이어를 합친 총량을 사전 검사하며, desktop은 viewport당 최대
65,536 key를 넘으면 이전 request를 취소하고 과도한 계획을 실행하지 않는다. 모든 desktop
렌더 경로에서 각 viewport 갱신 시 Scheduler cache를 visible keys로 교체해, read-only가
아닌 일반 materialized-source 렌더에서도 과거 패닝 chunk를 계속 보유하지 않는다. Scheduler의
`Request`/`RequestUnique`도 같은 입력 상한을 builder 실행 전 검사한다. 재사용 key buffer는
수용량을 상한으로 제한하고 반환 시 layer-string 참조를 clear한다. planner overflow,
oversized scheduler request, key-buffer capacity/reference 정리 회귀 테스트를 추가했다.

Scheduler chunk cache도 key 수만 제한하면 고밀도 창에서 큰 geometry payload가 누적될 수
있으므로, 저장하는 immutable vertex의 총량을 viewport batch와 같은 4,194,304개로 제한한다.
동일 키 replacement, viewport `RetainOnly`, layer invalidation 모두 vertex accounting을
갱신하며 상한을 넘는 결과는 cache하지 않는다. rejected replacement의 예전 key 결과도
제거해 낡은 geometry가 재사용되지 않도록 했다.

## 1M GeoJSON Point bbox scanner 최적화

Apple M3의 1M GeoJSON desktop loader가 약 5.08M allocation / 448 MB cumulative allocation을
기록해 profile 경로를 조사했다. Go 1.27 설치에는 `go tool pprof`가 포함되지 않아 CPU/heap
profile을 직접 분석할 수 없었지만, 코드 inspection상 모든 Point geometry bbox 계산이
`json.Unmarshal`로 매 feature마다 좌표 slice tree를 materialize하고 있었다. 보편적인
compact 2D Point를 위한 lexical fast path를 추가해 nested slice 할당을 제거했고, 다른
geometry, 3D Point, escaped member name과 fast-path에 확신이 없는 입력은 기존 validating
decoder로 fallback한다. malformed/trailing JSON, geometry collection, 표준 nesting 및
3D fallback 회귀 테스트를 추가했다.

같은 Apple M3의 `BenchmarkGeoJSONGeometryBoundsPoint -benchmem -benchtime=200ms -count=3`
결과는 기존 방식 503–511 ns/op, 120 B/op, 5 allocs/op에서 285–286 ns/op, 16 B/op,
1 alloc/op으로 바뀌었다. 동일 1x desktop 1M synthetic benchmark도 약 1.44 s에서 1.21 s,
448 MB/5.08M allocations에서 336 MB/1.08M allocations로 감소했고, 관측 peak RSS는
273.5 MiB에서 226.7 MiB였다. 두 RSS 측정은 한 번 실행이라 변동성이 있으며, synthetic
point 자료에 한정된다. 복잡 polygon, Qt/GPU frame latency와 전국 실자료 안전성의 증거는
아니다.

복잡한 line/polygon의 metadata bbox 계산도 이전에는 JSON 좌표를 전부 다중 중첩 Go slice로
decode했다. 이제 token-stream traversal로 bbox만 계산하고 Feature/GeometryCollection당
최대 1,000,000 coordinate position에서 명시적으로 중단한다. 이는 인덱스 구축 중 큰 단일
geometry가 coordinate-slice overhead로 RSS를 급증시키는 것을 막으며, 상한 초과 geometry는
부분 범위로 조용히 생략하지 않고 dataset scan 오류로 거부한다. 1M Point loader를 다시
실행했을 때 allocation은 336 MB/1.08M 수준으로 유지됐다. 단일 실행 peak RSS는 252 MiB로
앞의 226.7 MiB와 달라, process high-water mark 비교는 반복 실행 없이는 확정적인 향상으로
해석하지 않는다.

## GeoJSONSeq 1M bounded streaming

이전 `.geojsonl`/GeoJSONSeq import가 GDAL의 전체 sequence 경로에서 peak RSS 약 7.5 GiB를
보인 문제를 줄이기 위해, newline-delimited 또는 RFC 8142 record-separator 형식을 한 record씩
검증·처리하는 bounded reader를 추가했다. 단일 record는 기존 64 MiB feature 상한을 넘을 수
없고, viewport 결과·properties page·선택 feature는 각각 제한된 양만 materialize한다. 인덱스는
최대 1M record로 제한하며 원본 파일의 크기·수정 시각 stamp가 바뀌면 재사용하지 않는다.
정의되지 않은 여러 Feature를 한 줄에 이어 붙이는 형식은 지원하지 않는다.

Apple M3에서 `BenchmarkDesktopReadOnlyLoadGeoJSONSeq1M -benchmem -benchtime=1x`는 1,000,000
feature, 약 809 ms/op, 첫 viewport window 약 25.4 ms, process peak RSS 226.1 MiB,
retained heap 49.2 MiB, cumulative allocation 343.8 MB/3.08M allocations로 측정됐다.
같은 synthetic benchmark는 이전에 GDAL sequence 경로에서 약 7.5 GiB peak RSS를 기록했다.
따라서 관측 peak는 크게 낮아졌지만, 전체 scan의 CPU/allocation 비용은 존재하며 RSS는 단일
실행의 process high-water mark다. 이 결과는 synthetic GeoJSONSeq Point 데이터에 한정되고,
복잡한 geometry, 다른 OS, 전국 SHP/GeoPackage, 전체 편집 모드나 Qt/GPU 자원 사용의 상한을
보증하지 않는다.

2026-10-01 현재 checkout에서 GeoJSON FeatureCollection과 GeoJSONSeq 1M desktop loader를
각각 다시 1회 측정했다. FeatureCollection은 1.245 s, 첫 viewport 27.4 ms, process peak RSS
201.5 MiB, retained heap 49.23 MiB, cumulative allocation 344.8 MB/3.08M allocations였고,
GeoJSONSeq는 0.786 s, 첫 viewport 15.0 ms, process peak RSS 237.9 MiB, retained heap
49.23 MiB, cumulative allocation 343.8 MB/3.08M allocations였다. 이전 GeoJSONSeq 단일 실행의
peak RSS 226.1 MiB와 차이가 있으므로 RSS 값은 안정된 상한이나 성능 회귀로 단정하지 않는다.
Go heap/allocation은 두 형식에서 유사하지만 process RSS에는 GDAL/native runtime 및 allocator
영향도 포함된다. 두 입력 모두 synthetic point fixture이며 실제 세종 polygon GUI 렌더링의
안전성 증거가 아니다.

전체 레이어 렌더 API는 임의의 `chunkSize`를 받아 normalized unit square 전체를 순회하므로,
매우 작은 양수 chunk size 하나만으로도 지나치게 많은 격자 셀을 열거할 수 있었다. 전체 격자를
구축하는 호출에는 축당 최대 256셀(최대 65,536셀) 제한을 적용하고, 더 세밀한 값은 입력을
거부한다. 반면 화면에서 요청한 단일 청크만 만드는 viewport 경로는 전체 격자를 순회하지
않으므로 이 제한을 적용하지 않는다. 회귀 테스트는 전체 레이어의 1e-9 chunk size가 거부되고
같은 크기의 단일 청크 요청은 허용되는지 확인한다.

Lua label 비활성 조건의 반복 이동 스트레스 테스트 `GOGIS_TEST_REPEATED_VIEWPORT_1M=1`은
동일한 Apple M3에서
1M GeoJSON FeatureCollection의 128 viewport 이동을 완료했다. 총 1,952 feature hit,
process peak RSS 198 MiB, 실행 시간 약 1.72초였고 매 이동 후 viewport key/chunk/feature/payload
카운터 상한도 통과했다. 이는 synthetic Point fixture에서 cache가 과거 viewport를 계속 붙들지
않는다는 근거이며, 연속지적도급 복잡 polygon, GPU scene graph, 실제 GUI 상호작용의 검증은 아니다.

GeoJSONSeq 레코드 경계에 대한 별도 Go fuzz target은 잘린 JSON, 임의 바이트, LF/RS 조합을
포함해 30초 동안 약 19,500개 입력을 실행했고 panic/crash 없이 통과했다. 이는 parser 입력
안전성의 일부 검증이며, 64 MiB 상한까지의 worst-case 메모리/시간 측정이나 GDAL/C++ 경계
fuzzing을 대체하지 않는다.

1M GeoJSON read-only viewport stress test를 Lua label rule `return feature.name`이 활성화된
상태로도 128회 패닝했다. 1,952 visible feature와 동일 수의 Lua label을 계산했고, peak RSS는
약 193 MiB, 실행은 1.71초였다. 각 이동 후에도 viewport key/chunk/feature/payload 상한을
통과했다. 이 시험은 전체 1M label evaluation이 아니라 1M 데이터에서 반복된 visible-window
label 작업을 검증한다. 별도 scripting benchmark는 1M composer evaluations를 1.42초에
수행했지만 geometry, label object, Qt/GPU 비용은 포함하지 않는다.

## 1M GeoPackage / Shapefile viewport 비교

Apple M3에서 동일한 synthetic 1M Point population을 GeoPackage RTree 및 Shapefile QIX로
변환한 후 실제 read-only desktop loader를 한 번씩 측정했다. GeoPackage는 첫 viewport 약
7.7 ms, isolated loader child peak RSS 69 MiB였고, QIX Shapefile은 약 9.4 ms / 82 MiB였다.
같은 Shapefile에서 `.qix`를 제외하면 첫 viewport는 약 102 ms로 약 11배 느렸으며 child peak
RSS는 82 MiB였다. 전체 timed loader는 각각 약 13 ms, 16 ms, 108 ms였다. 측정은 fixture
generation을 제외하고 loader를 timed region에서 실행하며, 각 수치는 단일 실행이다. 따라서
대형 SHP의 권장 운영 조건으로 `.qix` sidecar 존재 여부를 확인하고, 없을 때는 성능이 저하될
수 있음을 UI/문서에서 안내할 근거가 된다. `.qix` 생성 자체의 비용과 실제 세종 polygon 자료는
이 synthetic point benchmark로 검증된 것이 아니다.

## 손상된 WKB의 메모리/재귀 상한

보안 점검 중 WKB `PointCount()`의 안전성 검사가 `Parts()`/`WKT()` 디코더와 좌표 mapper에는
적용되지 않는 경로를 발견했다. 작은 payload의 ring/child/point count를 그대로 slice 길이로
쓰거나 깊게 중첩된 GeometryCollection을 재귀 처리할 수 있었다. 디코더는 이제 byte length로
count를 사전 검증하고, 재귀 깊이를 64로 제한하며, 하나의 geometry decode에서 ring/child/point
요소 총량을 1,000,000으로 제한한다. `MapWKBXY`와 `MoveWKBVertex`에도 같은 재귀 깊이 상한을
적용했다. 4GB claim count, 65단계 collection, 1,000,001 empty ring 회귀 입력은 큰 할당 전에
오류로 거부되며, 일반 core/render 테스트와 WKB decoder fuzz 검증을 실행했다.

GeoJSON geometry 검사에서는 GeometryCollection 자식들을 `[]json.RawMessage`로 만들기 전에
token-stream preflight를 수행해 전체 컬렉션 자식 수를 최대 100,000개로 제한한다. Point/일반
좌표 geometry는 기존 빠른 경로를 유지하며, GeoJSON의 합법적인 `\\u` 이스케이프된 type 값도
preflight 탐지를 우회하지 않도록 한다. 좌표 1,000,000개 및 GeometryCollection 깊이 64 상한과
함께 대형 단일 피처의 OGR geometry allocation 증폭을 제한한다. escaped type과 100,001개 빈
child 회귀 입력이 배열 materialization 전에 오류 처리되는 native 테스트가 통과했다.

## 중첩 feature 속성의 메모리 추정 순회 제한

GDAL feature의 payload 크기를 합산하는 경로가 `[]any`/`map[string]any`를 재귀적으로
순회했다. 깊은 값은 Go stack을 불필요하게 키울 수 있고, 순환 참조가 유입되면 무한 순회할 수
있어 반복형 순회로 바꾸고 최대 1,048,576개 property node에서 포화 추정치로 중단한다.
50,000단계 nesting과 순환 map 회귀 테스트가 native GDAL 테스트에서 통과했다. 일반 GDAL
디코더가 순환 map을 생성한다는 뜻은 아니며, 이 방어는 payload accounting 경계가 임의 깊이와
비정상 object graph에 취약하지 않도록 한다.

## 속성 페이지 aggregate payload 상한

페이지당 feature 수(최대 1,000)와 feature당 속성 크기(최대 8 MiB)만 제한하던 경로는 최악의
경우 페이지 하나에 수 GiB를 누적할 수 있었다. GeoJSON/GeoJSONSeq indexed·streaming 경로와
OGR SQL/fallback/cursor 페이지 경로 모두 feature를 결과에 추가하기 전에 추정 payload를 누적하고,
페이지 전체가 128 MiB를 넘으면 오류를 반환한다. 기존 per-feature cap은 그대로 유지해 단일 값
복사량도 제한한다. 누적 byte budget을 초과하면 초과 feature를 결과에 추가하지 않는 단위 테스트와
각 reader 경로의 기존 attribute-page 회귀 테스트가 통과한다.

속성 JSON은 8 MiB 이내라도 아주 많은 짧은 object key/value가 map metadata를 수십 배로 늘릴 수
있다. 일반적인 작은 속성 payload에는 비용을 더하지 않도록 큰 JSON 또는 깊은 구조만 `json.Unmarshal`
전에 token-stream으로 검사하고, key/value/container를 합쳐 최대 100,000 node와 최대 128 nesting
depth로 제한한다. 50,001개 key 및 과도한 nesting fixture는 map materialization 전에 거부된다.

속성표 페이지 캐시도 개수만 제한하면 페이지당 payload 상한(128 MiB) 때문에 이론상 1 GiB 이상을
유지할 수 있었다. 캐시를 최대 8개 및 총 32 MiB로 제한하고, 개별 payload가 byte cap보다 크면
캐시에 보관하지 않으며, 합산 초과 시 오래된 항목부터 제거한다. 큰 페이지도 한 번 표시할 수는
있지만 이전 큰 페이지가 계속 쌓이지 않도록 하는 보호다. 페이지를 JSON으로 만들기 전에는
HTML/control-character escaping 확장과 nested value 구조를 포함한 보수적 크기 추정을 수행하고,
16 MiB 초과 시 `json.Marshal` 및 Qt bridge 복사를 건너뛴다. 따라서 단일 페이지 직렬화 과정도
큰 escape expansion으로 C++/QML 쪽 복사본이 급증하지 않게 제한한다.

## Read-only window 속성 payload 계량

read-only viewport의 runtime payload 검사가 최상위 문자열/바이트 값만 세어 중첩 JSON
배열·객체의 실제 크기를 누락했다. 속성별 추정도 반복형 node-budget 순회로 바꿔 중첩
컨테이너를 계량하고, 65,536 node 초과 또는 payload budget 초과 시 포화값을 반환한다.
대형 nested string과 순환 map 테스트가 통과한다. GDAL reader의 별도 제한을 대체하지 않고,
runtime 경계에서 driver가 달라도 같은 payload budget이 적용되도록 하는 방어다.

추가 검토에서 property 추정기가 순회 횟수만 제한하고 `[]any`/map의 전체 자식을 작업 slice에
한 번에 추가할 수 있음을 확인했다. 최대 pending node 수도 65,536으로 제한하고 초과 시 즉시
포화값을 반환하도록 해, 거대한 폭의 컨테이너를 추정하는 과정에서 임시 작업 메모리가 급증하지
않게 했다. wide slice/string-slice/map 테스트와 race 테스트를 추가했다.

## GeoJSON spatial index 상한 초과 시 partial-index tail scan

GeoJSON FeatureCollection과 GeoJSONSeq는 최대 1,000,000개 offset/bounds entry를 메모리에
유지한다. 이전에는 다음 feature에서 cap을 넘으면 prefix index까지 버리고 이후 viewport마다
전체 파일을 재파싱했다. Apple M3 단일 실행에서 1,000,001-feature GeoJSON의 첫 창이 1,099 ms,
loader가 3.434 s였다. 이제 bounded prefix를 유지하고, index에 없는 suffix만 순차 파싱한다.
GeoJSONSeq는 newline/RS record 경계에서 같은 방법으로 suffix를 스캔한다. Tail의 feature ID는
prefix 길이 뒤의 원본 순번을 유지하며, feature 조회/attribute page가 index 밖이면 정확성을 위해
streaming fallback을 사용한다.

Suffix 순차 스캔은 viewport가 tail과 거의 겹치지 않아도 tail 길이에 비례하는 비용이 남는다.
이를 줄이기 위해 tail을 최대 4,096 feature 단위의 연속 byte-range block으로 나누고 각 블록의
feature bounds union을 저장한다. viewport bounds와 교차하지 않는 블록은 파일에서 읽지 않는다.
요약 블록 수는 32,768개로 제한하고, cap 초과 시 인접 블록을 계층적으로 합쳐 메모리를 제한한다.
상세 prefix index는 항목 수뿐 아니라 Go slice의 backing-array capacity도 설정된 feature cap을
넘지 않도록 용량 증가를 직접 제어한다. 따라서 마지막 성장 단계의 자동 capacity over-allocation이
인덱스 메모리 예산을 초과하지 않는다.
FeatureCollection과 GeoJSONSeq 각각에서 비교차 블록을 건너뛰고 마지막 블록의 ID/offset을
검증하는 회귀 테스트를 추가했다.

회귀 테스트는 partial index가 있는 두 포맷의 tail window와 tail feature/attribute lookup을
검증한다. 재측정에서 1,000,001-feature GeoJSON 첫 창은 26.07 ms(전체 1.252 s, process peak
RSS 232.6 MiB), GeoJSONSeq 첫 창은 26.32 ms(전체 0.792 s, peak RSS 164.8 MiB)였다. 이는
각각 단일 synthetic Point 실행이다. Prefix index 자체는 1M entry로 제한되며, 이 cap 이후의
suffix는 bounded spatial block summary를 이용해 비교차 범위를 건너뛴다. 다만 넓은 viewport,
공간적으로 뒤섞인 feature 순서, 또는 block summary cap으로 합쳐진 범위가 많으면 더 많은 tail을
읽는다. 메모리/CPU 상한이나 10M 이상 데이터의 응답성 보장은 아니다.

1,000,000 prefix + 100,000 tail synthetic Point benchmark는 Apple M3 단일 실행에서 검사 1.369 s,
첫 window 30.59 ms, process peak RSS 222.8 MiB, retained heap 49.2 MiB를 보였다. 이는 공간적으로
연속된 입력에서 block skip 경로가 동작하는 증거이지, 반복 실행 통계나 무작위/대한민국 전역 자료의
성능 보장은 아니다.

인덱스 cap 이후 `OpenFeature`도 전체 파일을 처음부터 스캔하던 경로를 tail block의 ordinal 이진
탐색으로 바꿨다. 요청한 feature가 속한 block만 읽어 최대 4,096 tail feature를 훑고, 마지막
feature에서는 block byte end도 검증한다. Apple M3 1.1M GeoJSON(1M prefix +100K tail)의 마지막
피처 반복 lookup은 3회 실행 묶음에서 1.43–1.72 ms/op, 1.05 MB/op, 27 allocs/op였다. Source
inspection은 benchmark timed region 밖이다. 이 수치는 warm local file cache 환경이며, 첫 검사나
실제 storage latency를 포함하지 않는다.

같은 1.1M 규모에서 tail 입력 순서를 결정적 affine 순열로 섞는 benchmark도 추가했다. Apple M3
3회 실행에서 첫 window는 130.9–140.4 ms, 전체 load/index는 1.465–1.516 s, GC 후 retained heap은
49.3 MiB였다. 무작위 tail에서는 각 4,096-feature block의 bbox가 넓은 영역을 덮어 비교차 block을
적게 건너뛰므로 정렬 데이터보다 첫 window가 약 4배 느렸다. RSS 값(232.9–263.5 MiB)은 한 Go
benchmark 프로세스에서 세 반복을 수행하며 `getrusage` peak가 누적된 수치여서 반복별 독립 peak로
해석하면 안 된다. 이 결과는 대략 1.1M 입력의 처리 가능성을 보여줄 뿐, 10M 이상이나 넓은 viewport의
latency 보장은 아니다.

별도 새 프로세스에서 shuffled benchmark를 한 번 더 실행했을 때 전체 1.478 s, 첫 window 141.0 ms,
process peak RSS 212.3 MiB, retained heap 49.3 MiB였다. 앞의 반복 실행 값보다 peak RSS가 낮아,
그 RSS 차이는 반복마다 누적되는 Go/native allocator의 resident pages와 peak 계측 특성의 영향을 받는다.

같은 partial-index source에서 128회 viewport 이동을 한 stress test도 추가했다.
1,000,001 Point feature, 1,952 total hit/label, 1.65 s, process peak RSS 194 MiB로 통과했고
각 move 뒤 이전 window cache 제거 및 viewport budget을 검사했다. 재현 명령과 반복측정/GUI 한계는
`docs/verification/deferred-user-validation.md`에 남긴다.

인덱스의 source stamp는 크기와 수정시각뿐 아니라 OS 파일 identity도 비교한다. 따라서 같은
크기·시각을 보존한 atomic replacement가 기존 byte offset index를 새 파일에 재사용하지 못한다.
동일한 inode를 직접 수정하면서 크기와 수정시각까지 의도적으로 보존하는 상황은 일반 파일 API
stamp만으로 탐지할 수 없으므로, 읽기 전후의 stamp 검증을 유지하고 reload 오류를 반환한다.

## 1M GeoJSON viewport bbox 후보 grid

기존 offset/bbox index는 viewport 후보를 고를 때마다 최대 1,000,000개 entry를 선형
순회했다. 이 작업을 줄이기 위해 per-session 256×256 uniform coarse grid를 추가한다.
Feature bbox는 교차하는 grid cell에 최대 64회 등록하고, 전체 참조는 2,000,000개로
제한한다. 이보다 넓은 geometry는 별도 overflow 목록에 두며, overflow 비율·query 영역·후보
수가 상한을 넘으면 기존 정확한 선형 경로로 fallback한다. 후보 ordinal은 중복 제거하고
원본 순서로 정렬한 뒤 원래 bbox 검사와 payload/feature cap을 그대로 적용한다. Session
mutex 아래에서 dedupe bitmap과 candidate slice를 재사용한다.

Apple M3의 synthetic 1M Point 비교에서 좁은 고배율 window(1 feature)는 3.61 ms에서
1.24 ms로, wide window(3,969 features)는 같은 실행 묶음에서 14.43 ms에서 12.17 ms로
개선됐다. grid의 일회성 build는 별도 1회 측정에서 18.54 ms와 4.93 MB allocation이었다.
따라서 첫 viewport에서 소량의 추가 지연/약 5 MiB index를 치르고 반복 pan/zoom query를
줄이는 선택이다. 측정은 단일 synthetic dataset/호스트이며 complex polygons와 다른 OS에서
다시 평가해야 한다. Differential bbox test, dense overflow와 broad-query fallback test,
GDAL native test/race, 1M 및 1,000,001-feature 128-pan stress가 통과했다.
`AttributeSession.Close()`도 overview/prefix/tail index와 함께 coarse grid, dedupe bitmap,
candidate buffer 및 source stamp를 비워, 닫힌 상태로 계속 참조되는 session이 spatial index
메모리를 붙잡지 않게 한다. 50K-entry session test에서 grid 생성 후 close가 이 상태를 모두
해제하며 통과했다.
