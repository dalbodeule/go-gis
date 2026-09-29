#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="${ROOT_DIR}/build"
GOCACHE_DIR="${GOCACHE:-${TMPDIR:-/tmp}/gogis-go-build}"

usage() {
	cat <<'EOF'
Usage: scripts/build.sh [target]

Targets:
  cli       Build the default GoGIS CLI (default)
  native    Build the CLI with GDAL/PROJ/GEOS native drivers
  desktop   Build the Qt Quick desktop prototype
  desktop-native
            Build the Qt Quick desktop with GDAL/PROJ/GEOS input support
  all       Build cli and desktop
  all-native
            Build native CLI and native Qt Quick desktop
  clean     Remove generated files under build/
EOF
}

build_cli() {
	mkdir -p "${BUILD_DIR}"
	GOCACHE="${GOCACHE_DIR}" go build \
		-o "${BUILD_DIR}/gis-cli" \
		"${ROOT_DIR}/cmd/gis-cli"
}

build_native() {
	mkdir -p "${BUILD_DIR}"
	GOCACHE="${GOCACHE_DIR}" go build \
		-tags native \
		-o "${BUILD_DIR}/gis-cli-native" \
		"${ROOT_DIR}/cmd/gis-cli"
}

build_desktop() {
	mkdir -p "${BUILD_DIR}"
	# Qt 6 requires C++17. Preserve a caller-provided flag and append the
	# requirement when it is absent.
	local cgo_cxxflags="${CGO_CXXFLAGS:-}"
	case " ${cgo_cxxflags} " in
		*" -std=c++17 "*|*" -std=gnu++17 "*) ;;
		*) cgo_cxxflags="${cgo_cxxflags} -std=c++17" ;;
	esac
	CGO_CXXFLAGS="${cgo_cxxflags# }" \
		GOCACHE="${GOCACHE_DIR}" \
		go build -tags qt \
			-o "${BUILD_DIR}/gogis-desktop" \
			"${ROOT_DIR}/cmd/gis-desktop"
}

build_desktop_native() {
	mkdir -p "${BUILD_DIR}"
	local cgo_cxxflags="${CGO_CXXFLAGS:-}"
	case " ${cgo_cxxflags} " in
		*" -std=c++17 "*|*" -std=gnu++17 "*) ;;
		*) cgo_cxxflags="${cgo_cxxflags} -std=c++17" ;;
	esac
	CGO_CXXFLAGS="${cgo_cxxflags# }" \
		GOCACHE="${GOCACHE_DIR}" \
		go build -tags "qt native" \
			-o "${BUILD_DIR}/gogis-desktop-native" \
			"${ROOT_DIR}/cmd/gis-desktop"
}

target="${1:-cli}"
case "${target}" in
	cli)
		build_cli
		;;
	native)
		build_native
		;;
	desktop)
		build_desktop
		;;
	desktop-native)
		build_desktop_native
		;;
	all)
		build_cli
		build_desktop
		;;
	all-native)
		build_native
		build_desktop_native
		;;
	clean)
	rm -rf "${BUILD_DIR}"
		;;
	-h|--help|help)
		usage
		;;
	*)
		usage >&2
		exit 2
		;;
esac

if [[ "${target}" != "clean" && "${target}" != "-h" && "${target}" != "--help" && "${target}" != "help" ]]; then
	echo "Build complete: ${BUILD_DIR}"
fi
