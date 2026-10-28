# Precompute accepted SHP feature IDs for simple display rules

## Context

The Gongju workspace's building and survey-point layers use simple numeric-string display rules. A city-scale request completed 428 chunks in 31.8 seconds; the building layer occupied most of the first 25 seconds. The existing SHP path evaluates Lua before copying rejected geometry, but GDAL still passes every spatial candidate through Go attribute decoding and Lua. Only 74 chunks produced geometry.

Adding the SQL condition to every spatial window was measured at 36.5 seconds versus 28.0 seconds for the same unmodified QIX window path, so that approach was discarded. A one-time scan of the million-building source returned 104,454 matching FIDs in about 1.1 seconds.

## Decision

Recognize only complete Lua rules of the form `return feature.FIELD == 'digits'` or `return string.sub(feature.FIELD, 1, N) == 'digits'` where `N` matches the literal length, and only when the source field is text. A one-time OGR query records matching source feature IDs in a bounded bitset. Each viewport tile keeps its original QIX spatial query and checks source IDs before decoding attributes or running Lua. The original Lua program still evaluates every retained feature, so the bitset is a prefilter rather than a replacement for the display rule. Unsupported rules, non-text fields, and index failures use the existing path. Keep coarse overview queries on their existing path so feature sampling does not change. A changed display rule invalidates the old bitset immediately. The source data, exported features, and workspace rule remain untouched.

The same scan unions the envelopes of accepted source geometries. A tile wholly outside that conservative envelope returns empty before opening a GDAL window. If any accepted envelope cannot be read or is invalid, the envelope shortcut is disabled while the FID filter remains usable. Missing source FIDs also fall back to the Lua predicate.

The one-time query's result is closed after construction. The bitset stores one bit per source feature (about 125 KiB for one million buildings), with a 50-million-feature limit. Only four envelope coordinates are retained; no geometry or attribute values are retained.

## Verification

Compare ordinary and indexed SHP window results and budgets on a small redistributable fixture. Check the actual Gongju SHP through a temporary local probe without adding it to the repository. Run Go and Qt/native tests, vet, and formatting checks.

In the same local viewport harness, the original QIX path completed in 27.98 seconds. With the FID bitsets, rendering completed in 18.75 seconds; workspace load plus rendering took 20.95 seconds. Both produced 428 chunks, 74 nonempty chunks, and 8,504,404 vertices. These measurements are from the test harness, not an interactive GUI timing guarantee.

After adding the accepted-envelope shortcut, the same harness completed rendering in 10.36 seconds and workspace loading in 2.27 seconds. Chunk and vertex totals stayed identical. The user's preceding interactive log with FID bitsets took 18.61 seconds to render; the envelope shortcut still needs a new interactive run to confirm its effect in the application.
