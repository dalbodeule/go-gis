# ADR 0005: Lua predicate filters and composed labels

- Status: implemented
- Date: 2026-10-01

## API semantics

- `gogis.filter_lua(source, result, predicate)` selects features whose predicate
  returns a boolean `true`. Each call adds a new result layer, so scripts can
  chain filters by using the previous result as the next source.
- `gogis.label_lua(source, result, textScript[, ruleScript, height, style])`
  derives label text from any feature attributes. A false rule, `nil`, or empty
  text suppresses only the label; it does not remove the feature from the result.
- Both commands calculate before opening the project edit transaction. A Lua
  runtime error or invalid return type therefore cannot publish a partial layer.
- Lua receives a read-only `feature` property view; `rawset` is unavailable and
  `table.insert/remove/sort` reject that proxy while remaining usable on local
  tables. Scripts have no filesystem, process, module, coroutine, or debug
  access and observe context cancellation.

The desktop label renderer uses the same compiled composer for rule and text,
so a feature requires one Lua VM call and one attribute-view update rather than
separate rule and text evaluations. The Lua editor is a separate window with
keyword/string/comment highlighting, schema-derived field/type hints, and
quoted access insertion for field names containing spaces. Highlight rendering
escapes source tokens in batches and joins output fragments once, avoiding
per-character HTML string accumulation; it recognizes Lua long-bracket strings
and comments as well as quoted strings and line comments. Keyword/API lookup
maps are retained per editor, and the HTML escape callback is reused during each
debounced full-source pass. QML tests cover escaping, multi-line numbering,
long-bracket syntax, and a 60 KB script near the runtime source limit.

The layer properties Lua editor also stores a separate feature display rule.
It runs against each feature in the render snapshot and must return a boolean.
Returning `false` hides that feature from rendering and map hit testing without
changing the source layer or saved project features. Label visibility rules
remain independent and only suppress labels. In materialized layers, label and
display-rule changes rebuild the render source; in viewport-backed layers, the
affected cached windows are discarded and rebuilt with the new settings.

## Measurements

Apple M3 rerun on 2026-10-01, 10K synthetic features, three runs with
`-benchmem -benchtime=2s -count=3`:

| Path | Time / 10K | Bytes / 10K | Allocations / 10K |
| --- | ---: | ---: | ---: |
| Separate text and rule programs | 22.42–22.90 ms | 56.89 MB | 287,646–287,647 |
| Combined label composer | 9.35–9.41 ms | 0.99 MB | 73,752–73,753 |
| Lua filter predicate + result layer | 4.68–4.72 ms | 1.14 MB | 28,766 |
| Text program only | 14.33–14.60 ms | 28.88 MB | 178,824–178,828 |
| Rule program only | 7.64–7.79 ms | 27.99 MB | 108,750 |

The latest three-run measurements show about 58% lower time, 98% fewer
allocated bytes, and 74% fewer allocations for the combined label pipeline
versus separately evaluating text and rule programs. Filter measurements
include predicate evaluation and construction of the in-memory result layer.
Lua evaluation now uses GopherLua's normal VM loop for contexts whose `Done`
channel is nil; cancelable contexts still use instruction-level cancellation.
Compared with the preceding three-run baseline, this measured about 3–6% lower
time for the combined label and filter paths, with unchanged allocations. These
are synthetic microbenchmarks on one Apple M3 host and do not represent
end-to-end desktop render timing or establish cross-platform performance.

## Outstanding external validation

`scripts/verify-ares-precheck.sh` generates UTF-8 and CP949 DXF profiles and
passes GDAL round-trip checks. ARES Commander 2027 visual/font/save validation
still requires the target ARES Commander application on a supported OS and is tracked in
`docs/verification/ares-commander.md`.
