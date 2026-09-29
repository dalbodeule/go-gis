# Qt Quick prototype

This directory documents the Milestone B shell. The runnable entry point is
`cmd/gis-desktop` and is guarded by the `qt` build tag so ordinary Go builds do
not require a Qt installation.

The current prototype has two deliberate boundaries:

- `internal/render` owns viewport-independent chunk scheduling, generation
  invalidation, stale-result rejection, and cache reuse.
- `cmd/gis-desktop/qml/Main.qml` owns the initial desktop layout. The map
  canvas is backed by a custom `QQuickItem`/`QSGGeometryNode` bridge in
  `ui/qt/native`. The prototype now passes a scheduler-produced normalized XY
  vertex batch from Go into that node. Dragging pans the canvas and the mouse
  wheel changes zoom while incrementing the QML viewport generation. The Qt
  bridge now exposes that generation to Go, which advances the scheduler and
  batch store before requesting the next canvas update. Older requests are
  cancelled when a new viewport arrives, and `ChunkPlanner` limits each request
  to the visible chunks plus look-ahead margin.

## Local build

Install Qt 6 development packages and a CGO-compatible C/C++ toolchain first.
MIQT's Qt 6 binding is currently pinned to `github.com/mappu/miqt v0.14.0`.
Then run:

```sh
CGO_CXXFLAGS=-std=c++17 go run -tags qt ./cmd/gis-desktop
```

To open a real SHP or GeoPackage vector layer in the native data-backed
prototype, build with the native tag and pass `--input` (and optionally
`--layer`):

```sh
./scripts/build.sh desktop-native
./build/gogis-desktop-native --input testdata/sample.geojson --layer sample --save build/sample-edited.gpkg
```

native 입력에서는 필요할 때 `--source-crs EPSG:4326`으로 입력 CRS를
명시하고, `--target-crs EPSG:5179`로 모든 레이어를 공통 표시 CRS에 맞출 수
있다. CRS를 명시하지 않으면 첫 번째 CRS가 공통 표시 CRS로 사용된다.

The first native UI slice preserves the loaded layer name and renders normalized
Point/LineString/Polygon WKT geometry through the same chunk scheduler and
scene-graph bridge as the demo. All layers returned by the dataset are loaded
into the project, and the layer tree and attribute table are populated from the
loaded project read model. Clicking a layer selects it as the active attribute
table source, while the checkbox controls visibility independently.

When `--save` is provided, an edit Commit writes the loaded layer to the new
GeoPackage or Shapefile path. For a multi-layer dataset, the currently selected
layer is written to the destination. The current writer intentionally does not
replace an existing destination.

On macOS, Homebrew's Qt installation must be visible to `pkg-config`; on Linux
install the Qt 6 Core, Gui, Quick, Qml, and QuickControls2 development packages;
on Windows use a Qt 6 MinGW/LLVM toolchain compatible with CGO. The exact
platform setup is tracked in `docs/build.md`.
