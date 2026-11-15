# Bounded sampled overview for streamed GeoJSON

## Context

Opening multiple large files can produce a wide map extent, including a few coordinate outliers. The preferred initial fit already uses bounded geometry-envelope samples while retaining the full source extent for later navigation. At broad zoom levels, a dense GeoJSON or GeoJSONSeq polygon window can still exceed the exact viewport limit even after spatial subdivision, leaving the tile blank. The SHP and GeoPackage approximate retry cannot read the streaming GeoJSON index.

## Decision

Extend the explicitly approximate read-only viewport retry to streamed GeoJSON and GeoJSONSeq. The indexed reader stops after at most 2,000 features or 8 MiB of estimated decoded payload per window, including partial-index tail blocks. Exact reads keep their error-on-limit behavior. The sampled path scans index entries in source order and skips building a potentially large spatial candidate array for an already dense window. The source file stamp is checked before and after reading, including before geometry-envelope sampling for initial fit. Divide the runtime-wide per-feature index budget among the remaining input files and debit the entries actually retained, including when a file is larger than its allocated share. A session already indexed during preview is reused and debited by its retained entry count. If several files exhaust the shared budget, each remaining stream still builds bounded tail blocks from its first feature. Exact and sampled viewport reads, attribute pages, and individual feature lookups then seek to the relevant block instead of rescanning the whole file. Initial-fit sampling counts each tail block envelope once, so a single outlier-bearing block cannot be mistaken for many independent outliers.

For now, a GeoJSON layer with a Lua display rule keeps the exact bounded path because the stream sampler has no attribute-predicate hook. This avoids drawing hidden features in an approximate overview. The approximate result has no selection or export authority; exports and exact feature requests still use their existing source reads. The prefix may underrepresent later records or other spatial clusters.

## Verification

Check a GeoJSONSeq with both a deliberately short per-feature index and a zero-entry per-feature index. Exact reads must report limit errors when over budget and return all records when within budget; feature and byte-budget samples must stop within their budgets and preserve ordinal IDs. A desktop test with more than 20,000 coincident polygons verifies the automatic retry marker and a nonempty bounded overview without retaining a full feature snapshot. Run native, Qt/native, vet, and QML gates.
