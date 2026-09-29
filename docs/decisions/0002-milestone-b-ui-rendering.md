# ADR 0002: 마일스톤 B 지도 UI와 부분 렌더링

- 상태: Qt Quick 우선 프로토타입 채택
- 기준일: 2026-09-29
- 범위: 3개 OS의 벡터 지도 캔버스, 부분 갱신, 패닝/줌, 선택·편집 UX

## 결정

마일스톤 B의 첫 UI 프로토타입은 Qt 6 Quick Scene Graph와 Go용 MIQT 바인딩으로 만든다. UI 기술을 코어·명령·드라이버에 노출하지 않으며, 렌더러는 별도 `ui/render` 경계에 둔다.

권장 구조:

```text
Go GIS Core / Query Planner
        │ visible extent + style + generation
        ▼
Go Render Cache / Tile or Chunk Scheduler
        │ immutable vertex batches
        ▼
Qt Quick custom scene graph item
        │ QSGGeometryNode / GPU buffers
        ▼
Metal(macOS), Direct3D 11/12 또는 Vulkan(Windows), Vulkan/OpenGL(Linux)
```

화면 전체를 매 프레임 Go 객체로 다시 만들지 않는다. 화면 영역과 여유 경계만 조회하고, 레이어·스타일·CRS·뷰포트 세대가 바뀐 청크만 비동기로 재생성한다. GUI/render thread에는 immutable한 정점·색상·텍스처 버퍼만 전달한다.

## 후보 비교

| 후보 | 부분 렌더링 | GPU/스레드 | Go 개발 생산성 | 3개 OS 배포 | 주요 위험 |
| --- | --- | --- | --- | --- | --- |
| Qt Quick + MIQT | 매우 유리. scene graph 노드와 dirty update 경계를 직접 설계 | Qt RHI가 Metal/Vulkan/D3D/OpenGL로 추상화하고 threaded render loop를 제공 | 중간 | 중간 | MIQT가 2024년에 시작된 비교적 새로운 CGO 바인딩이며 Qt 개발 툴체인이 필요 |
| Wails + WebGL/WebGPU | 유리하지만 렌더 데이터는 브라우저 쪽에 상주해야 함 | WebView GPU에 의존. Windows WebView2, macOS WebKit, Linux WebKitGTK 차이 | 높음 | 중간 | Go↔JS bridge로 geometry를 매 프레임 보내면 병목; Linux WebKit 버전 편차 |
| Gio | 빠른 custom canvas를 Go만으로 구성 가능 | Go immediate-mode GUI와 GPU renderer | 중간 | 중간 | GIS용 고급 테이블/트리·접근성·Qt 수준의 데스크톱 위젯 생태계가 부족 |
| Qt Widgets/QGraphicsView | 기본 dirty-region 기능은 제공 | 성숙하지만 OpenGL viewport에서는 전체 갱신이 더 적합할 수 있음 | 중간 | 높음 | 대규모 feature를 개별 QGraphicsItem으로 만들면 item 관리 비용이 커짐 |

## 선택 근거

Qt의 `QGraphicsView`는 기본적으로 최소 viewport 갱신 모드를 제공하지만, OpenGL viewport는 전체 갱신이 선호될 수 있다. 따라서 GoGIS는 수십만 개 feature를 개별 `QGraphicsItem`으로 만들지 않고, Qt Quick scene graph의 배칭 가능한 geometry node를 사용한다.

Qt Quick은 scene graph를 UI 상태와 분리하고 많은 플랫폼에서 전용 render thread를 사용한다. Qt RHI는 Metal, Vulkan, Direct3D, OpenGL을 대상으로 하므로 3개 OS에서 동일한 렌더러 계약을 유지하기 좋다.

MIQT는 Qt 6.4+와 Qt Quick/QML을 지원하고 macOS ARM64, macOS x86_64, Windows x86_64, Linux x86_64/ARM64를 표에 명시한다. 다만 바인딩이 비교적 새롭고 CGO·Qt C++ 개발 환경이 필수이므로 반드시 작은 수직 프로토타입으로 검증한다.

Wails는 빠른 화면 구성과 Go↔웹 기술 결합에는 매력적이다. 그러나 WebView가 OS별로 다르고, Wails v3의 Linux 기본 요구사항은 GTK4/WebKitGTK 6.0이며 오래된 배포판은 별도 legacy tag가 필요하다. Wails를 선택할 경우에도 geometry와 GPU 버퍼를 JavaScript 쪽에 유지하고 bridge에는 명령·가시 영역·선택 결과만 보내야 한다.

## 렌더링 설계 규칙

1. 공간 인덱스와 화면 영역 조회는 Go worker에서 수행한다.
2. 뷰포트 변경은 generation 번호를 증가시키고, 오래된 결과는 render thread에 제출하지 않는다.
3. 청크 키는 `(layer, style, zoom bucket, tile/chunk extent, generation)`으로 구성한다.
4. 패닝은 기존 청크를 먼저 재사용하고 새로 노출된 경계만 조회·렌더링한다.
5. 줌 변경은 줌 버킷이 유지되는 동안 기존 버퍼를 재사용하고, 버킷이 바뀔 때만 재간략화한다.
6. 선택·편집은 전체 레이어 버퍼를 무효화하지 않고 영향을 받은 feature/chunk만 dirty 처리한다.
7. UI thread에서 GDAL/GEOS/PROJ 호출을 하지 않는다.

## 현재 프로토타입 상태

첫 수직 슬라이스의 기반은 다음과 같이 추가되었다.

- `internal/render.Scheduler`: 청크 캐시, viewport generation, 취소, 오래된
  결과 폐기를 UI 툴킷과 독립적으로 처리한다.
- `internal/render.BatchStore`: 현재 generation에 해당하는 vertex batch만
  적용하고 늦게 도착한 결과는 기존 화면을 유지한 채 폐기한다.
- `internal/render.ChunkPlanner`: viewport extent와 look-ahead margin으로
  필요한 청크 키만 계산하고 zoom bucket을 함께 지정한다.
- `internal/render.LayerVisibility`: 레이어 트리의 가시성 상태를 UI와
  독립적으로 보관하고 숨김·미등록 레이어의 chunk 요청을 제거한다.
- QML 레이어 체크박스는 `MapCanvas.visible`을 변경하고, Qt 브리지가 이를
  Go에 전달한다. 숨겨진 레이어는 새 요청을 만들지 않으며 기존 batch도
  제거한다.
- 지도 클릭은 현재 데모 피처 선택 상태를 갱신하고 속성 패널에 레이어·피처·
  편집 가능 상태를 표시한다. 실제 geometry hit-test와 core feature ID
  연결을 위해 `internal/render.HitTest`가 line feature의 closest segment와
  tolerance를 계산하며, `ScreenPointToWorld`/`HitTestScreen`이 Qt 화면 좌표와
  world 좌표 및 픽셀 tolerance를 변환한다. QML 클릭은 native bridge의
  generation/좌표 snapshot을 거쳐 Go에서 hit-test되고 선택 결과가 다시
  QML 속성 패널로 전달된다.
- `cmd/gis-desktop/qml/Main.qml`: 레이어 목록·지도 캔버스·속성 패널의 Qt
  Quick 셸을 제공한다.
- `cmd/gis-desktop/main_qt.go`: QML을 embed하고 MIQT의
  `QQmlApplicationEngine`으로 로드한다. `qt` build tag가 없으면 컴파일되지
  않는다.
- `ui/qt/native`: MIQT에 없는 `QQuickItem`/`QSGGeometryNode` 경계를 Qt C++로
  얇게 감싸고 `GoGIS.MapCanvas` QML 타입으로 등록한다. 현재는 연결 검증용
  polyline을 그리고, Go 스케줄러가 만든 정규화 XY vertex batch를 C ABI로
  복사해 `QSGGeometryNode`에 공급한다.
- `Main.qml`의 지도 입력은 드래그 패닝과 휠 줌을 처리하며, 각 입력을
  viewport generation으로 증가시킨다. `MapCanvas.itemChange`가 이를 감지해
  C ABI polling 경계로 넘기고, Go가 `Scheduler.AdvanceGeneration` 및
  `BatchStore.BeginGeneration`을 호출한다.
- 새 viewport 요청이 시작되면 이전 chunk request context를 취소하고 최신
  generation 요청만 비동기로 적용한다. 취소를 무시하는 네이티브/외부 작업도
  `BatchStore`의 generation 검사에서 최종적으로 화면 반영이 차단된다.
- 하나의 visible extent 요청 안에서는 최대 4개 청크를 병렬 생성해 느린
  청크 하나가 전체 첫 표시를 막지 않도록 한다.
- `Scheduler.Stats()`로 요청 수, 실제 생성 수, cache hit, stale 폐기,
  취소 요청을 수집해 성능 기준을 정량화한다. planner/cache benchmark는
  `go test -bench ./internal/render`로 실행한다.
- `internal/presentation.AttributeTableModel`이 레이어 필드와 feature 값을
  UI 중립 read model로 고정한다. Qt adapter는 특정 `name` 필드에 의존하지
  않고 모든 명시·추론 컬럼을 표시하며, `RenderProgress`가 로딩/완료/취소
  상태 문자열을 동일한 형식으로 제공한다.
- QML layer delegate의 선택과 가시성은 별도 상태로 전달된다. 선택된 layer는
  native bridge를 통해 Go attribute read model을 갱신하고, 체크 상태는
  render visibility만 변경한다.
- `internal/render.LayerSource`는 normalized geometry를 0.25 world-unit
  grid cell에 분배한다. native data mode는 `ChunkPlanner`의 visible keys만
  요청하므로 전체 layer batch를 매 viewport 변경마다 복제하지 않는다.
- MultiLineString·MultiPolygon·GeometryCollection의 disjoint part는
  `HitFeature.Parts`로 보존해 render chunk와 hit-test가 서로 다른 component를
  가상의 선으로 연결하지 않는다.
- 선택 경로는 `internal/render.HitIndex`의 immutable uniform grid를 사용한다.
  native normalized data에서는 0.01 world-unit 셀을 사용해 클릭 tolerance 주변
  후보만 검사하며, geometry가 바뀌면 새 index를 만들어 교체한다. UI thread에서
  원본 feature 전체를 선형 순회하지 않으며, 숨긴 레이어는 후보 단계에서
  제외한다.

- `scripts/build.sh desktop-native`로 만든 바이너리에 `--input ...`을
  전달하면 GDAL layer를 `internal/render.LayerSource`로 정규화하고, 실제
  layer 이름·속성 table·선택 feature를 동적 QML layer tree에 전달한다.
  여러 layer는 첫 번째 유효 CRS를 표시 CRS로 삼고 PROJ로 변환한 다음,
  dataset 전체의 공통 extent를 기준으로 정규화하여 서로 다른 원본 좌표
  범위가 화면에서 잘못 겹치지 않게 한다. CRS가 없는 layer가 유효 CRS
  layer와 섞이면 로딩을 중단하고 오류를 보고한다. 기본 `desktop` target은
  외부 GIS 런타임 없이 데모 source를 유지한다.

다음 구현 단계는 대용량 multi-layer fixture의 성능 측정과 ARES Commander를
포함한 외부 포맷 호환성 검증이다.

## 반드시 수행할 수직 벤치마크

같은 Go core/query 결과를 두 UI 후보에 공급하거나, 우선 Qt Quick 프로토타입과 Wails 대조군을 만든다.

- 데이터: 10만·100만 point/line/polygon, 실제 CRS 포함
- 동작: 연속 패닝, 10배 줌, 선택 사각형, 피처 속성 수정
- 측정: 첫 표시 시간, steady-state FPS, frame p50/p95, CPU, RSS, bridge bytes, stale-result discard 수
- 합격 기준 초안: 연속 패닝 중 UI thread block 16ms 초과 없음, stale 작업이 화면을 덮어쓰지 않음, 100만 feature 전체 geometry를 매 프레임 직렬화하지 않음

최종 UI 확정은 이 수치와 Windows/macOS/Linux 실제 빌드 결과를 근거로 별도 ADR에서 결정한다.

현재 재현 가능한 기준 benchmark는 `go test -bench ./internal/render`의
`BenchmarkLayerSource100KLines`와 `BenchmarkLayerSource100KChunkBuild`이다.
이는 실제 파일 I/O가 아닌 geometry 정규화·immutable batch 생성 비용을
측정하므로, 이후 GDAL 화면 영역 조회 benchmark와 별도로 비교한다.
규모별 정규화와 multi-layer batch 비용은 다음 benchmark로 재현한다.

```sh
go test -run '^$' -bench 'BenchmarkLayerSourceScale|BenchmarkMultiLayerChunkBuild' \
  -benchtime=1x -benchmem ./internal/render
```

Apple M3에서 `-benchtime=200ms`로 측정한 정규화 비용은 10K 약 3.56ms,
100K 약 34.1ms, 1M 약 360ms였고, 4개 layer의 chunk build는 약 9.4µs였다.
이 수치는
합성 WKT 입력 기준이며 GDAL I/O와 화면 영역 질의 비용은 포함하지 않는다.
100K line source에서 선택 benchmark도 제공한다. Apple M3 기준 `-benchtime=200ms`
로 `BenchmarkHitTestLinear100K`는 약 0.89ms, `BenchmarkHitTestIndexed100K`는
약 0.0014ms(약 2.5KB 할당)였으며, 두 경로의 결과 동등성은 단위 테스트로
검증한다. 측정 환경은 macOS Darwin/arm64, Apple M3이며, 실행 시점의 CPU
부하와 Go 버전에 따라 값은 변할 수 있다.

## 참고 문서

- [Qt QGraphicsView viewport update modes](https://doc.qt.io/qt-6/qgraphicsview.html)
- [Qt Quick Scene Graph](https://doc.qt.io/qt-6/qtquick-visualcanvas-scenegraph.html)
- [Qt graphics and RHI](https://doc.qt.io/qt-6/topics-graphics.html)
- [MIQT README and platform/build notes](https://github.com/mappu/miqt)
- [Wails architecture](https://v3.wails.io/concepts/architecture/)
- [Wails installation and Linux WebKit requirements](https://v3.wails.io/quick-start/installation/)
- [Gio](https://gioui.org/)
