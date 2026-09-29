# Qt Quick prototype

This directory documents the Milestone B shell. The runnable entry point is
`cmd/gis-desktop` and is guarded by the `qt` build tag so ordinary Go builds do
not require a Qt installation.

The current prototype has two deliberate boundaries:

- `internal/render` owns viewport-independent chunk scheduling, generation
  invalidation, stale-result rejection, and cache reuse.
- `cmd/gis-desktop/qml/Main.qml` owns the initial desktop layout. The map
  placeholder is the future insertion point for a custom `QQuickItem` whose
  scene-graph node will consume immutable chunk vertex batches.

## Local build

Install Qt 6 development packages and a CGO-compatible C/C++ toolchain first.
MIQT's Qt 6 binding is currently pinned to `github.com/mappu/miqt v0.14.0`.
Then run:

```sh
CGO_CXXFLAGS=-std=c++17 go run -tags qt ./cmd/gis-desktop
```

On macOS, Homebrew's Qt installation must be visible to `pkg-config`; on Linux
install the Qt 6 Core, Gui, Quick, Qml, and QuickControls2 development packages;
on Windows use a Qt 6 MinGW/LLVM toolchain compatible with CGO. The exact
platform setup is tracked in `docs/build.md`.
