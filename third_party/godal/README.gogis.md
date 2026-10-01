# Local godal patch

This directory is based on `github.com/airbusgeo/godal v0.0.18` and retains its
Apache-2.0 license and copyright headers. Local additions in `godal.go` expose
`Geometry.WKBSize()`, bounded `Geometry.WKBWithMaxSize()` export, and
`Feature.FieldsWithMaxTotalValueBytes()`. These let callers reject oversized
geometry and attribute values before allocating Go-side copies. The WKB size
check in `godal.cpp` occurs before native output-buffer allocation. Upstream
module metadata and sources are retained so the patch can be compared and
refreshed deliberately.
