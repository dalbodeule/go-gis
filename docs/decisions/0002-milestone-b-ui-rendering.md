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
- `cmd/gis-desktop/qml/Main.qml`: 레이어 목록·지도 캔버스·속성 패널의 Qt
  Quick 셸을 제공한다.
- `cmd/gis-desktop/main_qt.go`: QML을 embed하고 MIQT의
  `QQmlApplicationEngine`으로 로드한다. `qt` build tag가 없으면 컴파일되지
  않는다.

다음 구현 단계는 `QQuickItem` 기반 custom scene-graph item을 추가해
`Scheduler`의 immutable vertex batch를 `QSGGeometryNode`에 연결하는 것이다.

## 반드시 수행할 수직 벤치마크

같은 Go core/query 결과를 두 UI 후보에 공급하거나, 우선 Qt Quick 프로토타입과 Wails 대조군을 만든다.

- 데이터: 10만·100만 point/line/polygon, 실제 CRS 포함
- 동작: 연속 패닝, 10배 줌, 선택 사각형, 피처 속성 수정
- 측정: 첫 표시 시간, steady-state FPS, frame p50/p95, CPU, RSS, bridge bytes, stale-result discard 수
- 합격 기준 초안: 연속 패닝 중 UI thread block 16ms 초과 없음, stale 작업이 화면을 덮어쓰지 않음, 100만 feature 전체 geometry를 매 프레임 직렬화하지 않음

최종 UI 확정은 이 수치와 Windows/macOS/Linux 실제 빌드 결과를 근거로 별도 ADR에서 결정한다.

## 참고 문서

- [Qt QGraphicsView viewport update modes](https://doc.qt.io/qt-6/qgraphicsview.html)
- [Qt Quick Scene Graph](https://doc.qt.io/qt-6/qtquick-visualcanvas-scenegraph.html)
- [Qt graphics and RHI](https://doc.qt.io/qt-6/topics-graphics.html)
- [MIQT README and platform/build notes](https://github.com/mappu/miqt)
- [Wails architecture](https://v3.wails.io/concepts/architecture/)
- [Wails installation and Linux WebKit requirements](https://v3.wails.io/quick-start/installation/)
- [Gio](https://gioui.org/)
