# Plan filtered SHP tiles from accepted bounds

## Context

A city-scale workspace render diagnostic planned 428 tiles for three layers, of which 354 were empty. The read-only SHP loader already builds an exact accepted-feature ID set and its geometry envelope for simple string display rules, but viewport planning still used the unfiltered source bounds. A distant filtered-out feature could therefore schedule many empty tiles.

## Decision

Keep the full source bounds in layer metadata and the combined map extent. Store a separate display-CRS render envelope in the read-only binding when the source-side accepted ID set has bounds. Use that envelope only to prune viewport tile keys. An empty accepted set schedules no tiles. Changing the display rule invalidates both the ID set and the derived render envelope. If an accepted envelope cannot be transformed, leave planning on the source bounds so no visible feature is skipped.

The rule is applied only to SHP sources for which the existing index has enumerated accepted IDs. Other Lua rules and formats continue to use their existing fail-open planning behavior.

## Verification

A read-only SHP regression with one accepted local polygon and one filtered distant polygon retains the full source extent and a local preferred fit. The accepted render envelope reduces over 1,000 synthetic candidate tiles to at most four and the actual initial viewport reaches a ready render with no more than 16 tiles. Native Qt, GDAL, default Go tests, vet, QML tests, and diff checks are the required gates.
