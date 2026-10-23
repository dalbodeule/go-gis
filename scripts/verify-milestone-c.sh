#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GOCACHE_DIR="${GOCACHE:-${TMPDIR:-/tmp}/gogis-go-build}"
GOMODCACHE_DIR="${GOMODCACHE:-${TMPDIR:-/tmp}/gogis-go-mod}"
export GOCACHE="${GOCACHE_DIR}"
export GOMODCACHE="${GOMODCACHE_DIR}"

cd "${ROOT_DIR}"

echo "== Go version and module graph =="
go version
go mod verify

echo "== shared command, CLI, Lua =="
go test ./...
go vet ./...

echo "== native CLI and PostGIS driver =="
CGO_CXXFLAGS="${CGO_CXXFLAGS:-} -std=c++17" go test -tags native ./cmd/gis-cli ./drivers/postgis

if command -v pkg-config >/dev/null 2>&1 && pkg-config --exists Qt6Core Qt6Quick Qt6Qml; then
	 echo "== native desktop and Qt bridge =="
	 CGO_CXXFLAGS="${CGO_CXXFLAGS:-} -std=c++17" go test -tags "qt native" ./cmd/gis-desktop ./ui/qt/native
	 if command -v qmltestrunner >/dev/null 2>&1; then
		 echo "== QML workflow =="
		 QT_QPA_PLATFORM=offscreen qmltestrunner \
			-import cmd/gis-desktop/qmltests \
			-input cmd/gis-desktop/qmltests -silent
	 else
		 echo "QML workflow skipped: qmltestrunner not found"
	 fi
else
	 echo "native desktop skipped: Qt6 pkg-config modules not found"
fi

if command -v docker >/dev/null 2>&1; then
	 echo "== live PostGIS rollback and desktop record workflow =="
	 ./scripts/test-postgis.sh
else
	 echo "live PostGIS skipped: Docker not found"
fi

if command -v govulncheck >/dev/null 2>&1; then
	 echo "== vulnerability scan =="
	 govulncheck ./...
else
	 echo "vulnerability scan skipped: govulncheck not found"
fi

echo "== patch check =="
git diff --check
echo "Milestone C verification complete"
