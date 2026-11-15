# Sampled overview for overfull OGR windows

## Context

At ten million features, an exact viewport window can contain more than the 20,000-feature or 32 MiB per-query budget. Spatial subdivision helps when features are spread out, but fails when many polygons overlap the same small location. The previous behavior stopped after bounded queries and left that tile empty with a safety error.

## Decision

Allow one automatic retry of a read-only viewport as an explicitly approximate overview when an exact window exceeds its feature or subdivision limit. In that forced overview, SHP and GeoPackage sessions read at most 2,000 matching features or 8 MiB from each indexed spatial result. The reader stops before decoding any further feature and avoids asking OGR for the full result count. Existing display-rule evaluation and accepted-FID prefilter still apply to the sampled prefix. The overview is labeled approximate in render status; exact feature selection and exports continue to use the original source and exact paths. A failed sampled query falls back to the existing bounded exact behavior.

This protects the UI and Go heap from a pathological overlap without claiming complete coverage. The first matching source features may not represent all areas of a large window. Format-specific sampled paths for streamed GeoJSON and PostGIS were added later in decisions 0055 and 0056.

## Verification

Use a temporary SHP with 20,001 overlapping polygons, exceeding the exact per-window cap, and verify the forced overview produces bounded vertices without retaining all source features or selection names. Check sampled GDAL window feature and byte limits, one-retry policy, full native tests, `go vet`, and QML tests.
