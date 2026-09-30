# Layer properties and workspaces

Build the native desktop application with `./scripts/build.sh desktop-native`,
then launch `./build/gogis-desktop-native`. The layer properties dialog is
available from the attributes pane after loading vector data.

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
to 1 (opaque); polygon holes are preserved.

Labels can use a property template, for example `${name}` or
`${name} (${class})`. Text height is in millimeters. Minimum and maximum scale
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

The optional Lua label script replaces the property template and must return a
string, number, or `nil`. The optional display rule runs first and must return a
boolean. Both scripts read attributes through `feature`, for example:

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
