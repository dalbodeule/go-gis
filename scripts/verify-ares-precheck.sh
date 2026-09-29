#!/usr/bin/env bash
set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/gogis-ares.XXXXXX")
trap 'rm -rf "$tmp_dir"' EXIT

cd "$repo_root"

utf8_dxf="$tmp_dir/sample-utf8.dxf"
cp949_dxf="$tmp_dir/sample-cp949.dxf"
cli="$tmp_dir/gogis-cli-native"

go build -tags native -o "$cli" ./cmd/gis-cli

"$cli" convert \
  --input testdata/sample.geojson \
  --output "$utf8_dxf" \
  --source-crs EPSG:4326 \
  --target-crs EPSG:4326 \
  --profile ares-utf8

"$cli" convert \
  --input testdata/sample.geojson \
  --output "$cp949_dxf" \
  --source-crs EPSG:4326 \
  --target-crs EPSG:4326 \
  --profile ares-cp949

grep -q 'AC1015' "$utf8_dxf"
grep -q 'UTF-8' "$utf8_dxf"
grep -q 'ANSI_949' "$cp949_dxf"

if command -v ogrinfo >/dev/null 2>&1; then
  ogrinfo -ro -al -so "$utf8_dxf" >/dev/null
  ogrinfo -ro -al -so "$cp949_dxf" >/dev/null
  echo "ARES precheck: DXF generation and GDAL round-trip passed"
else
  echo "ARES precheck: DXF generation passed; ogrinfo unavailable, round-trip skipped" >&2
fi

echo "Generated profiles: ares-utf8, ares-cp949"
echo "ARES Commander visual verification remains a manual Windows step."
