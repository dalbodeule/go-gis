#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
BUILD_DIR="${ROOT_DIR}/build"
VERSION="0.1.0-dev"
ARCH="$(uname -m)"
MAKE_APPIMAGE=0

usage() {
	cat <<'EOF'
Usage: scripts/package_linux.sh [--version VERSION] [--arch amd64|arm64] [--appimage]

Build a native Qt/GDAL/PROJ/GEOS Linux AppDir, portable tar.gz and .deb.
--appimage additionally requires linuxdeploy and linuxdeploy-plugin-qt.
EOF
}

while (($#)); do
	case "$1" in
		--version) VERSION="${2:?missing value for --version}"; shift 2 ;;
		--arch) ARCH="${2:?missing value for --arch}"; shift 2 ;;
		--appimage) MAKE_APPIMAGE=1; shift ;;
		-h|--help) usage; exit 0 ;;
		*) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
	esac
done

case "${ARCH}" in
	x86_64|amd64) ARCH=amd64; DEB_ARCH=amd64 ;;
	aarch64|arm64) ARCH=arm64; DEB_ARCH=arm64 ;;
	*) echo "unsupported Linux architecture: ${ARCH}" >&2; exit 2 ;;
esac
[[ "$(uname -s)" == Linux ]] || { echo "run this script on Linux" >&2; exit 1; }
case "$(uname -m)" in
	x86_64|amd64) [[ "${ARCH}" == amd64 ]] || { echo "native packaging cannot cross-build" >&2; exit 1; } ;;
	aarch64|arm64) [[ "${ARCH}" == arm64 ]] || { echo "native packaging cannot cross-build" >&2; exit 1; } ;;
	*) echo "unsupported host architecture: $(uname -m)" >&2; exit 1 ;;
esac

for command in go pkg-config dpkg-deb tar; do
	command -v "${command}" >/dev/null || { echo "required command not found: ${command}" >&2; exit 1; }
done
for module in gdal proj geos Qt6Core Qt6Quick Qt6Qml; do
	pkg-config --exists "${module}" || { echo "pkg-config module not found: ${module}" >&2; exit 1; }
done

slug="${VERSION//[^A-Za-z0-9.-]/-}"
PACKAGE_ROOT="${BUILD_DIR}/packages/linux/${ARCH}"
APPDIR="${PACKAGE_ROOT}/GoGIS-${slug}"
TARBALL="${PACKAGE_ROOT}/GoGIS-${slug}-linux-${ARCH}.tar.gz"
DEB="${PACKAGE_ROOT}/gogis_${slug}_${DEB_ARCH}.deb"
if [[ -e "${APPDIR}" || -e "${TARBALL}" || -e "${DEB}" ]]; then
	echo "package output already exists; choose another --version or move it manually" >&2
	exit 1
fi
mkdir -p "${PACKAGE_ROOT}" "${APPDIR}/usr/bin" "${APPDIR}/usr/share/applications" "${APPDIR}/DEBIAN"

export CGO_ENABLED=1
export CGO_CXXFLAGS="${CGO_CXXFLAGS:-} -std=c++17"
export GOGIS_VERSION="${VERSION}"
"${ROOT_DIR}/scripts/build.sh" desktop-native
cp "${BUILD_DIR}/gogis-desktop-native" "${APPDIR}/usr/bin/GoGIS"
chmod 0755 "${APPDIR}/usr/bin/GoGIS"

cat > "${APPDIR}/usr/share/applications/org.gogis.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=GoGIS
Comment=Desktop GIS application
Exec=GoGIS
Terminal=false
Categories=Education;Science;Geography;
EOF

mkdir -p "${APPDIR}/resources"
for resource in gdal proj; do
	source="$(pkg-config --variable=prefix "${resource}")/share/${resource}"
	[[ -d "${source}" ]] || { echo "resource directory not found: ${source}" >&2; exit 1; }
	cp -a "${source}" "${APPDIR}/resources/${resource}"
done
plugin_source="$(pkg-config --variable=libdir gdal)/gdalplugins"
mkdir -p "${APPDIR}/resources/gdalplugins"
if [[ -d "${plugin_source}" ]]; then cp -a "${plugin_source}/." "${APPDIR}/resources/gdalplugins/"; fi

if command -v linuxdeploy >/dev/null 2>&1; then
	export APPIMAGE_EXTRACT_AND_RUN="${APPIMAGE_EXTRACT_AND_RUN:-1}"
	linuxdeploy_args=(--appdir "${APPDIR}" --executable "${APPDIR}/usr/bin/GoGIS" \
		--desktop-file "${APPDIR}/usr/share/applications/org.gogis.desktop")
	if command -v linuxdeploy-plugin-qt >/dev/null 2>&1; then
		linuxdeploy_args+=(--plugin qt)
	fi
	linuxdeploy "${linuxdeploy_args[@]}"
else
	echo "linuxdeploy not found: AppDir is not a self-contained release yet" >&2
fi

cat > "${APPDIR}/BUILD-INFO.txt" <<EOF
GoGIS ${VERSION}
Target: linux/${ARCH}
Go: $(go version)
GDAL: $(pkg-config --modversion gdal)
PROJ: $(pkg-config --modversion proj)
GEOS: $(pkg-config --modversion geos)
Qt: $(pkg-config --modversion Qt6Core)
EOF

cat > "${APPDIR}/DEBIAN/control" <<EOF
Package: gogis
Version: ${slug}
Section: science
Priority: optional
Architecture: ${DEB_ARCH}
Maintainer: GoGIS contributors
Depends: libc6, libstdc++6
Description: GoGIS desktop GIS application
 Native Qt desktop GIS application with GDAL, PROJ and GEOS support.
EOF
dpkg-deb --root-owner-group --build "${APPDIR}" "${DEB}" >/dev/null
tar -C "${PACKAGE_ROOT}" -czf "${TARBALL}" "GoGIS-${slug}"

if ((MAKE_APPIMAGE)); then
	command -v linuxdeploy >/dev/null 2>&1 || { echo "--appimage requires linuxdeploy" >&2; exit 1; }
	linuxdeploy --appdir "${APPDIR}" --output appimage
	find "${ROOT_DIR}" -maxdepth 1 -type f -name 'GoGIS*.AppImage' -exec mv -n {} "${PACKAGE_ROOT}/" \;
fi

echo "Portable directory: ${APPDIR}"
echo "Portable tarball: ${TARBALL}"
echo "Debian package: ${DEB}"
