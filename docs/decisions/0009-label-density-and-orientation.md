# ADR 0009: Screen label density and orientation

- Status: implemented
- Date: 2026-10-04

## Decision

- The desktop map culls labels outside the viewport and suppresses overlapping
  text boxes. At approximate scales of 1:5,000, 1:25,000 and 1:100,000 or
  wider, collision padding increases. The omission is a screen-only policy;
  feature labels and properties are not removed from the project.
- After collision removal, a screen-only label count cap depends on the
  approximate scale denominator: below 1:18,000 = 200;
  1:18,000–49,999 = 150; 1:50,000–99,999 = 100; 1:100,000 or wider = 50.
  This is an upper bound, not a target count; sparse areas and collisions can
  display fewer labels. Unknown scales also use the 200-label screen cap.
  The separate 20,000-label serialization safety limit remains in place.
- DXF export regenerates labels from the complete source layer and writes every
  eligible label, even when labels coincide on screen. Its existing feature and
  byte safety limits remain in force.
- `center` means an east–west baseline (0°), `vertical` means a north–south
  baseline (90°), and `free-angle` uses the longest non-degenerate geometry
  segment. A line label is anchored at that segment's midpoint; polygon labels
  keep their point-on-surface anchor and take only the angle from the boundary.
  Reverse digitization is normalized to an upright angle. The existing
  `center-rotated` and explicit rotation-field settings remain readable for
  previously saved workspaces.

## Verification boundary

QML tests verify that maximized and full-screen transitions retain the layer
pane, map canvas, toolbar and status controls under the offscreen Qt platform.
They do not prove the same appearance in macOS native Spaces/full-screen mode;
a user screenshot or live macOS visual check is still required if the reported
disappearance recurs. Window-state transitions now log requested/current state,
window and map sizes, and layer count to distinguish a layout problem from a
lost project or a native full-screen presentation issue.
