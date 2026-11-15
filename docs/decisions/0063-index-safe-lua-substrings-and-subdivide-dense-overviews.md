# Index safe Lua substring filters and subdivide dense overviews

## Context

The local Gongju workspace contains three SHP layers. Its building and survey-point display rules use `gogis.contains(feature.FIELD, '44150')`. The existing source-side filter index recognized only equality and prefix comparisons, so the renderer repeatedly decoded features that Lua would hide. A distant point also expands the raw project extent to millions of map units, making the preferred filtered fit important.

After indexing the filter, a dense building window could still exceed the 32 MiB per-window payload limit. The overview path skipped exact subdivision for all SHP sources and retried the entire viewport with generalized polygon boundaries. On this workspace that removed configured building fills even though most chunks were within budget.

## Decision

Recognize only a single literal `gogis.contains(feature.FIELD, 'digits')` call as a source-side substring prefilter. The field must be text. Restricting the literal to digits keeps the OGR `LIKE '%digits%'` expression unambiguous; the original Lua rule remains authoritative for every accepted feature. The filter index stores one bit per source feature and an accepted envelope, so hidden features can be skipped before WKB and attributes are copied.

Use that accepted-ID index during overview window reads as well as detail reads. When an indexed window is overfull, subdivide it with the same indexed reader and deduplicate returned features. Unfiltered sources at or below the default 500,000-feature viewport limit may also subdivide exactly at overview scale. Larger sources retain the bounded sampled-overview fallback. Existing per-window, merged-payload, viewport, and render-vertex limits still apply.

## Verification

Unit tests cover parser rejection of complex expressions, substring index membership, and equality between indexed and ordinary filtered windows. With the local Gongju workspace loaded from outside the repository, 104,684 of 1,000,000 building features and 13,397 of 153,803 survey points enter the accepted sets. The saved viewport finishes with about 4.22 million vertices, including about 1.00 million building fill vertices, without a generalized-boundary retry. Five consecutive dense detail views finish with building fills, including one with 8.72 million vertices and 991,482 building fill vertices. Moving from zoom 256 to 512 releases offscreen windows: retained payload falls from about 247 MiB to 88 MiB. The heaviest tested detail view retains about 313 MiB, within the 512 MiB viewport payload limit. Warm single-machine runs take about 9 seconds for the saved viewport and about 7 seconds for the heaviest detail view; these are observations, not fixed test thresholds. One run stalled in GDAL `GetNextFeature` long enough to exceed the 60-second render wait, while two subsequent complete runs finished in about 29 seconds each. The intermittent read latency remains a performance risk; the stacks did not indicate a Go lock deadlock or retained offscreen payload.
