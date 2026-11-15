# Enforce PostGIS viewport payload budget while reading

## Context

The PostGIS viewport queried at most 20,001 rows but previously decoded the complete bounded result under the driver's 128 MiB snapshot cap before applying the desktop's 32 MiB window budget. Dense rows could therefore allocate far more transient geometry and properties than the viewport allowed. The error still reached the renderer, but too late to protect peak memory and latency.

## Decision

Count the same WKT, JSON, and fixed per-feature bytes inside the row reader before appending each feature. Stop and return the existing viewport byte-limit error as soon as the next row would exceed the requested window budget. Keep full-layer and attribute-page reads on their existing 128 MiB bound. The approximate overview path retains its separate 8 MiB decoded sample budget.

Database rows are closed by the caller on every error or cancellation. This change does not alter the PostGIS query's CRS, ordering, feature-count limit, or export path.

## Verification

A deterministic `database/sql` test driver supplies 100 matching rows and counts `Next` calls. A 400-byte viewport limit must fail after at most two rows, proving the driver does not decode the rest. Run Go and native Qt tests, vet, and diff checks. The optional live PostGIS test still requires `GOGIS_TEST_POSTGIS_DSN`.
