# ADR 0006: Scale-aware read-only overview and DXF import

## Status

Accepted; implementation and data-path tests are present. Visible Qt/GPU validation remains deferred.

## Context

Large cadastral layers can create more than 13,000 small GDAL viewport chunks at a municipality-wide
view. Retaining full hit-test geometry, labels, and render vertices for every chunk can exceed the
viewport payload or Qt scene-graph vertex budget. DXF already has a GDAL/OGR reader but was not
selectable through the desktop vector-file dialogs.

## Decision

- Prioritize requested chunks by distance to the viewport center, preserving layer draw order, so
  useful geometry is published before the full viewport finishes.
- Use larger bounded read-only cells at overview scale and refine the grid to 1/64 and 1/128 as the
  user zooms in. Keep the native vertex and per-window payload budgets; do not raise them to hide
  incomplete rendering.
- At zoom bucket 1 and below, query geometry without attributes and keep a deterministic feature
  sample for display. Do not retain overview hit-test, attribute-name, or label geometry. Mark this
  mode in the status bar; zoom bucket 2 restores all features and interaction.
- At zoom bucket -2 and below, topology-preserving simplify complex line/polygon geometries for
  display and generate polygon fill meshes from that simplified copy. Never write the simplified
  geometry back to the source or project model.
- Import `.dxf` through the installed GDAL/OGR DXF driver. Preserve its entity/layer model. DXF
  commonly has no CRS, so do not guess a projected CRS; the reader treats coordinates as supplied
  unless the user provides a CRS through an existing override path.

## Consequences

Overview rendering is intentionally approximate and feature selection/labels are disabled until
zoom-in. Per-feature simplification does not guarantee shared cadastral boundaries remain identical.
The source-window queries and full overview may still take time, but the user can receive partial
geometry early while viewport memory and native vertex limits stay bounded. Small DXFs can be loaded
as vector layers; large CRS-less DXFs may not qualify for windowed read-only loading and remain
subject to the materialized reader's existing safety limits.

## Verification

- Synthetic runtime test proves simplified polygon fill contains fewer vertices and detail is kept
  on the exact zoom tier.
- Downloads SHP integration: initial overview reached first non-empty publication at 100 ms and
  completed in 64.4 s without a viewport/scene-graph safety error; overview retained no hit geometry.
- The same two SHPs passed 128 exact-viewport moves per layer at zoom bucket 2, with 80 MiB test
  process peak RSS.
- The sample DXF imports through OGR as four `entities` features. Real ARES rendering and visible
  Qt/GPU verification remain outstanding.
