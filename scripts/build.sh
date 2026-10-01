#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
BUILD_DIR="${ROOT_DIR}/build"
GOCACHE_DIR="${GOCACHE:-${TMPDIR:-/tmp}/gogis-go-build}"

ensure_build_dir() {
	if [[ -L "${BUILD_DIR}" ]]; then
		echo "Refusing to use symlinked build directory: ${BUILD_DIR}" >&2
		return 1
	fi
	if [[ -e "${BUILD_DIR}" && ! -d "${BUILD_DIR}" ]]; then
		echo "Build path exists but is not a directory: ${BUILD_DIR}" >&2
		return 1
	fi
	mkdir -p -- "${BUILD_DIR}"
	local resolved_dir
	resolved_dir="$(cd "${BUILD_DIR}" && pwd -P)"
	if [[ "${resolved_dir}" != "${BUILD_DIR}" ]]; then
		echo "Refusing unexpected build directory target: ${resolved_dir}" >&2
		return 1
	fi
}

clean_build_dir() {
	if [[ -L "${BUILD_DIR}" ]]; then
		echo "Refusing to remove symlinked build directory: ${BUILD_DIR}" >&2
		return 1
	fi
	if [[ ! -e "${BUILD_DIR}" ]]; then
		return 0
	fi
	if [[ ! -d "${BUILD_DIR}" ]]; then
		echo "Refusing to remove non-directory build path: ${BUILD_DIR}" >&2
		return 1
	fi
	local resolved_dir symlink_path
	resolved_dir="$(cd "${BUILD_DIR}" && pwd -P)"
	if [[ "${resolved_dir}" != "${BUILD_DIR}" ]]; then
		echo "Refusing to remove unexpected build directory target: ${resolved_dir}" >&2
		return 1
	fi
	symlink_path="$(find "${BUILD_DIR}" -type l -print -quit)"
	if [[ -n "${symlink_path}" ]]; then
		echo "Refusing to clean build directory containing symlink: ${symlink_path}" >&2
		return 1
	fi
	rm -rf -- "${BUILD_DIR}"
}

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
	ensure_build_dir
	GOCACHE="${GOCACHE_DIR}" go build \
		-o "${BUILD_DIR}/gis-cli" \
		"${ROOT_DIR}/cmd/gis-cli"
}

build_native() {
	ensure_build_dir
	GOCACHE="${GOCACHE_DIR}" go build \
		-tags native \
		-o "${BUILD_DIR}/gis-cli-native" \
		"${ROOT_DIR}/cmd/gis-cli"
}

build_desktop() {
	ensure_build_dir
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
	ensure_build_dir
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
	clean_build_dir
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
