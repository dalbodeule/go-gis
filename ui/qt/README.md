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
Read-only layers keep low-zoom query cells at no more than 1/64 of the combined
extent. This bounds dense cadastral queries per cell and lets the renderer
publish early successful cells while later cells continue loading; per-window
and total visible-data safety limits still apply.
Visibility toggles also retain cached current-view chunks for hidden layers up
to a 1M-vertex aggregate budget, so a quick hide/show can reuse completed work
without pinning the full hidden viewport in memory.

The status bar translates load/render outcomes and visually separates errors,
cancellations, warnings, progress, and success; its busy indicator also covers
long saves. A separate sampler updates process memory once per second, including
during synchronous save work, and shows Go heap separately. The Logs button at the
far left of the footer opens a session-only, bounded viewer for captured stdout/stderr
and application errors.
On macOS/Linux, output is mirrored to the launching terminal while being captured;
Windows redirects Go and Win32 standard output handles to the viewer. The latest
500 messages and at most 512 KiB are retained in memory; individual lines are
limited to 16 KiB. Application and native renderer errors are also echoed to
stdout (native renderer errors remain on stderr as well). No log file is written
automatically. The process metric is
platform-specific (Linux current RSS, Windows working set, macOS CGO current RSS;
macOS without CGO uses peak RSS) and excludes GPU memory; it is diagnostic, not a
memory quota.
The native staging buffer releases storage for an empty viewport and shrinks after
a 4x-or-greater viewport payload reduction, while retaining capacity for ordinary
small frame fluctuations.

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
