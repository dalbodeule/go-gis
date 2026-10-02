#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GOCACHE_DIR="${GOCACHE:-${TMPDIR:-/tmp}/gogis-go-build}"
export GOCACHE="${GOCACHE_DIR}"

cd "${ROOT_DIR}"

echo "== repository required files =="
test -f LICENSE
test -f agents.md
test -f testdata/sample.geojson

echo "== go test =="
go test ./...

echo "== go vet =="
go vet ./...

echo "== race test =="
go test -race ./...

echo "== native test =="
go test -tags native ./...

echo "== native race test =="
go test -race -tags native ./...

echo "== native build =="
./scripts/build.sh all-native

echo "== nested GODAL vet =="
(cd third_party/godal && go vet ./...)

echo "== nested GODAL test =="
(cd third_party/godal && go test ./... -skip '^TestVSIGCSNoAuth$')

echo "== nested GODAL race test =="
(cd third_party/godal && go test -race ./... -skip '^TestVSIGCSNoAuth$')

if command -v pkg-config >/dev/null 2>&1 && pkg-config --exists Qt6Core Qt6Quick Qt6Qml; then
	 echo "== Qt native test =="
	 CGO_CXXFLAGS="${CGO_CXXFLAGS:-} -std=c++17" go test -tags "qt native" ./cmd/gis-desktop
	 echo "== 1M-feature viewport stress =="
	 GOGIS_TEST_REPEATED_VIEWPORT_1M=1 CGO_CXXFLAGS="${CGO_CXXFLAGS:-} -std=c++17" \
		 go test -tags "qt native" ./cmd/gis-desktop \
		 -run '^TestWindowedReadOnlyRepeatedViewportMoves1M$' -count=1
	 if command -v qmltestrunner >/dev/null 2>&1; then
		 echo "== layer properties QML test =="
		 QT_QPA_PLATFORM=offscreen qmltestrunner \
			-import cmd/gis-desktop/qmltests \
			-input cmd/gis-desktop/qmltests -v1
	 else
		 echo "== layer properties QML test skipped: qmltestrunner not found =="
	 fi
else
	 echo "== Qt native test skipped: Qt6 pkg-config modules not found =="
fi

echo "== patch check =="
git diff --check

echo "Verification complete"
