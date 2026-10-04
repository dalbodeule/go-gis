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

## Follow-up: municipality-scale performance and close inspection (2026-10-02)

The Sejong test data showed that an 8x wheel ceiling could not reach cadastral detail,
and that 1/128 cells still read roughly 200 m across for a 25 km source extent. The
viewport now derives its wheel ceiling from a 25 m target screen width and the current
canvas-to-viewport ratio. EPSG:4326 longitude width is converted at the data extent's
center latitude; projected coordinates follow the existing meter-unit convention.
The wheel delta magnitude also controls the zoom step, including high-resolution
trackpads.

Read-only cells retain their existing sizes through zoom bucket 4 and halve at each
higher bucket, up to bucket 20. At bucket 9 a 25 km source extent produces roughly
6 m cells, limiting a 25-50 m view to a small neighborhood of windows. Coarse sampled
views through bucket 1 are simplified before fill triangulation. Progressive full-batch
publication is throttled to 20 updates per second because every publish flattens and
copies the accumulated visible geometry. Native and viewport vertex/payload safety
budgets remain unchanged.

These changes target first useful overview cost and high-zoom query locality. They do
not yet replace full-batch publication with per-tile scene-graph nodes, and need timing
and visible Qt/GPU validation on the supplied cadastral layers before claiming a
measured speedup or exact 25 m behavior on every projected CRS.

## Follow-up: automatic SHP spatial index (2026-10-02)

The supplied SHP files lack `.qix` sidecars, so repeated GDAL viewport filters can scan
the same source repeatedly. For viewport-backed SHP layers at or above a configurable
feature threshold (default 100,000), the desktop can prepare a QIX automatically.
`cache` mode copies the source components into an OS temporary cache and indexes that
copy; `source` mode writes only the `.qix` beside the original SHP; `off` disables the
behavior. The temporary copy is fingerprinted by source path and component size/mtime
and is reused when that identity is unchanged. Index creation is best-effort: on
failure the original source still opens without an index. Startup arguments override
environment configuration. The default cache mode avoids writing beside user data at
the cost of temporary disk space and a one-time copy/index build.

## Follow-up: complete viewport coverage and parcel detail (2026-10-02)

The Qt map item can be narrower than its clipped parent viewport, especially for a
tall dataset. Planning a square normalized window (`1/zoom` on both axes) then omits
the left and right portions visible on screen. The chunk planner now uses the parent
viewport dimensions divided by the map item's unscaled dimensions for each axis.
The Qt scene graph also rebuilds its stroke/point triangles when item scale or
display pixel density changes, so a cached vertex batch keeps the requested
physical symbol size while zooming. Its 0.5-pixel minimum is applied in final
screen pixels before dividing by item zoom. Previously that minimum was in
local item pixels, so 200x zoom could turn a line or point into a 100-pixel
wide mark even when its configured millimeter size was small.

The routine municipality overview retains individual parcel outlines and simplifies
their display copies at one-quarter of the previous tolerance. Dissolving the parcel
coverage is reserved for an explicit render-budget retry because it discards all
internal boundaries. The native viewport batch ceiling rises from 2.5 million to
8 Mi source vertices, with matching 8 Mi native source and 24 Mi Qt scene-graph
vertex ceilings. These are bounds, not preallocations; chunk and source-window
limits still apply. Qt Quick remains the renderer because the observed clipping came
from viewport planning and the sparse overview from geometry reduction.

With the two supplied Sejong SHPs, the full fitted overview produced 3,220,396
source vertices across 270 chunks in 3.605 seconds in the native integration
test. The coverage-only safety retry produced 75,146 vertices in 20.926 seconds.
Both stayed within the new bound. Visible macOS window inspection is still needed
to confirm appearance and memory use with a live Qt scene graph.

## Follow-up: parcel visibility during rapid zoom and pan (2026-10-02)

The next user log showed a complete two-layer frame at generation 4, followed by
dozens of viewport generations without another ready frame. A new zoom bucket
invalidated the old chunk keys; the first quick point chunks then replaced the
whole native vertex batch while slower polygon chunks were still loading. The
cancel event in that log was a user button press after the blank parcel view
appeared, not a background failure.

For read-only maps, retain the last complete native frame while a replacement
generation loads. Publish the replacement only when every requested chunk has
arrived; initial loads remain progressive. Viewport gestures are scheduled after
120 ms without another pan/zoom event, avoiding repeated cancellation of GDAL
window queries on each 16 ms UI poll. A manual cancel leaves the last complete
map on screen. The real two-SHP integration at zoom 194.44 produced 1,715,577
parcel vertices and 1,718,045 total vertices, confirming that the zoomed
parcel source itself has drawable geometry within the viewport budget.

## Follow-up: preserve view on layer append and reduce close-scale queries (2026-10-02)

Appending a read-only point source previously called `fitPreferredDataExtent`,
which reset the center and scale to the parcel bounds. Even excluding point
outliers from the preferred fit did not honor the current pan and zoom. The
append path now transfers the current world center and map units per screen
pixel into the new total extent. It calculates the new Qt map item's size from
the parent viewport and the changed extent aspect ratio, then sets the zoom
that preserves those units per pixel. The preferred parcel extent remains
available to the explicit Full Extent action.

From zoom bucket 7 upward, query cells are one power-of-two step larger. This
reduces the number of GDAL windows during close inspection without changing
the retained geometry budget. A real two-SHP test at zoom 194.44 used 96
chunks and completed in 3.518 seconds; at zoom 840 it used 84 chunks and
completed in 2.578 seconds. These are sequential builder measurements in the
test process, not live GPU frame times. The wheel factor is now 1.3 per notch;
the existing 25 m visible-width ceiling already permits more than 20x zoom
from the reported approximately 1:6,437 view, as checked in a QML test.

## Follow-up: bound intermediate scene-graph publication (2026-10-04)

The Windows Sejong parcel run completed 1,225 overview chunks with 9,219,615
source vertices. Its projected extent, fitted extent, canvas size, and normalized
vertex bounds were valid, but the UI still showed only a small part of the map
when the Go render scheduler reported Ready. A full intermediate publication
copies the accumulated batch into native storage and expands strokes to Qt
triangles. At this size, doing that every 50 ms can leave the scene-graph
thread processing obsolete frames and retain several large buffers.

For read-only layers, stop intermediate geometry publications after 250,000
accumulated source vertices and publish the complete batch once. Keep the first
small preview and continue progress/label updates while geometry builds. Log
nonempty chunk count and vertex distribution so a subsequent Windows run can
distinguish a sparse generated batch from a late scene-graph frame. This change
reduces redundant transfer work; visible Qt/GPU validation is still required
before treating the partial map as resolved.

## Follow-up: filled coverage for sub-pixel parcels (2026-10-04)

The next Windows run confirmed that the final 9,219,615 source vertices reached
Qt as 15,750,315 scene-graph vertices. Six hundred chunks had geometry, and
their vertices occupied 165 of 256 map bins. The scheduler and scene graph
both completed while only a small part of the county was visibly colored.
At roughly 1:209,749 with no MSAA, many parcel triangles and thin edges are
smaller than a screen pixel. A polygon boundary-only overview would discard
the fill the user needs.

For read-only polygon layers with at least 100,000 source features, semantic
overview buckets 0 and below now union each bounded window into fillable
polygon coverage. The existing 1/32-extent overview windows remain: a trial
with 1/8 windows made dense Sejong queries too expensive and hit the 180-second
serial integration-test deadline. More detailed buckets keep the existing
individual parcel geometry. This is a display-only approximation;
source data and detailed feature queries are unchanged. The existing explicit
safety retry still uses the cheaper outer boundary. Real Windows appearance
and cross-window seams still need a live check.

The cached, QIX-indexed Sejong SHP completed all 1,225 planned overview chunks
in a sequential native test: 600 chunks contained fill, with 206,849 source
vertices (179,793 fill vertices) in 56.3 seconds. The previous parcel-by-parcel
run emitted 9,219,615 source vertices. These timings are CPU-side builder
measurements; the app schedules chunks concurrently. Reading the unindexed
source directly was about 2.6 seconds per sampled query, so performance
comparisons must use the same QIX-backed data path as the app.

## Follow-up: split the Qt scene graph into bounded triangle batches (2026-10-04)

The filled-coverage run still displayed only a central block. The final
206,849 source vertices reached Qt as 260,961 scene-graph vertices within
milliseconds, so a late frame was not the cause. A full native test measured
about 0.505 of normalized map area in fill triangles, distributed across the
whole county. Independently rasterizing the same indexed SHP at the canvas
resolution produced a continuous full-extent county footprint. This rules out
missing source data, an incorrect extent, or sub-pixel rural polygons as the
main explanation for the partial display.

Qt now receives the converted triangle stream in separate, triangle-aligned
scene-graph nodes of at most 60,000 vertices. This avoids depending on one
large draw in the affected Windows OpenGL scene-graph path, while preserving
the per-vertex colors, alpha, drawing order, and source geometry. Intermediate
read-only publication is also capped at eight frames or 30,000 source vertices,
whichever comes first; the final frame is always published. The previous
250,000-vertex threshold never engaged for this 206,849-vertex overview and
caused hundreds of redundant scene-graph rebuilds. A live Windows visual
check is still needed to confirm that batching resolves the GPU-side symptom.

The first Windows build with child nodes still looked similar. Qt may merge
compatible geometry nodes during scene-graph batching, so the vertex-color
material now requests `NoBatching` and the optional performance log reports
the actual child-node count and item dimensions. A full indexed-source test
also confirms that the final `BatchStore` flattening preserves all 206,849
vertices and the four quadrants' fill area. All fill triangles have one
consistent winding; loss of only outer quadrants is not explained by a
per-region winding change. This remains an experimental rendering fix until
the script-built Windows executable is visually checked.

## Follow-up: Windows dense-cadastre windows at city and detail scales (2026-10-04)

A three-layer Windows run with the 208,015-feature Sejong polygon SHP and
far-away point coordinates showed 294 chunks and 810,521 source vertices at
semantic LOD 5, taking 129,965 ms to become ready. The QSG renderer is shared
by macOS and Windows; there is no separate macOS map drawing implementation.
The total data extent was much larger than the preferred parcel fit extent,
so the physical zoom was 575 even though the parcel area occupies a small
fraction of normalized coordinates.

At semantic LOD 1, a dense cadastral window can exceed the polygon fill
triangulation budget and deliberately fall back to outlines. Extend the
display-only filled coverage dissolve from LOD 0 through LOD 1 for polygon
layers of at least 100,000 features. This keeps small parcels visible as a
continuous filled area at municipality scale; individual parcel geometry and
selection return at LOD 2. A forced safety overview remains boundary-only.

For physical zoom bucket 8 and above, keep one additional chunk-size plateau
before halving the window dimensions. At bucket 9, this doubles each side of
a GDAL query window and reduces the number of tiny indexed queries. Existing
per-window feature, byte, and polygon-complexity guards and subdivision still
apply. Live Windows visual and timing checks are required before claiming the
reported view is fixed; the 1:11,704 diagnostic frame is distinct from the
1:272,674 screenshot.

The ready diagnostic now reports fill-vertex counts per layer and zero-alpha
fills. This separates a deliberate outline-only fallback or saved transparent
style from a downstream Qt draw failure without copying user geometry into
logs.

## Follow-up: zero-alpha read-only style and empty outlier windows (2026-10-04)

The subsequent Windows Ready frame at 1:272,674 reported 138,780 fill
vertices for the 208,015-feature cadastral SHP, and exactly 138,780
zero-alpha fills. This proves the absence of green fill is a style-alpha
problem, not a missing QSG draw. The read-only window builder used a copied
layer style in `readOnlyBindings`, but applying a style-only layer-settings
change updated only the project service and `layerStyles`. The copied binding
and saved read-only source specification retained the former opacity zero.
Synchronize both when a read-only layer's style/presentation changes, so
immediate redraw and later source rebuilds use the same settings. An explicit
zero-opacity/outline-only choice remains valid and is not overridden.

The same frame planned 306 chunks but only 56 had geometry, taking 51,265 ms.
The far-away point extent makes the normalized world much larger than the
two polygon layer bounds. Before scheduling a read-only chunk, discard it when
its world-space tile is disjoint from that layer's known metadata bounds;
unknown bounds remain fail-open. This does not drop source features or change
the visible layer extent. A real Windows timing comparison after rebuilding
is still needed before claiming a speedup.

## Follow-up: bounded parallel overview builds on Windows (2026-10-04)

The next single-layer Windows frame showed the complete county and no
zero-alpha fills, but building 1,024 QIX-backed overview windows still took
42,653 ms. On 16 sampled nonempty windows, the geometry union dominated
the query, simplification, and fill stages. A reproducible, opt-in native
comparison on the same indexed source built 128 sampled windows (76 nonempty)
with identical 27,313 output vertices: two workers took 4.623 s and four
workers took 2.121 s. These are builder timings, not a live UI benchmark.

Allow four simultaneous read-only window builds and four scheduler workers
for responsive coverage overviews. Keep a separate two-slot gate for detailed
windows, acquired before the shared four-slot gate, so zoomed-in geometry
retains its previous concurrency and memory bound. Each window still has its
existing decoded-geometry byte and feature limits. More concurrent overview
unions may raise transient native memory use; verify the full SHP on Windows
before treating the sampled speedup as a production result. No new native
dependency or coverage-union shortcut is introduced: coverage union assumes
valid, non-overlapping polygons, which has not been established for this SHP.

The next live Windows run completed the same 1,024 windows, 600 nonempty
windows, and 206,849 vertices in 28,066 ms, versus 42,653 ms before the
bounded-parallel change (about 34% less wall time). A read-only experiment on
the indexed source compared just GDAL window queries plus GEOS coverage
union at 1/32, 1/16, and 1/8 normalized window sizes with four workers. The
three configurations took 17.6 s for 1,024 windows, 16.1 s for 256 windows,
and 15.9 s for 64 windows in one run. This excludes simplification, mesh
creation, and Qt publication, and does not prove equivalent rendered output.
The modest core-stage gain does not justify increasing per-window native
memory and delaying partial publication, so retain 1/32 coarse windows.

## Follow-up: retain screen-resolvable parcel boundaries (2026-10-05)

The same 208,015-feature SHP at approximately 1:208,000 and semantic LOD -1
produced exactly 206,849 vertices on both macOS and Windows. Their map
canvases were almost identical in logical pixels (513x732 and 509x726), so
monitor resolution and an OS-specific renderer did not explain the missing
detail. The display-only coverage union removed *all* parcel interior edges
on both systems. The earlier 9,219,615-vertex per-parcel view retained those
edges but caused excessive scene-graph expansion and memory use.

Replace the default dense-polygon coverage union with an ephemeral hybrid
overview. Query and simplify each parcel once, triangulate every parcel for
fill, and emit interior boundaries only for parcels whose bounding boxes are
resolvable at the semantic LOD. The threshold is derived from the existing
world-unit simplification tolerance, not the host DPI, so chunk cache keys
remain valid across window resizes and both OSes get the same geometry. A
forced safety retry still dissolves to a boundary-only overview. Source data,
exports, and zoomed-in selection remain unchanged.

On the indexed Sejong SHP, the whole-extent native test generated 6,496,211
vertices (5,954,265 fill vertices) across 1,225 planned windows, with 600
nonempty windows. The selected parcel boundaries used roughly 542,000
vertices and the full output stayed under the 12M batch limit. The normalized
fill area was approximately 0.506 across all four quadrants, matching the
previous dissolved coverage's area. The sequential CPU-side build completed
in 23.4 s, compared with 56.3 s for the earlier dissolved integration test;
these are not equivalent live UI timings. A separate four-worker builder
experiment took about 13 s, but did not retain the viewport batch. Windows
scene-graph memory, visual contrast, and end-to-end startup latency still
require a live check before claiming the regression is fully resolved.

## Follow-up: parallel indexed SHP readers and Windows workers (2026-10-05)

The hybrid overview restored equivalent detail on both systems: the same
208,015-feature Sejong SHP produced 6,496,211 vertices on Windows and
6,496,207 on macOS, including an identical 5,954,265 fill vertices. Windows
still took 12.525 s to build its live 1,024-window frame versus 3.470 s on
macOS. Sequential Windows stage profiling over 600 nonempty windows assigned
9.376 s to GDAL queries, 2.488 s to simplification, and 9.341 s to fill
generation. These stage sums are sequential CPU work, not live wall time.

`AttributeSession.OpenWindowWithLimits` locks its retained GDAL dataset for
the whole query, so the four overview workers previously serialized spatial
reads for each layer. On Windows, for indexed SHP layers with at least 100,000 features,
open up to four additional read-only GDAL sessions on the actual indexed path
and circulate them through a cancellation-aware window-query pool. Keep the
original session for attribute reads. Each clone has its own dataset and
internal lock; it is returned to the pool immediately after the window query,
and all clones close with the runtime. If any clone cannot open, close the
partial pool and use the original session. This changes neither source files
nor geometry, CRS, encoding, or exports. macOS, Linux, and other formats retain
their existing single-session behavior.

On Windows, cap the overview scheduler and cross-runtime build gate at
`min(8, max(1, logical CPUs / 2))`; retain the two-window detail gate. macOS
and Linux keep four overview workers. The read pool remains capped at four
datasets even with more builders, bounding extra GDAL handles and native
memory. On this 12-logical-CPU Windows host, four independent query sessions
read 128 sampled indexed windows in 0.539 s versus 1.319 s through one
session, with the same 30,713 returned features. The full 1,024-window
builder comparison returned the same 6,496,211 vertices with two/four/six
workers in 12.247/7.339/5.794 s. These are CPU-side test timings without
Qt publication or scene-graph expansion; final live UI and working-set
measurements remain necessary before claiming the cross-platform latency
gap is closed. Higher-core Windows machines may use eight builders and need
separate memory validation.

## Follow-up: avoid invisible detail fill work (2026-10-05)

The three-layer Windows detail view at semantic LOD 5 took 48.931 s to
complete 135 windows. Its scheduler was configured for six workers on a
12-logical-CPU host, but detailed read-only windows were still gated at two
concurrent builds to bound decoded geometry and GEOS memory. The render log
did not expose this distinction. Report both `workers` and `window_limit` in
each request so a future trace makes the active window limit clear.

The same frame generated 682,491 cadastral fill vertices with zero alpha.
Those triangles are invisible yet require fill-capacity estimation, GEOS
triangulation, clipping, and scene-graph transfer. Skip fill generation when
the effective fill color alpha is zero. Keep outline generation and source
geometry unchanged. A later opacity/style change rebuilds the fill mesh,
which is covered by materialized and read-only regression tests. The change
applies only to a fully invisible fill, not the user's partially transparent
fill settings. The exact three-layer Windows improvement remains to be
measured in the live app before claiming a speedup.

The user-provided three-layer `TEST.gogis` exposed a larger detail-view
bottleneck. With the app's former 100,000-feature QIX threshold, the 208,015
parcel SHP was indexed but the 77,352-feature building SHP and 11,971-feature
survey-point SHP were not. Stage profiling showed building queries taking
roughly 1-2 s per window while fill generation took milliseconds. On 24
representative detail windows, the CPU-side build took 9.142 s after invisible
fills were omitted. Applying the existing cache-mode QIX policy at 10,000
features indexed all three sources and reduced that same 24-window build to
0.211 s with the same 100,240 vertices and zero invisible fills. Lower the
default viewport-read-only SHP index threshold to 10,000; preserve CLI and
environment overrides. First use may copy SHP components and create the
temporary index, adding startup time and disk use; subsequent opens reuse the
fingerprinted cache. This test does not include Qt scene-graph publication,
and the full three-layer live timing still requires validation.
With the new default and the reused QIX caches, all 135 detail windows in the
same workspace completed CPU-side in 1.883 s, producing 759,607 vertices,
including the unchanged 132,429 visible building-fill vertices and no
zero-alpha fill vertices. The previous live 48.931 s included Qt publication,
so it is not a like-for-like speedup ratio; a new live run remains necessary.
