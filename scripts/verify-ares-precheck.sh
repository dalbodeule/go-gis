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

go run -tags native ./scripts/generate-ares-project-samples.go "$tmp_dir"
project_utf8_dxf="$tmp_dir/sample-project-utf8.dxf"
project_cp949_dxf="$tmp_dir/sample-project-cp949.dxf"

grep -q 'AC1021' "$utf8_dxf"
grep -q 'AC1015' "$cp949_dxf"
grep -q 'UTF-8' "$utf8_dxf"
grep -q 'ANSI_949' "$cp949_dxf"
test "$(grep -c '^TEXT$' "$utf8_dxf")" -eq 2
test "$(grep -c '^TEXT$' "$cp949_dxf")" -eq 2
test "$(grep -c '^SOLID$' "$project_utf8_dxf")" -gt 0
test "$(grep -c '^SOLID$' "$project_cp949_dxf")" -gt 0
for dxf in "$project_utf8_dxf" "$project_cp949_dxf"; do
  if grep -q '^HATCH$' "$dxf"; then
    echo "unexpected HATCH in project fixture: $dxf" >&2
    exit 1
  fi
done
grep -q '^\$PDMODE$' "$project_utf8_dxf"
grep -q '^\$PDSIZE$' "$project_utf8_dxf"
grep -q '^malgun.ttf$' "$project_utf8_dxf"
for dxf in "$utf8_dxf" "$cp949_dxf"; do
  awk 'previous == "0" { entity = $0 }
       previous == "50" && entity == "TEXT" { count++; if ($0 == "30") found = 1 }
       { previous = $0 }
       END { exit !(count == 1 && found) }' "$dxf"
done
grep -q '한글 도로' "$utf8_dxf"
iconv -f CP949 -t UTF-8 "$cp949_dxf" | grep -q '한글 도로'

if command -v ogrinfo >/dev/null 2>&1; then
  ogrinfo --config DXF_ENCODING UTF-8 -ro -al -so "$utf8_dxf" >/dev/null
  ogrinfo -ro -al -so "$cp949_dxf" >/dev/null
  ogrinfo --config DXF_ENCODING UTF-8 -ro -al -q "$utf8_dxf" | grep -F 'Text (String) = 한글 도로' >/dev/null
  ogrinfo -ro -al -q "$cp949_dxf" | grep -F 'Text (String) = 한글 도로' >/dev/null
  for layer in '0-연속지적도' '0-지적도근점' '0-건물'; do
    ogrinfo --config DXF_ENCODING UTF-8 -ro -al -q "$project_utf8_dxf" | grep -F "Layer (String) = $layer" >/dev/null
    ogrinfo -ro -al -q "$project_cp949_dxf" | grep -F "Layer (String) = $layer" >/dev/null
  done
  echo "ARES precheck: DXF generation and GDAL round-trip passed"
else
  echo "ARES precheck: DXF generation passed; ogrinfo unavailable, round-trip skipped" >&2
fi

echo "Generated profiles: ares-utf8, ares-cp949"
echo "ARES Commander visual verification remains a manual Windows step."
