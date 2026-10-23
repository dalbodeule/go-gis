#!/bin/sh
set -eu

docker compose up -d --wait
cleanup() {
	docker compose down
}
trap cleanup EXIT INT TERM

GOGIS_TEST_POSTGIS_DSN='postgres://gogis:gogis@localhost:55432/gogis?sslmode=disable' \
	go test ./drivers/postgis -run TestPostGISIntegrationSaveLayerRollbackPreservesSnapshot -count=1

if command -v pkg-config >/dev/null 2>&1 && pkg-config --exists Qt6Core Qt6Quick Qt6Qml; then
	GOGIS_TEST_POSTGIS_DSN='postgres://gogis:gogis@localhost:55432/gogis?sslmode=disable' \
		CGO_CXXFLAGS="${CGO_CXXFLAGS:-} -std=c++17" \
		go test -tags 'qt native' ./cmd/gis-desktop \
			-run '^TestPostGISDesktopRuntimePersistsRecordAddAndDelete$' -count=1
fi
