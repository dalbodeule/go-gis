# Filtered bounds for the initial map fit

## Context

When multiple files are opened, the full bounds of every polygon source contribute to the preferred initial fit. A display rule can hide a distant outlier but its original source bounds still expand the map fit, making the visible cluster tiny and causing excess chunk planning. The full source extent is still needed for coordinate mapping and complete source inspection.

## Decision

When a Shapefile's safe string display-rule prefilter has a complete, valid union envelope of accepted geometries, transform that envelope to the display CRS and use it for the preferred fit. Keep the full source bounds in `mapExtent` and per-layer query bounds. A filter accepting no features contributes no preferred fit while other layers are present. If the accepted envelope is uncertain or cannot be transformed, fall back to the source bounds. This changes the initial view only; accepted features remain accessible through navigation and the full extent.

For a strongly one-sided extent, the read-only level-of-detail calculation estimates the preferred fit's on-screen fraction after aspect-preserving canvases are applied to the full and preferred extents. It uses this estimate only when the X and Y fit fractions differ by more than 64 times. This prevents a distant coordinate on just one axis from making the initial visible area look like an extreme detail zoom. Other extents retain the prior LOD policy.

For unfiltered read-only sources, the driver returns up to 256 evenly spaced geometry envelopes without retaining feature objects. GeoJSON/GeoJSONSeq uses its retained prefix and tail-block index; OGR layers use fast indexed cursor positioning only when the driver advertises it. The desktop trims the outer one percent of sampled bounds and uses this candidate for the initial fit only if the full source is at least 64 times wider on one axis. The source bounds and all features remain available. A small distant file is also excluded from the preferred fit when its feature count is below one percent of the largest layer in the same geometry group and including it would expand that group's bounds more than 64-fold. Comparable or nearby files remain in the fit. Polygon/line layers continue to take priority over point layers for initial framing.

## Verification

Use two temporary Shapefiles: one with a nearby accepted polygon and a distant rejected polygon, and another nearby polygon. Confirm that source extent still includes the distant coordinate while preferred fit contains the visible cluster. Additional work is needed for outliers that remain visible or occur in sources without this prefilter.

For a 10,000-by-2 full extent and 2-by-2 preferred fit, raw zoom bucket 12 should use overview bucket 0.

Test a large nearby point file plus a distant one-feature file, and a distributed sample with one distant point. These rules are only initial-framing heuristics; a legitimately separate, very small distant dataset may require Full Extent or layer zoom to view it.
