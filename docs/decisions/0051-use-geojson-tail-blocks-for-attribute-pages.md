# Use GeoJSON tail blocks for attribute pages

## Context

The desktop keeps at most one million per-feature GeoJSON/GeoJSONSeq index entries. It retains coarse byte-offset blocks for the rest of the file so map windows and single-feature lookup can avoid scanning from the beginning. Attribute pages beyond the per-feature index still called the full-file scanner. On a ten-million-feature source, opening a page near the end could therefore parse all ten million records again while the UI waited for 128 rows.

## Decision

Use the retained tail block offsets for attribute pages outside the per-feature prefix. Seek to the block containing the first requested ordinal and scan only that block and subsequent blocks needed to fill the page. Decode properties for returned rows only. A page crossing the prefix boundary combines indexed and tail rows under the same page payload limit. Retain the source-stamp validation before and after the read, cancellation checks, and the existing full-scan fallback if no tail blocks are available. No source data or persistent cache is changed.

The coarse index has at most 32,768 blocks and initially groups 4,096 features per block; it merges blocks when needed to keep memory bounded. Random pages now cost one block scan plus at most 1,000 returned rows instead of a full-source scan. This changes random access time, not the one-time inspection cost of a GeoJSON source.

## Verification

Test collection and sequence sources with a partial per-feature index, a page spanning the prefix boundary, a page spanning tail blocks, the last page, and a page past the end. Run native GDAL tests and the repository Go checks.

An explicit Apple M3 benchmark generated 10,000,001 GeoJSONSeq point records in a temporary directory. The initial inspection took 6.642 seconds; reading the final 128-row page took 0.279 ms and 150,520 B/op, with 48,440,248 bytes of retained Go heap after inspection and the page. This measures the attribute-page path; it does not measure Qt frame time or a dense viewport.

A separate explicit Qt/native desktop benchmark generated 10,000,001 point records, selected the read-only policy, inspected and loaded the source, and built one viewport chunk. It completed in 7.451 seconds total, with a 17.78 ms first chunk, 50.54 MiB retained Go heap, 192.3 MiB peak process RSS, and 64 visible features in that chunk. This is a sparse point fixture and does not establish dense-polygon or multi-file frame performance.
