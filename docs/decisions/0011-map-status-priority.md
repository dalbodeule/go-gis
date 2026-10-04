# ADR 0011: Prioritize map context in the desktop status bar

## Status

Accepted for the desktop prototype. QML interaction and layout tests are in
place; an on-screen user check remains useful across display sizes.

## Context

The single-line footer put loading text, coordinate entry fields, memory use,
layer count, CRS, cursor coordinates, and scale in one row. At typical window
widths, the map information needed for navigation was clipped at the right.

## Decision

- Give CRS, current map coordinates, and approximate scale a dedicated primary
  row. Show cursor coordinates while the pointer is over the map; otherwise
  show the current viewport center.
- Keep loading status, cancellation, logs, layer count, and memory use in a
  secondary row. Long loading messages may elide and retain a full tooltip;
  memory use may hide in narrow windows.
- Move X/Y coordinate entry into a small navigation dialog, opened from the
  secondary row. Prefill it from the viewport center and reject empty or
  nonnumeric input.
- Keep the scale explicitly approximate because its denominator uses a 96 DPI
  display assumption, not calibrated physical screen DPI.

## Consequences

The map viewport is slightly shorter. The primary map context remains readable
without relying on the width left over by long render messages. The navigation
dialog adds one click to manual coordinate entry but frees persistent space.
