# Skip transparent detail fill budgets

## Context

The Gongju parcel layer has zero fill opacity. Its detail windows still scanned every polygon WKB to decide whether fill triangulation was safe and emitted `outline-only` diagnostics. The fill builder already skips triangulation for a fully transparent fill. A profile of the 10-second viewport showed the remaining work is largely GDAL reads and visible geometry generation, so this change targets redundant safety work rather than altering display detail.

## Decision

For detail windows with a fully transparent effective fill color, skip the polygon vertex budget scan and fill attachment call. Continue drawing the original outlines. Keep the overview path unchanged because its `outline-only` decision also affects coarse boundary selection. An opacity or style change rebuilds the layer presentation through the existing settings path.

## Verification

The Gongju read-only viewport produced the same 428 chunks, 74 nonempty chunks, and 8,504,404 vertices after the change. Default, Qt, and Qt/native tests and QML tests must pass. The measured render time changed only slightly and is within run-to-run variation; no speedup claim is based on this change alone.
