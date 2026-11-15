# Retry overfull overviews before spatial subdivision

## Context

An exact broad-scale viewport query may exceed its feature limit. Spatial subdivision can recover complete results when features are distributed, but it repeatedly decodes the same 20,000 source geometries when millions of features occupy one point or polygon. The read-only overview never retains selection geometry, and an explicitly approximate bounded retry is already available for SHP, GeoPackage, streamed GeoJSON, and PostGIS sources.

## Decision

At overview zoom only, make one exact root-window query. If it exceeds the existing feature or byte budget, request the existing one-time sampled overview retry immediately. Detail zooms continue to use bounded spatial subdivision for complete visible features. Sources without a compatible sampler keep the original subdivision behavior. Display rules are applied to a successful exact root window, and SHP's source-side filtered read remains the first attempt where available. Exports and feature selection continue to use exact source paths.

This reduces repeated decoding in a known overfull overview without increasing the per-window feature or payload budgets. A sampled overview is explicitly approximate and may omit later source features.

## Verification

Use the explicit 10,000,001-feature dense GeoJSONSeq benchmark with a second distant file. Check initial fit, exact-limit retry, bounded sampled vertices, the full viewport scheduler/publication path, peak RSS, and timing. On Apple M3 the overfull root reached the retry in about 56–70 ms versus about 408 ms after repeated subdivision; peak RSS fell from about 346 MiB to about 211–232 MiB across runs. The full synthetic viewport published 4 nonempty chunks with 4,000 vertices in about 75 ms.

An additional standalone Qt launch used a temporary 10,000,001-feature dense GeoJSONSeq file with `QT_QPA_PLATFORM=offscreen`, `QT_QUICK_BACKEND=software`, and `GOGIS_PERF=1`. The native bridge published 4,000 source vertices at 7,637 ms after load start; the Qt scene graph finished 12,000 expanded triangle vertices at 7,643 ms, across one node. The temporary input and executable were removed afterward. This verifies an actual offscreen scene-graph update, but not a hardware GPU frame, a dense polygon workload, or OS-specific performance. Run native Qt tests, Go tests, vet, QML tests, and diff checks.
