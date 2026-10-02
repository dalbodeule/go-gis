# ADR 0007: Layer removal and metric map-view continuity

## Status

Accepted for the desktop prototype. QML, native runtime, and offscreen
real-MapCanvas bridge tests are present; large multi-file interaction still
needs an on-screen user validation pass.

## Context

Layer-tree and map-metadata publications can arrive in adjacent GUI ticks when
several files are added quickly. Refitting on every tree update, or combining a
new data extent with an old canvas snapshot, changes the apparent map scale.
The layer context menu also stayed open after clicking elsewhere, and there
was no project-layer removal action.

## Decision

- Keep the map center and horizontal meters-per-screen-pixel across extent
  changes. Recompute zoom from the new extent and canvas dimensions rather than
  reusing a normalized zoom. Preserve the selected layer when the tree changes.
- Acknowledge each map-metadata generation only after QML applies it. Until
  that acknowledgement reaches the native bridge, use the last saved map view
  for another rapid layer operation instead of a stale canvas snapshot. Compute
  an initial fit view before publishing the first layer, so a second addition
  in that same interval still has a center and scale to preserve.
- Queue vector-file selections made during an active load and process them in
  order after publication. Keep selections separate so a failed source does
  not block later sources; cancelling a load discards its pending selections.
  QML journals selections until the native scene-graph bridge captures them;
  the bridge drains the captured requests to Go in order. This covers multiple
  selection signals within one GUI frame, where a single latest-path property
  would otherwise lose all but the last request.
- Use equal horizontal and vertical map units per screen pixel for projected
  meter-based data. For EPSG:4326, use the extent-center latitude to adjust
  longitude degrees to approximate meters; a single 2D canvas cannot be
  exactly metric over an entire geographic extent.
- Close the layer context menu on outside click or Escape. Remove a layer only
  after confirmation; rebuild the project from remaining layers while leaving
  source files untouched. Rebuilt in-memory layers must serve attributes from
  the rebuilt project, not an empty source path. Cancel or reject removal while
  a file load is active.

## Consequences

Rebuilding a read-only project after removal may reopen remaining GDAL sources
and briefly use more memory. The confirmation dialog makes clear that source
files remain. Full-extent fitting remains an explicit user action after the
first layer is shown. Geographic meter-scale correction is local to the map
center and should not be treated as a geodesic measurement tool.
