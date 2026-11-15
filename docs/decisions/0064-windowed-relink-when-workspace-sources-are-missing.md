# Windowed relink when workspace sources are missing

## Context

A workspace moved without its original directory layout can retain three missing SHP references. With no available sources, feature-count preflight reported zero and opened an editable runtime. Relinking a million-feature building SHP then used the editable full-snapshot loader and failed at its 128 MiB source payload limit. The source file itself was valid; a normal large-source open would have used windowed read-only mode.

## Decision

Treat a workspace whose every source is unavailable as having an unknown feature count, so its placeholders open in windowed read-only mode for relinking. A workspace with at least one available small source retains its existing editable behavior. When an already-open editable runtime relinks a source, inspect the replacement count before loading it. If it is large, switch to windowed read-only loading when the other layers are only unavailable placeholders. If other resident editable data exists, report that it must be saved or exported first rather than silently discard it.

The workspace still stores and displays the missing source paths. Relinking remains an explicit user action; the loader does not infer a replacement from similarly named files.

## Verification

Native desktop tests cover a workspace with only missing sources and the existing mixed available/missing workspace case. The moved local Gongju workspace has stale relative SHP paths, while the building SHP exists at a different path under Downloads. The data file is not copied into the repository.
