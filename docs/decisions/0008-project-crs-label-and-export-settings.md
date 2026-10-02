# Project CRS, label viewport bounds, and DXF encoding selection

## Decision

- Project CRS is an editable project-level setting. A change is validated with
  PROJ and rebuilds the loaded runtime asynchronously; source datasets remain
  unchanged. The workspace persists the selected target CRS.
- Source CRS override belongs to each layer's data-source settings. It is
  optional, is retained separately from the layer's current/display CRS, and
  applies only when GDAL's detected CRS is missing or incorrect.
- Label publication is viewport-bounded and capped at 20,000 text objects.
  Dense visible sets are sampled deterministically rather than causing the
  complete label layer to disappear. The UI exposes a per-layer current-view
  count and a status hint when the bound is reached.
- DXF text encoding is an explicit export choice: UTF-8, CP949 (`ANSI_949`),
  or Shift-JIS (`ANSI_932`). The interface language selects the initial choice
  (Korean → CP949, Japanese → Shift-JIS, other → UTF-8), while all options
  remain available regardless of locale.
- Cursor coordinates use four fixed decimals and comma-grouped thousands in
  the current project CRS.

## Rationale

Project CRS belongs to project state rather than to one input file. Rebuilding
from immutable source references gives each layer consistent transformed
coordinates without writing back to user data. CRS overrides solve datasets
whose metadata is absent or wrong while keeping the original CRS reference
available for reloads.

The former all-or-nothing label payload limit made labels appear disabled on
large cadastral datasets. Viewport culling and bounded deterministic sampling
preserve useful labels while limiting QML object creation. DXF code pages are
interoperability choices controlled by the target CAD application, so locale
should choose a sensible default but must not remove the user's choice.

## Verification

- Native PROJ tests validate recognized and invalid CRS inputs.
- Desktop Go tests cover viewport culling and the label payload bound.
- DXF tests round-trip CP949 and Shift-JIS profile text and check their header
  code pages.
- QML tests cover project CRS requests, coordinate formatting, language-based
  encoding preference, and source CRS form state.
- Manual verification remains necessary in ARES Commander for encoding and
  visual compatibility.
