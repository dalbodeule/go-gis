# Layer properties and workspaces

Build the native desktop application with `./scripts/build.sh desktop-native`,
then launch `./build/gogis-desktop-native`. The desktop opens an empty project.
Use **Add vector files** or **Open workspace** to begin. Right-click a layer in
the layer list to open its category menu; choosing General, Data source,
Symbology, or Labels and expressions opens the corresponding page in the layer
properties dialog. **Attributes** opens a focused table for the active layer.
On the map, drag blank space to pan, scroll to zoom around the cursor, and click
a feature to inspect it. The displayed map viewport preserves the data extent's
aspect ratio so horizontal and vertical map units use the same screen scale.
Appending another layer keeps the current map center and zoom while expanding
the combined extent. Start the desktop in Korean or Japanese with
`./build/gogis-desktop-native --lang=ko` or `--lang=jp`; English is the default.
The **About GoGIS** window shows version, runtime, build target, and license.
Right-click a layer and choose **Remove layer…** to remove it from the current
project after confirmation. Its SHP, GeoPackage, or other source file is not
deleted. Clicking outside the layer menu closes it. Adding or removing other
layers preserves the current map center and approximate metric screen scale;
use **Zoom to full extent** when you want to fit all layers again.
For an editable layer, select a feature, choose **Edit vertices**,
and drag a vertex handle. The edit is applied to the layer and saved to its
configured destination; read-only layers do not expose vertex handles. To limit
UI overhead on complex features, at most 10,000 handles are shown per feature.

## Layer source and project name

The display name is local to the GoGIS workspace; it does not rename a layer in
the source dataset. The source path and source-layer name identify the dataset
and layer to reopen. Use **Browse** to relink a moved dataset. Changing the
source path, source layer, or encoding reopens that layer; if reopening fails,
the previously loaded layer remains in the project and the status bar reports
the failure.

Encoding is a per-layer GDAL open option. Leave it empty for driver detection,
or enter an encoding such as `UTF-8` or `CP949` for a Shapefile whose DBF text
is not detected correctly. It does not modify or transcode the source files.

## Symbology and labels

Point, line, and polygon colors are separate `#RRGGBB` values. Point size,
line width, and label height are in millimeters and stay screen-sized while
zooming. The renderer uses Qt's reported screen pixel density to convert
millimeters to screen coordinates; reported physical size can vary in accuracy
by monitor and operating system. Polygon fill opacity is from 0 (transparent)
to 1 (opaque); polygon holes are preserved. Use **Outline only (no polygon
fill)** to set opacity to 0 while keeping boundary lines visible. Unchecking
it restores the previous nonzero opacity. The slider offers a quick visual
adjustment and the numeric field allows an exact value. The symbology page
shows controls relevant to the layer's geometry type when it is known.

Labels can use a property template, for example `${name}` or
`${name} (${class})`. Click **Available fields…** next to the label template to see this layer's
attribute names and types; clicking one inserts `${FIELD}` at the cursor.
The list may take a moment to appear while the active layer's attributes load.
Text height is in millimeters. Minimum and maximum scale
are denominator limits: at `1:25,000`, a minimum of `1,000` and maximum of
`50,000` includes the label. A zero limit is unbounded. Polygon anchors are
placed on the polygon interior; line labels use the half-length point.

Placement modes are:

- **Center**: unrotated representative point.
- **Center + rotation**: representative point with the selected rotation field
  (finite numeric degrees counter-clockwise from map east; a null/empty value
  is treated as 0). The renderer converts map angles to screen angles using
  the current extent and canvas aspect ratio.
- **Free angle**: uses the selected rotation field when set; otherwise line
  labels follow the local line angle, while other geometries remain unrotated.

The **Open Lua editor and examples…** dialog shows the active layer's field
names and provides separate editors for the optional Lua label script and
display rule. The script replaces the property template and must return a
string, number, or `nil`. The optional display rule runs first and must return a
boolean. Both scripts read attributes through the read-only `feature` table,
for example:

```lua
return string.format("%s (%s)", feature.name, feature.class)
```

```lua
return feature.class == "primary"
```

The label Lua context exposes only base, table, string, and math libraries; it
does not expose filesystem or process libraries. Scripts are size-limited and
observe cancellation.

## Saving project state

Use **Save workspace** to write a `.gogis` file and **Open workspace** to
restore it. A workspace stores source references, display names, encoding,
visibility, styles, labels, map view, and active layer; it does not embed
feature data. Paths inside the workspace are made relative to the workspace
directory when possible. Keep the referenced datasets available or relink them
if they move. Relative paths use a portable slash form in the workspace and
are resolved using the host platform's path rules when opened. To persist
feature edits, use **Save GeoPackage** separately. If a source cannot be
opened, the workspace retains that layer as an empty relinkable entry, marks it
in the layer list, and lets you choose a replacement source in Layer Properties
without losing the other layers.

See [build instructions](build.md) and the [manual verification checklist](verification/mvp-checklist.md).
