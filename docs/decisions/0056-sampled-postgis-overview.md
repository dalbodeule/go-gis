# Bounded PostGIS overview retry

## Context

The read-only desktop viewport recognizes GDAL feature and payload limit errors for spatial subdivision. PostGIS uses different limit messages, so a dense table could stop at the first overfull window. When overlapping geometries remain overfull after subdivision, the SHP/GeoJSON approximate retry cannot use the PostGIS store.

## Decision

Recognize PostGIS viewport feature and byte limit errors as subdivision candidates. If subdivision still exceeds its safety limits, allow one explicitly approximate overview retry for PostGIS tables without a Lua display rule. The store queries at most 2,001 rows and retains no more than 2,000 decoded features or 8 MiB of estimated payload for each sampled window. A single feature larger than the budget still returns an error. Exact reads, exports, and selection continue to use their original paths.

The sample is ordered by source ID and can omit later spatial clusters. A table with a Lua display rule keeps the exact bounded path until the PostGIS sampler supports predicate evaluation. No schema or native dependency changes are required.

## Verification

Check PostGIS error classification and sampler eligibility without a database. The existing optional live PostGIS integration test reads a one-feature sample when `GOGIS_TEST_POSTGIS_DSN` is configured. Run Go tests, native desktop tests, vet, QML tests, and diff checks. A live dense PostGIS database and GUI frame test remain needed to quantify performance at tens of millions of rows.
