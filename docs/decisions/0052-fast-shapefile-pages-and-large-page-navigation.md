# Fast Shapefile pages and large-page navigation

## Context

The attribute table displays 128 rows at a time. A ten-million-feature layer has over 78,000 pages. Previously a non-adjacent Shapefile page used OGR SQL `LIMIT/OFFSET`, which can scan preceding rows, while the UI only offered Previous and Next buttons.

## Decision

For a Shapefile layer advertising `FastSetNextByIndex`, position its retained OGR cursor at the zero-based page offset and decode only the requested rows. Keep the existing cursor path for adjacent pages and the SQL fallback for other drivers or a failed positioning call. Serialize all cursor access under the existing `AttributeSession` lock. Add an editable page-number SpinBox to the attribute dialog, clamped to the reported page count. The page payload remains capped and only the requested 128 rows reach QML.

Opening the PostGIS export dialog now selects the first available layer explicitly. A dynamically populated layer model could otherwise leave its ComboBox index at -1, causing the confirm action to reject a valid table name; the existing QML export test exposed this while verifying the page control.

## Verification

Test random, adjacent, first, and repeated SHP page requests against source ordinals. Run QML tests for the dialog control and native GDAL tests. An explicit Apple M3 benchmark streamed 10,000,001 point features into a temporary Shapefile, then read its final 128-row page in 0.209 ms (120,320 B/op, 1,172 allocs/op). The one-time fixture creation took about 59 seconds including compilation and test setup; the page metric excludes that creation and source inspection. This fixture measures page seeking, not dense geometry rendering.
