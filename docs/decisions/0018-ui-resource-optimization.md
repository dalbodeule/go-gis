# ADR 0018: UI bridge polling and idle resource budget

## 상태

Accepted — 2026-10-23

## 결정

QML 상태 동기화 타이머는 유휴 상태에서 125ms, 렌더링·정점 편집 중에는 33ms
(약 30Hz)로 적응시킨다. Go native viewport bridge polling과 Qt native property
bridge timer는 표시 중 33ms를 유지하고, 지도 viewport redraw 자체는 기존 120ms
debounce를 유지한다.

상태 전달은 generation 값이 바뀐 경우에만 JSON payload를 파싱·복사하므로, 60Hz
polling은 대부분의 idle frame에서 실질적인 UI 변경 없이 wake-up만 발생시킨다.
작업 중 30Hz는 선택·상태·진행률 표시 지연을 약 33ms 이내로 유지한다. 유휴 QML
상태 동기화는 125ms로 완화해 JSON 세대값 확인과 Qt/Go 경계 property 조회의
idle CPU wake-up을 줄인다. 상태 payload는 generation gate를 사용하므로 유휴
주기에서도 변경을 잃지 않는다.

## 안전장치와 검증

- payload는 기존 generation gate와 크기 제한을 그대로 사용한다.
- 속성 테이블 GUI 페이지는 200행에서 128행으로 줄여 한 번의 Qt bridge 복사와 QML
  모델 생성을 bounded하게 유지한다. 전체 데이터는 페이지 이동으로 계속 접근한다.
- native scene graph는 source vertex 수와 batch 구성이 같을 때 기존
  `QSGGeometryNode`/geometry를 재사용하고 vertex data만 갱신한다. batch 구성이
  바뀌거나 안전성 검사가 실패할 때만 node를 재생성한다.
- 창이 최소화되거나 MapCanvas가 숨겨지면 visibility generation을 통해 native source
  vertex buffer를 비우고, 복귀 시 현재 viewport를 한 번만 다시 렌더링한다. 파일 선택
  요청을 수집하는 native bridge timer는 계속 유지한다.
- 창이 숨겨지거나 최소화되면 transient QML 속성 행·지도 라벨·정점 핸들 배열도 비우고,
  복귀 시 payload/generation marker를 무효화해 활성 polling이 최신 데이터를 다시 채운다.
  `visible=false`와 최소화/복원 양쪽 QML window 상태 변화를 모두 감시한다.
- 공간 연산을 포함한 모든 busy 상태에서 동일한 취소 버튼을 노출한다. 취소는 기존
  generation/cancellation 경계를 사용해 로딩·렌더·공간 연산을 일관되게 중단한다.
- QML payload 동기화 timer는 창이 최소화되거나 숨겨지면 일시 정지한다. 표시 중에는 유휴
  125ms, 렌더링·정점 편집 중에는 33ms로 동작한다. 초기 빈 지도에서
  파일 선택 이벤트를 받아야 하므로 native bridge polling은 이 변경으로 중단하지 않는다.
- native bridge polling은 창이 보일 때 33ms를 유지하고, 숨겨진 동안에는 250ms로 낮춘다.
  숨김 상태에서도 파일 선택·저장 generation은 계속 수집하되 idle Qt property wake-up을 줄인다.
- Go viewport synchronizer도 같은 33ms/250ms adaptive cadence를 사용한다. 숨김 중에는
  viewport redraw를 시작하지 않고 generation 기반 파일·저장·편집 요청만 느린 주기로
  확인하며, 복원 generation에서 현재 viewport를 한 번만 다시 요청한다.
- 메모리 상태 모니터는 표시 중 1초, 숨김/최소화 중 5초로 샘플링한다. 사용자에게
  표시되지 않는 동안 RSS/Go heap 조회 wake-up을 줄이되, 표시 중 메모리 상태 UX는
  기존 갱신 지연을 유지한다. 직전 샘플과 process bytes/Go heap/kind/availability가
  모두 같으면 Qt property generation 전송도 생략한다.
- diagnostic log publisher는 bounded log generation이 바뀐 경우에만 최대 512KiB
  로그 snapshot을 JSON marshal해 Qt로 전달한다. 로그가 정지된 동안의 주기적인 전체
  복사·marshal·문자열 비교를 제거한다.
- diagnostic log publisher timer도 표시 중 250ms, 숨김/최소화 중 1초로 완화해 로그
  generation 확인 wake-up을 줄인다. 실제 로그 창을 열었을 때의 반응성은 유지한다.
- Go viewport synchronizer도 숨김 상태에서는 pending debounce나 layer toggle로 새 렌더를
  시작하지 않는다. 복원 시 visibility generation이 현재 viewport를 한 번만 다시 요청한다.
- 숨김 전환 시 Go scheduler cache, BatchStore의 현재 batch, read-only viewport feature/label
  payload까지 비우고 프로젝트·레이어 메타데이터와 source session은 보존한다. 복원은 현재
  viewport를 재생성하는 경계로 처리해 대용량 화면 데이터가 백그라운드에 남지 않게 한다.
- 레이어 목록도 속성 목록과 같이 delegate 재사용 및 `cacheBuffer: 0`을 사용해 많은
  레이어가 열렸을 때 화면 밖의 QML 객체를 보존하지 않는다.
- 레이어 tree payload 갱신은 기존 행과 새 role을 비교해 변경된 role만 `setProperty`하고,
  추가·삭제된 행만 append/remove한다. 기존 레이어 delegate를 보존해 메타데이터 갱신 때
  hover·스크롤 상태와 delegate 객체를 불필요하게 재생성하지 않는다.
- 레이어 style/label role 비교는 generation 변경 때마다 JSON stringify를 수행하지 않고,
  UI가 사용하는 명시적 필드만 직접 비교해 임시 문자열과 GC 부담을 줄인다.
- layer-tree 동기화가 기존 활성 레이어를 다시 선택할 때는 `activeLayerGeneration`을
  증가시키지 않는다. 실제 레이어가 바뀔 때만 속성 페이지 재조회와 native payload 갱신을
  발생시켜 불필요한 I/O와 UI 갱신을 줄인다.
- Go runtime도 마지막 layer-tree JSON을 기억해 동일 payload에 대한 cgo 문자열 복사와
  native bridge 호출을 생략한다. load 오류 경로와 일반 UI 경로가 동시에 접근해도 별도
  mutex로 비교·갱신을 직렬화한다.
- attribute page 조회는 대화상자가 열려 있을 때만 시작한다. 닫힌 상태의 활성 레이어
  변경·페이지 generation은 기록하되 결과를 버리는 DB/file I/O는 시작하지 않고, reopen
  시 QML이 첫 페이지를 명시적으로 재요청한다.
- 지도 피처 선택과 레코드 수정 후의 attribute 재조회도 같은 gate를 사용한다. 선택
  상태·지도 hit 결과는 유지하지만, 닫힌 속성 대화상자를 위해 버려질 page read와 cache
  재구성을 시작하지 않는다.
- PostGIS table catalog도 대화상자 종료 시 QML 모델과 native JSON payload를 함께 해제하고,
  다시 열 때 catalog generation으로 재조회한다.
- 데스크톱 PostGIS catalog는 드라이버의 `ListTablesLimited`를 통해 SQL `LIMIT 2048`을
  사용한다. 전체 목록 API인 `ListTables`는 보존해 CLI/다른 소비자의 의미를 바꾸지 않고,
  GUI가 수만 개의 metadata row를 먼저 Go 메모리에 적재하는 것을 방지한다.
- limited 조회는 `limit+1`번째 행으로 truncation 여부를 감지해 UI에 안내한다. 따라서
  메모리 상한을 유지하면서도 사용자가 목록이 일부만 표시된 사실을 알 수 있다.
- PostGIS catalog 조회는 viewport sync loop를 막지 않는 비동기 작업으로 실행하고,
  새 refresh가 오면 이전 context를 취소한다. 15초 timeout과 generation 검사를 함께 적용해
  느린 DB 응답이나 늦은 결과가 최신 UI 목록을 덮어쓰지 않게 한다.
- catalog dialog가 열린 뒤 닫히는 visibility 전환도 query cancel로 연결해, 사용자가 picker를
  닫은 뒤 진행 중인 DB 작업이 timeout까지 남지 않게 한다.
- 레이어 tree JSON은 QML 경계에서 배열 여부와 파싱 오류를 검증한다. 잘린 payload가
  동기화 timer 예외로 이어지지 않도록 일시적으로 빈 목록을 적용하고 다음 generation의
  정상 payload를 받을 수 있게 한다.
- 속성·레이블·정점 핸들·메타데이터와 load journal payload도 같은 안전 파서 경계를
  사용한다. 비정상 payload는 빈 bounded 모델/기본 metadata로 격리하고, 이후 generation의
  정상 payload가 transient UI를 복구한다.
- attribute payload는 QML 경계에서도 최대 128행·256열과 유한한 page/total 값을 재검증해,
  native 상한이 변경되거나 malformed producer가 생겨도 QML delegate/model이 무제한으로
  커지지 않게 한다.
- 대형 attribute/layer-tree payload의 변경 감지는 payload 문자열 비교 대신 native
  generation property를 사용한다. QML은 마지막 generation만 보관해 매 polling tick의
  O(payload bytes) 비교와 동일 문자열 sentinel 복제를 제거하며, QML fixture처럼 payload만
  직접 갱신하는 producer는 property change에서 local generation을 올려 호환성을 유지한다.
- CRS 공통/지역/전체 검색 목록과 라벨 필드 선택 목록에도 같은 delegate 재사용 및
  `cacheBuffer: 0` 정책을 적용해 검색·설정 대화상자에서 목록 이동 시 객체 누적을 막는다.
- 전체 CRS 검색은 입력 이벤트마다 즉시 전체 카탈로그를 필터링하지 않고 120ms debounce 후
  결과 모델을 한 번만 교체한다. 빠른 검색어 입력 중 GUI thread의 반복 배열 생성과 ListView
  모델 재평가를 줄이면서, 입력이 멈추면 기존 검색 결과와 선택 동작을 유지한다.
- DXF 내보내기 대화상자의 레이어 옵션 모델은 열 때마다 현재 레이어 트리에서 재생성해,
  레이어 삭제·추가 뒤 오래된 옵션과 QML 객체를 유지하지 않는다.
- DXF 레이어 옵션 입력 행은 `Repeater`에서 재사용 가능한 `ListView`로 변경해, 많은
  레이어를 내보낼 때 화면 밖의 CheckBox/TextField delegate를 모두 유지하지 않는다.
- PostGIS 테이블 카탈로그도 재사용 가능한 `ListView`와 `cacheBuffer: 0`을 사용한다.
  카탈로그는 공간 테이블 메타데이터만 최대 2,048개까지 전달하고, 레코드/geometry
  payload는 테이블 선택 후 필요한 페이지·viewport 경로에서만 읽어 bridge 메모리를
  분리한다.
- PostGIS 드라이버에 `ReadWindow`를 추가해 source-CRS envelope, feature 수, decoded
  byte budget을 한 번의 화면 영역 조회에 적용한다. 100,000개 이상인 테이블은
  extent·schema만 초기화한 뒤 이 window source를 데스크톱 read-only runtime에 연결하고,
  속성 페이지·선택 feature는 DB에서 지연 조회한다. 작은 테이블은 기존 편집 runtime을
  유지하며, 대형 테이블의 추가·삭제 컨트롤은 QML에서 비활성화해 실패성 편집 요청을
  막는다. 점 geometry처럼 퇴화한 extent는 작은 좌표 padding으로 렌더 가능한 bounds로
  확장한다.
- 대형 read-only layer의 속성 대화상자에는 편집이 비활성화된 이유와 작은 테이블에서만
  편집할 수 있다는 안내를 표시한다. 단순히 버튼을 disabled 상태로 두지 않아 사용자가
  데이터 손실이나 오류로 오해하지 않도록 한다.
- 속성 대화상자의 레이어 탭도 가로 `ListView`로 유지해, 레이어가 많을 때 화면 밖
  `TabButton`을 재사용하면서 기존 탭 선택 UX와 `currentIndex` 계약을 보존한다.
- 속성 대화상자를 닫으면 bounded attribute row 배열을 비우고, 다시 열 때 현재 payload를
  generation gate로 재적용해 닫힌 대화상자가 행 delegate를 계속 보유하지 않게 한다.
- 진단 로그 `TextArea`는 로그 대화상자가 열린 동안에만 JSON을 문자열로 변환하고, 닫히면
  표시 문자열을 비워 렌더 중 불필요한 QML 파싱·문자열 보유를 줄인다.
- 라벨은 Go가 레이어 가시성·viewport·축척·declutter·밀도 제한을 적용한 결과만
  QML로 전달하므로, 라벨 delegate에서 레이어 목록을 반복 순회하지 않고 즉시 표시한다.
  이로써 라벨 가시성 갱신의 GUI 계산을 `O(labels * layers)`에서 payload 갱신 비용으로 줄인다.
- 라벨 QML 모델은 행별 `ListModel.append()` 대신 배열을 한 번에 교체하고, 기존
  `count/get` 조회 호환 객체를 유지한다. 대형 라벨 payload 갱신에서 행별 모델 신호와
  중간 상태를 줄인다.
- 속성 테이블도 bounded page 배열을 한 번에 교체해 페이지 전환 때 최대 128회의
  행별 모델 변경 신호를 없앤다. 화면 delegate 재사용 정책과 함께 페이지 메모리를 제한한다.
- 정점 편집 핸들도 배열 일괄 교체를 사용해 편집 상태 갱신에서 행별 모델 신호를 줄이고,
  기존 `count` 기반 편집 버튼 활성화 계약은 유지한다.
- 정점 핸들 데이터는 편집 버튼 활성화에 필요한 bounded 배열로 유지하되, interactive
  `Repeater` 모델은 정점 편집 모드에서만 연결한다. 평상시에는 최대 10,000개의 QML
  Rectangle/MouseArea delegate를 생성하지 않아 지도 idle 메모리와 scene-graph 객체 수를 줄인다.
- 레코드 추가 대화상자는 검증 실패 시 자동으로 닫히지 않는 사용자 정의 확인 흐름을 사용한다.
  빈 ID/WKT나 잘못된 Properties JSON은 입력값을 유지한 채 오류 상태만 표시하고, 유효한
  요청만 edit generation을 발행한 뒤 대화상자를 닫아 재입력 비용과 잘못된 작업 오해를 줄인다.
- 지도 이동 중 expensive render를 매 tick마다 재시작하지 않고 기존 120ms debounce를
  유지한다.
- QML 상태 동기화 타이머의 유휴 125ms·활성 33ms 적응 동작을 회귀 테스트로 고정한다.
- QML 상태 동기화는 모든 비-`Ready` 문자열을 busy로 취급하지 않고, 실제 loading/saving/
  rendering/spatial/reprojecting 등 진행 상태만 33ms cadence를 사용한다. 라벨 제한 안내나
  완료·취소·오류 메시지가 표시된 idle 상태에서는 125ms로 돌아가 불필요한 GUI wake-up을 줄인다.
- 동일한 render status는 native generation과 stdout/stderr 알림을 반복하지 않는다. 진행률이나
  오류 문구가 실제로 바뀐 경우에만 QML property와 터미널 출력이 갱신되어, 반복 상태 보고가
  UI wake-up과 로그 I/O를 누적시키지 않게 한다.
- 동일한 레이블 JSON payload도 native 레이블 generation을 반복 증가시키지 않는다. 렌더 완료
  경계에서 내용이 바뀌지 않은 레이블을 QML로 재복사·재파싱·재생성하지 않아, 지도 이동이나
  대형 레이어의 반복 렌더에서 불필요한 transient 객체 churn을 줄인다.
- 동일한 레이어 트리 JSON payload도 native generation을 반복 증가시키지 않는다. 오류 처리나
  재게시가 같은 목록을 내보내는 경우 QML의 role 비교와 ListView 갱신을 생략해 layer delegate
  상태를 보존하고 GUI 작업량을 줄인다.
- 동일한 속성 페이지 JSON payload도 native generation을 반복 증가시키지 않는다. 같은 페이지를
  재요청하거나 대화상자를 다시 열 때 bounded 행 배열의 QML 파싱·교체를 생략하고, 실제 페이지
  변경만 ListView 모델에 전달한다.
- 동일한 PostGIS 카탈로그 JSON payload도 native generation을 반복 증가시키지 않는다. 테이블
  목록 새로고침이 결과를 바꾸지 않은 경우 QML ListView의 모델 재평가와 delegate 작업을 생략한다.
- 동일한 정점 핸들 JSON payload도 native generation을 반복 증가시키지 않는다. 재선택이나
  편집 상태 재게시가 같은 정점 목록을 내보내는 경우 QML delegate 재평가를 생략한다.
- 동일한 선택 layer/feature/value/status 조합도 native generation을 반복 증가시키지 않는다.
  렌더 중 빈 선택 상태를 재게시하는 경로에서 QML 선택 카드와 속성 갱신을 불필요하게 깨우지
  않으며, 네 필드 중 하나라도 바뀌면 즉시 반영한다.
- 속성 payload generation은 대화상자가 닫힌 동안 JSON 파싱·행 모델 생성을 수행하지 않는다.
  닫힌 상태에서는 generation만 소비하고 bounded 행을 비우며, 대화상자를 다시 열 때 현재
  payload를 한 번만 복원해 선택 이벤트가 속성 UI 메모리를 다시 채우지 않게 한다.
- MapCanvas가 숨겨졌거나 표시 레이어가 없을 때는 레이블 JSON을 파싱하지 않고 label 모델을
  비운다. 레이어가 다시 표시되거나 창이 복원되면 generation 변화로 현재 viewport 레이블을
  한 번만 복원해 화면 밖 `Text` 객체가 남지 않게 한다. visibility 경계에서도 marker를
  무효화해 payload 내용이 같아 native generation이 유지된 경우에도 복원을 놓치지 않는다.
- 정점 편집 delegate도 편집 모드와 MapCanvas 표시 상태를 모두 만족할 때만 생성한다. 편집
  상태가 유지된 채 모든 레이어가 숨겨진 경우에도 Rectangle/MouseArea 객체가 남지 않는다.
- Lua 편집기는 원본 `TextArea`와 Rich Text HTML을 동시에 유지하므로, 128 KiB를 초과하는
  입력에서는 구문 강조·line-number 오버레이를 끄고 원본 텍스트를 직접 표시한다. 작은
  스크립트의 강조와 escaping은 유지하며, 입력이 다시 작아지면 자동 복구한다.
- `golang.org/x/net`은 Go HTTP/2 취약점 5건(GO-2026-6617, GO-2026-6612,
  GO-2026-6611, GO-2026-6610, GO-2026-6603)의 수정 버전인 `v0.60.0` 이상을
  사용한다. 이 모듈은 직접 호출 API가 아니라 `godal` 경로에서 전이적으로 포함되므로,
  애플리케이션 코드에 우회 패치를 두지 않고 모듈 그래프에서 최소 수정 버전으로
  고정한다.
- `go test ./...`, `go vet ./...`, native Qt 테스트와 offscreen QML 테스트를
  변경 후 실행한다.
- Go/Qt generation 경계와 비동기 payload 교체에 대해 `go test -race ./...`도 실행해
  data race가 없는지 확인한다.

## 되돌림 기준

실제 GUI에서 선택·편집·진행률 표시 지연이 눈에 띄거나 입력 손실이 재현되면 QML/Go/
Qt bridge polling만 16ms로 되돌리고, viewport redraw debounce는 별도 측정 후 조정한다.

## 기준 측정

2026-10-23 Apple M3/macOS ARM64에서 다음 benchmark를 1회 실행했다.

```sh
CGO_CXXFLAGS='-std=c++17' go test -tags 'qt native' ./cmd/gis-desktop \
  -run '^$' -bench '^BenchmarkDesktopReadOnlyLoadGeoJSON1M$' -benchtime=1x -count=1
```

결과는 1M feature, 첫 viewport 18.76ms, process peak RSS 171.8 MiB, retained Go
heap 50.34 MiB, visible payload 4.25 KiB, 192,478,416 B/op 및 3,001,865
allocations/op이었다. 이 값은 synthetic GeoJSON point fixture의 단일 실행 기준선이며,
실제 polygon 데이터·Qt scene graph GPU 메모리·모든 OS의 성능 보증으로 해석하지 않는다.

추가로 2026-10-23 Apple M3/macOS ARM64에서 native bridge 기준선을 측정했다.

```text
BenchmarkSetVertices100K       33.95–34.45 us/op   0 B/op       0 allocs/op
BenchmarkAttachPolygonFill10K  2.904–2.935 ms/op  1.316 MiB/op 62 allocs/op
```

따라서 현재 측정 범위에서는 vertex bridge의 할당 제거가 확인되며, polygon fill의
할당은 Go 렌더 준비 단계에 속한다. 이 수치는 3회 측정 범위이며, 실제 데이터의
geometry 복잡도와 GPU 메모리는 별도 측정 대상으로 남긴다.

동일한 1M synthetic GeoJSON read-only benchmark를 변경 후 재측정한 결과는 첫
viewport 18.20ms, process peak RSS 172.0MiB, retained Go heap 50.35MiB, visible
payload 4.25KiB였다. 기존 기준선(첫 viewport 18.76ms, peak RSS 171.8MiB,
retained heap 50.34MiB)과 비교해 메모리 예산은 사실상 유지되며, 첫 viewport는
단일 실행 변동 범위 안에서 개선됐다. 이 측정은 UI/QML scene graph GPU 메모리와
실제 polygon 데이터 성능을 포함하지 않는다.

추가로 동일한 1M synthetic GeoJSON benchmark를 2026-10-23에 3회 측정했다. 첫
viewport 시간은 13.96–19.50ms, process peak RSS는 172.1–173.3MiB, retained Go
heap은 50.35MiB, visible payload는 4.25KiB, visible features는 64개였다. 이전
단일 기준선과 비교해 heap·payload·화면 피처 상한은 유지되며, 첫 viewport 시간은
단일 실행 변동을 포함하므로 변경 효과의 단독 증거로 해석하지 않는다.

## Lua 편집기의 대형 입력 보호

Lua 편집기는 입력 중 구문 강조용 Rich Text HTML과 원본 `TextArea`를 동시에
유지한다. 따라서 큰 스크립트에서는 입력 한 번마다 전체 소스를 다시 파싱하고,
이스케이프된 HTML 문자열과 line-number 문자열을 추가로 만들 수 있다. 128 KiB를
초과하는 입력은 구문 강조와 line-number 오버레이를 자동으로 끄고 원본
`TextArea`를 직접 표시한다. 작은 스크립트의 강조 동작은 유지하며, 사용자가
텍스트를 줄이면 자동으로 다시 활성화된다. 이 경계는 UI 입력 지연과 일시적
메모리 증가를 제한하기 위한 표시 최적화이며 Lua 실행 허용 크기나 저장 포맷의
제한은 아니다.

QML 회귀 테스트는 일반 스크립트의 색상 강조·HTML escaping·long-bracket 처리와
128 KiB 초과 입력의 plain-text fallback을 함께 확인한다.

## 배포 리소스 점검

기존 macOS ARM64 내부 번들을 점검한 결과 약 1.1 GiB였고, `Contents/Frameworks`에
ICU 계열 파일 두 개가 각각 약 32 MiB로 보였다. 두 파일은 크기는 같지만 SHA-256이
달라 install-name 또는 빌드 내용이 다른 바이너리였으므로 합치지 않았다.
`scripts/package_macos.py`는 이제 파일 내용이 실제로 동일한 비시스템 라이브러리만
기존 번들 이미지에 연결하고, 다른 바이너리일 때만 해시 suffix를 붙인다. PROJ
grid/data는 좌표 변환 정확성을 위해 삭제하거나 샘플링하지 않는다.

새 내부 `.app` 생성 후 `codesign --verify --deep --strict`는 통과했지만, `hdiutil`
DMG 생성은 여유 공간 439 GiB인 임시 경로에서도 `create failed - 장치가 구성되지
않았음`으로 exit 1을 반환했다. 이는 현재 실행 환경의 DMG 장치 구성 제한으로 판단하며,
패키지 용량 절감이나 DMG 검증을 완료로 간주하지 않는다.

번들 용량을 항목별로 재점검한 결과 `Contents/Resources/proj`가 약 775 MiB로 가장
큰 항목이었다. 이는 PROJ grid/data이며 EPSG 변환 정확성과 지원 범위를 위해 임의 삭제
또는 샘플링하지 않는다. 따라서 현재 패키지 최적화의 안전한 범위는 동일 바이너리
중복 제거와 불필요한 Qt/GDAL 모듈의 의존성 검증 후 제거이며, grid 축소는 별도 제품
정책과 정확도 검증 없이는 적용하지 않는다.

## 2026-10-23 전체 게이트 재검증

`PATH="/Users/jioo0224/go/bin:$PATH" ./scripts/verify-milestone-c.sh`를 Go 1.27.3
환경에서 재실행했다. Go/module verify, native CLI/PostGIS, Qt/native, QML 60개,
Docker Desktop 기반 live PostGIS rollback 및 데스크톱 레코드 추가·삭제, govulncheck,
patch check가 모두 통과했다. 테스트 컨테이너는 검증 종료 후 compose 정리로 제거됐다.

## 2026-10-23 취약점 보완 검증

첨부된 `govulncheck@v1.8.0` 결과에서 `golang.org/x/net@v0.58.0`의 HTTP/2
취약점 5건이 확인되어 `v0.60.0`으로 업데이트했다. 이 업데이트로 필요한
`golang.org/x/crypto`도 `v0.57.0`으로 정합화되었다.

```text
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
No vulnerabilities found.
```

모듈 변경 후 offscreen QML 회귀 테스트는 53개 모두 통과했다. 이후 PostGIS 카탈로그
선택 workflow를 추가하고, 재사용 delegate·payload generation·`postgis://schema.table`
로드 및 대형 read-only 편집 안내를 고정하는 테스트를 더해 55개 모두 통과했다.
네이티브 Qt 테스트도 새 catalog bridge를 포함해 통과했다.

PostGIS 테이블 카탈로그도 동일한 lazy 원칙을 적용했다. 네이티브 catalog JSON을
source of truth로 유지하고, QML의 테이블 배열과 ListView delegate는 카탈로그
대화상자가 열려 있을 때만 materialize한다. 닫을 때 행 모델을 비우고, 다시 열면
현재 generation의 payload를 복원한다.

정점 핸들 payload도 편집 모드이면서 지도가 표시될 때만 JSON을 파싱하고 interactive
delegate를 생성한다. 편집 모드 진입 버튼은 이미 파싱된 핸들 개수에 의존하지 않도록
지도 표시 여부만 확인하며, 모드 진입 시 현재 generation을 다시 동기화한다. 따라서
평상시 지도 상태 동기화 tick에서 정점 핸들 payload를 반복 파싱하지 않는다.

CRS 전체 검색은 원본 catalog을 재사용하되, 대화상자 종료 시 검색 결과 배열과 debounce
timer를 해제한다. 대화상자를 다시 열면 현재 프로젝트 CRS와 빈 검색어를 기준으로
결과를 다시 만든다.

네이티브 map metadata bridge도 동일 payload를 다시 받으면 문자열을 다시 보관하거나
QML generation을 올리지 않는다. 실제 CRS/extent/view가 바뀐 경우에만 QML 동기화가
발생하도록 해 작업공간 복원과 반복적인 지도 상태 갱신의 불필요한 복사를 줄인다.

DXF 내보내기 설정의 레이어별 옵션 모델도 대화상자가 닫히면 비운다. 사용자가 확정한
export plan은 별도 JSON으로 보존되며, 다음 설정 화면을 열 때 현재 레이어 트리에서
옵션을 다시 만든다.

속성 대화상자는 닫힐 때 행뿐 아니라 현재 페이지의 열 배열과 페이지 메타데이터도
해제한다. 레이어 설정에서 사용하는 field hints는 유지해 레이블/Lua 편집기의 UX를
깨뜨리지 않으며, 속성 대화상자를 다시 열면 현재 payload로 복원한다.

레이블 publish 단계에서는 전체 레이블 배열을 먼저 복사하지 않고, 현재 viewport와
축척 조건을 잠금 상태에서 선필터링한 뒤 필요한 후보만 후속 declutter/sampling 단계로
넘긴다. 화면 밖 레이블이 많은 레이어의 임시 slice allocation과 복사 비용을 줄이되,
유효하지 않은 viewport에서는 기존 fallback 동작을 유지한다.

레이어 visibility도 별도 map으로 복제하지 않고 동일한 runtime lock 안에서 직접 조회해,
레이블 갱신마다 발생하던 또 하나의 임시 map allocation을 제거했다.

read-only viewport의 현재 chunk key 집합이 이전과 같을 때는 hit/label snapshot을
재구성하지 않고 그대로 재사용한다. 실제 chunk 집합이 바뀌거나 숨김 상태로 전환될
때만 offscreen 데이터를 제거하고 snapshot을 다시 만든다.

viewport refresh의 hidden-layer 정보도 전체 visibility map을 복제하지 않고 hidden
layer 이름 slice와 전체 layer count만 snapshot한다. 따라서 표시 레이어만 있는 일반
화면 갱신에서는 map allocation을 만들지 않는다.

레이블 publish 결과에는 원본 label revision과 viewport 상태를 함께 캐시한다. 렌더
완료 전후에 동일 상태가 반복되면 viewport 필터·충돌 제거·JSON 직렬화를 생략하고,
chunk 재구성·레이어 스타일 변경·runtime 교체 시에만 캐시를 무효화한다.

read-only snapshot의 label 중복 제거용 map도 실제 label 후보가 있는 경우에만 생성해,
레이블을 사용하지 않는 geometry-only 레이어의 재구성에서는 map allocation을 만들지
않는다.

progressive read-only rendering에서 새 geometry-only chunk가 추가될 때는 기존 hit
snapshot에 feature를 append한다. chunk 교체·삭제 또는 label 후보 추가처럼 전체 정합성
검사가 필요한 경우에만 snapshot을 재구성해, 여러 chunk가 순차 완료될 때의 반복 복사
비용을 줄인다.

coarse overview chunk는 선택·편집용 feature ID/name을 유지하지 않으므로, detail chunk에서만
`featureNames` map을 생성한다. overview 렌더링의 피처 수에 비례한 불필요한 map allocation을
제거한다.

PostGIS처럼 원본 feature ID를 보존하고 attribute reader가 원본 저장소를 직접 조회하는
경로에서는 `windowFeatureNames` 캐시 자체도 만들지 않는다.

속성 대화상자가 닫히면 QML 행 모델뿐 아니라 native bridge가 보유한 attribute JSON도
`[]`로 되돌린다. 닫힌 뒤 늦게 도착한 비동기 페이지도 표시 상태를 확인해 다음 bridge
tick에서 해제하므로, 최대 16MiB payload와 QML 디코딩 사본이 대화상자 밖에 남지 않는다.
다시 열 때는 현재 레이어의 첫 페이지를 재요청해 해제에 따른 데이터 복구 지연이나 빈
속성 화면을 방지한다.
Go 쪽의 페이지 캐시도 같은 visibility 신호를 받아 닫힌 동안 비우며, reopen 시에는
필요한 첫 페이지만 다시 읽는다.

이 변경 후 Apple M3에서 100만 포인트 GeoJSON benchmark를 `-benchtime=1x`로 실행한
결과는 첫 viewport 17.64ms, process peak RSS 172.0MiB, retained Go heap 50.35MiB,
visible features 64, visible payload 4.25KiB였다. 단일 실행 수치이므로 하드웨어나
모든 데이터셋의 성능 보증으로 해석하지 않는다.

후속 `windowFeatureNames` 캐시 생략 변경 후 동일 benchmark를 다시 실행한 단일 측정은
첫 viewport 21.04ms, process peak RSS 171.8MiB, retained Go heap 50.34MiB,
visible features 64, visible payload 4.25KiB, 192,467,952 B/op 및 3,001,851
allocs/op였다. 실행 간 시간 편차가 있으므로 캐시 생략의 시간 개선으로 해석하지 않고,
메모리 상한과 기능 결과가 유지된 근거로만 사용한다.

SHP를 포함한 read-only viewport에서 Lua 표시 필터가 설정되면 축소 지도에서도
속성 필드를 읽는다. 필터 판정은 도형·레이블·hit target 생성보다 먼저 수행한다.
필터에서 제외된 피처의 feature slice 꼬리를 비워 임시 window가 보유하던 WKB와
속성 map 참조를 해제한다. 축소 지도 샘플도 별도 작은 slice에 복사해 선택되지 않은
원본 window 전체를 붙잡지 않는다. 원본 파일과 프로젝트 데이터는 필터로 변경되지
않으며, 화면을 벗어난 chunk의 hit/label/name 캐시는 기존 viewport 수명 규칙에 따라
제거한다. 필터 평가는 현재 창 조회 뒤에 수행되므로 일시적인 읽기 한도는 원본 창의
피처 수와 크기를 기준으로 적용된다.
