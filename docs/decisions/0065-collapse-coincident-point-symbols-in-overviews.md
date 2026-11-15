# Collapse coincident point symbols in overviews

## Context

A synthetic indexed SHP with 10,000,001 point features rendered 475,392 overview vertices across 1,024 tiles. Each grid coordinate had 1,000 identical source points, so most overview vertices drew the same symbol repeatedly. The exact detail and attribute page paths must continue to expose all rows.

## Decision

For point layers without enabled labels, collapse identical WKB geometries within each overview window after overview sampling. Keep the first feature for display and clear discarded feature references from the reused window slice. Do not apply this to exact detail, attribute pages, or labeled point layers. Use the layer's source geometry type in the read-only metadata so UI and rendering decisions agree.

## Verification

The same 10,000,001-feature SHP still occupied all 256 spatial diagnostic cells and returned exact detail in two distant viewports. Overview vertices fell from 475,392 to 20,000; process peak RSS in these runs fell from about 335 MiB to about 297 MiB. Initial overview time remained about 7–8 seconds, so the measured gain is draw and memory work rather than source-query time. The last attribute page returned 128 rows from the original 10,000,001. A separate 10,000,001-feature GeoJSONSeq with a distant point loaded in about 7.4 seconds and completed its viewport in about 76 ms with roughly 232 MiB process peak RSS. These are single-machine benchmark observations, not fixed performance thresholds.
