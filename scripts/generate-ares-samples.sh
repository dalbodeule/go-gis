#!/usr/bin/env bash

set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/gogis-ares-samples.XXXXXX")
trap 'rm -rf "$tmp_dir"' EXIT

cd "$repo_root"
cli="$tmp_dir/gogis-cli-native"
output_dir="$repo_root/testdata/ares"
mkdir -p "$output_dir"
go build -tags native -o "$cli" ./cmd/gis-cli

"$cli" label \
  --input testdata/sample.geojson \
  --field name \
  --rotation-field angle \
  --height 2.5 \
  --style Korean \
  --output "$output_dir/sample-utf8.dxf" \
  --profile ares-utf8

"$cli" label \
  --input testdata/sample.geojson \
  --field name \
  --rotation-field angle \
  --height 2.5 \
  --style Korean \
  --output "$output_dir/sample-cp949.dxf" \
  --profile ares-cp949

echo "Generated ARES samples in $output_dir"
