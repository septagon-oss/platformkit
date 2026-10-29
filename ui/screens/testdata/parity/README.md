# Parity fixtures

Each `*.json` here is one reviewed case: a registration as the catalogue holds
it, some rows, and the semantic model every client must reach from it. Go's
`present.Resolve` reads these files in `ui/screens/parity_test.go`; a client in
another language reads the same files.

```jsonc
{
  "name": "enum-fallbacks",              // = file name without .json
  "provenance": {
    "author": "name of the reviewer",    // non-empty
    "date": "2026-10-23",                // YYYY-MM-DD, UTC
    "inputHash": "sha256:…",             // of "input", see below
    "expectedHash": "sha256:…"           // of "expected"
  },
  "input": {
    "language": "pt-PT",                 // the tag the strings were resolved for
    "schema": { },                       // entity.Schema, wire form
    "hints": { },                        // entity.EntryHints (Go field names)
    "immutable": [], "singleton": false, "commandsOnly": false,
    "operations": ["list", "read"],      // absent = all five
    "commands": [ ],                     // present.CommandInput: the commands the viewer may call
    "translations": { "present.true": "Sim" },  // the Text seam, key -> words
    "rows": [ { "id": "r1" } ]           // record values as the API returns them
  },
  "expected": {
    "resolved": { },                     // the part of present.Resolved the case is about
    "rows": { "<id>": { "listTitle": "…", "recordTitle": "…", "cells": { "<field>": "text" } } }
  }
}
```

- `expected.resolved` is compared as a subset: every key written must equal what
  the resolver prints, arrays by length and element. A case states what it is
  about and nothing else.
- Hash rule: `sha256` of the compact JSON of the member after decoding to `any`
  and re-encoding with `encoding/json` (keys sorted, HTML escaping off), written
  `sha256:<hex>`.
- The test refuses a fixture whose `inputHash` or `expectedHash` differs from the
  computed one, whose `author` or `date` is empty, or whose name differs from its
  file. Re-review, then rewrite the hashes with the reviewer's name.
- `expected` is written by hand and reviewed; it is never produced by running the
  resolver. A fixture update is a review.

## Web assertions

[policy_html_test.go](../../policy_html_test.go) renders each fixture through
`ui/resource` and parses semantic HTML. [web/expectations.json](web/expectations.json)
adds hand-written column labels, sort labels and explicitly synthetic probes for
values the original fixture leaves unspecified. Each companion pins the reviewed
source input/expected hashes. Its author and pending independent review are stated
separately; it does not replace or claim ownership of the reviewed corpus. The
corpus is shared, so the test names each fixture whose HTML it leaves unasserted
and refuses any other gap, or a companion naming no fixture of the corpus.

The tests compare titles, membership, field and option order, command copy/flags,
query values, pill tones and picture references. Supplemental value, mounted-query
and browser tests cover clocks, exact numbers, authorization and rejected input;
the original 17 fixtures are not claimed to cover those cases by themselves.

`sketch-en` and `sketch-pt-pt` pin the sketch control/kind and localized label.
Their plain-text cell is a dash; SVG geometry is checked in the component suite.
The catalogue golden's reflected drawing fields land before the phone parser and
its fixture/provenance refresh. An older parser rejecting `sketch` is incompatible.
