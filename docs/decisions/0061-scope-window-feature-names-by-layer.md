# Scope window feature names by layer

## Context

Read-only viewport chunks retain small feature names for current-window attribute lookup and selection. The name map was keyed only by numeric feature ID even though IDs are unique only within a source layer. Loading multiple files with the same IDs could overwrite a name, and releasing one layer's offscreen chunk could delete the other layer's visible name.

## Decision

Key retained names by both project layer name and feature ID. Insert and look up through that pair. When a chunk leaves the viewport, remove only names for its own layer and IDs. The existing per-chunk ID slices and payload budgets still bound the retained state.

## Verification

A native Qt regression seeds two visible layers with feature ID 7, removes one layer's chunk, and confirms the other layer's name remains available through the attribute reader. Existing resource-release and runtime-replacement tests cover the same map's lifecycle.
