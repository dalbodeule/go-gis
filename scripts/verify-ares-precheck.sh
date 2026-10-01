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

"$cli" label \
  --input testdata/sample.geojson \
  --field name \
  --rotation-field angle \
  --height 2.5 \
  --style Korean \
  --output "$utf8_dxf" \
  --profile ares-utf8

"$cli" label \
  --input testdata/sample.geojson \
  --field name \
  --rotation-field angle \
  --height 2.5 \
  --style Korean \
  --output "$cp949_dxf" \
  --profile ares-cp949

grep -q 'AC1015' "$utf8_dxf"
grep -q 'UTF-8' "$utf8_dxf"
grep -q 'ANSI_949' "$cp949_dxf"
test "$(grep -c '^TEXT$' "$utf8_dxf")" -eq 2
test "$(grep -c '^TEXT$' "$cp949_dxf")" -eq 2
test "$(grep -c '^50$' "$utf8_dxf")" -eq 1
test "$(grep -c '^50$' "$cp949_dxf")" -eq 1
awk 'previous == "50" && $0 == "30" { found = 1 } { previous = $0 } END { exit !found }' "$utf8_dxf"
awk 'previous == "50" && $0 == "30" { found = 1 } { previous = $0 } END { exit !found }' "$cp949_dxf"
grep -q '한글 도로' "$utf8_dxf"
iconv -f CP949 -t UTF-8 "$cp949_dxf" | grep -q '한글 도로'

if command -v ogrinfo >/dev/null 2>&1; then
  ogrinfo -ro -al -so "$utf8_dxf" >/dev/null
  ogrinfo -ro -al -so "$cp949_dxf" >/dev/null
  echo "ARES precheck: DXF generation and GDAL round-trip passed"
else
  echo "ARES precheck: DXF generation passed; ogrinfo unavailable, round-trip skipped" >&2
fi

echo "Generated profiles: ares-utf8, ares-cp949"
echo "ARES Commander visual verification remains a manual Windows step."
