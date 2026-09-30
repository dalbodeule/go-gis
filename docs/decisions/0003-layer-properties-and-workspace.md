# Layer properties and workspace persistence

## Decision

Layer display names, source dataset/layer identity, source text encoding,
visibility, symbology, and label rules belong to the format-neutral core layer
metadata. The stable `Name` is the render/source identity and `DisplayName` is
the editable workspace-only name. GUI controls edit this metadata through the project service; renderers
and GDAL adapters consume it through explicit boundaries. Renaming a displayed
layer must not change the layer name stored inside its source dataset.

The `.gogis` workspace is a versioned JSON document that stores references to
original files and project state, not copies of feature data. It must preserve
layer order, project/display CRS, paths, visibility, style, and label
configuration, normalized map center/zoom, and the active layer. Relative paths
are interpreted from the workspace file's directory by the workspace loader.
Saving uses a temporary file in the target directory so an incomplete write
does not replace a valid workspace.

Symbology is renderer-independent: separate point, line, and polygon colors,
physical point/line sizes in millimeters, and fill opacity. Label configuration
supports field/property expressions, center/rotated/free-angle placement,
rotation fields, physical text height, scale limits, and a rule expression.
Lua label source is stored as project data and evaluated by a restricted,
cancellable label context. The desktop properties dialog edits layer display
name, source reference/encoding, visibility, basic symbol colors/sizes,
opacity, and label settings. `.gogis` open/save preserves these settings and
references original datasets rather than embedding feature data.

## Validation and implementation boundary

Layer names are unique without regard to case. Colors, sizes, opacity, label
placement, label expression, and scale ranges are validated before persistence.
Source encoding is a per-layer setting; GDAL's process-global encoding options
must not be changed around concurrent reads. The adapter must use scoped/open
options or serialize access if GDAL cannot scope the selected encoding.

The version-1 document, validation, desktop editing, source reload/relink,
workspace reopen, scoped GDAL Shapefile encoding, and Lua label/rule evaluation
are implemented. The Qt renderer currently applies point/line/stroke colors and
renders configured labels. Point size and line width are expanded in screen
space from millimeter settings, so map zoom does not change their physical
screen size. Polygon interiors use GEOS constrained triangulation, preserving
holes; triangles are clipped to render chunks before applying fill opacity.
Polygon labels use a GEOS point-on-surface anchor, and line labels use the
half-length point and its local segment angle.
Windows build and actual desktop interaction still require planned manual
verification.
