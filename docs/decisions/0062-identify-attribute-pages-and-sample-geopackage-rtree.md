# Identify attribute pages and sample GeoPackage envelopes through the RTree

## Context

The layer properties Lua editor reused the global attribute page payload for field suggestions. A page requested for a previous layer could arrive after another layer's properties opened, so its fields appeared under the new layer's title. The payload did not identify its source layer.

For a 10,000,001-feature GeoPackage, 256 evenly spaced `SetNextByIndex` calls took about 14.7 seconds during initial map fitting, despite the driver's fast random positioning capability. The GeoPackage had a spatial RTree.

## Decision

Include the project layer name in every attribute page payload. The QML synchronizer accepts fields and rows only when the payload's layer matches the open properties dialog or active attribute table. Clear the previous hints and rows on layer changes.

For GeoPackage envelope sampling, find each target ID through the feature table's primary key index and read its bounds by exact ID from the `gpkg_geometry_columns` RTree. An RTree range scan was measured to be too slow and discarded. Close each SQL result and feature before the next probe. If the RTree is unavailable, return no optional samples rather than walking millions of features during project load. Other drivers keep their existing sampling paths.

## Verification

QML regression switches layers and delivers the old layer's page first; all 77 QML tests pass. A native GDAL test samples a selected layer in a multi-layer GeoPackage and checks each returned envelope. On a synthetic 10,000,001-point GeoPackage, 256 samples fell from about 14.7 seconds with `SetNextByIndex` to 602 ms through the primary key and RTree; read-only runtime loading fell from about 14.6 seconds to 357 ms. The full 1,024-chunk overview took 15.7 seconds in the latter run. An isolated loader process peaked at 65 MiB RSS. The fixture is generated outside the repository.
