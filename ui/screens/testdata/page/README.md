# Page fixtures

Each `*.json` here is one Page expectation beside a reviewed parity case in `../parity/`: the
same registration and the same rows, plus the presentation states a shell may ask that
registration about, and the Page each state answers. `kit/entity/view.Build` reads these files in
`ui/screens`; a client in another language reads the same files and must reach the same document.

```jsonc
{
  "name": "en-task",                    // = the parity case it is about, and = the file name
  "provenance": {
    "author": "…",                      // non-empty; "" is refused
    "date": "2026-10-25",               // YYYY-MM-DD, UTC
    "inputHash": "sha256:…",            // of "input", see below
    "expectedHash": "sha256:…"          // of "expected"
  },
  "input": {
    …the parity case's "input" members, unchanged…,
    "state": [ { "name": "list", "kind": "list", "now": "2026-10-25T08:00:00Z",
                 "home": "Workspace", "writable": true, "window": { "offset": 0, "limit": 50 },
                 "sort": "", "total": 1, "counted": true, "violations": {}, "detail": "",
                 "create": false, "formTitle": "" } ]
  },
  "expected": { "page": { "list": { …the Page JSON…, or { "refused": "operations" } } } }
}
```

- The case's own provenance is its own: the reviewed case's `author` and `date` stay in
  `../parity/`. The link is derived, not copied — `input` without its `state` member must hash
  to that case's `inputHash` under `../parity/README.md`'s canonical hash rule — so a page case
  cannot quietly disagree with the case it is about and no reviewed hash is rewritten.
- `state[]` is the presentation state: `kind` is required; `now` is the instant the page is
  stamped `builtAt`, so a case never moves on its own; `writable` defaults to true; `window`
  defaults to `{offset: 0, limit: 50}`; `total` and `counted` default to the rows it carries
  and `true`; everything else defaults to nothing.
- `expected.page` is keyed by the state's `name`, so map order never enters a hash. Each
  entry is the Page the builder writes for that state, compared as a subset of what the case
  is about. A state marked `"whole": true` is compared as the exact marshalled bytes instead:
  member order, key presence, the null discipline and every value spelling are pinned there.
- The rows reach `view.Build` decoded with numbers kept whole, as the platform's own reader
  decodes a document, so an integer stays an integer and a row's write counter survives the
  trip. `{"refused": "<member>"}` is an expectation too, and the honest one where no route or no
  row exists: a commands-only resource has no page, a singleton is never a list, and a record
  page from no row is a fiction.
- These expectations are the builder's own bytes for these inputs, kept under a hash so any
  later change is visible as a hash change, and marked `"author": "platform worker,
  unreviewed"` because no person has reviewed them yet.
- Five cases name no parity case — `large-integers`, `money-minor-units`, `null-false-empty`,
  `instant-and-date`, `undeclared-enum-value` — because they hold the value domain itself, both
  signs: their `input` is their own, so their `inputHash` is the hash of that input alone.
- A Page fixture states only what JSON can carry. A number with no JSON spelling
  (`NaN`, `±Inf`) and a calendar day cut in a zone the state does not name are pinned
  in `kit/entity/view`'s own value cases, which hand the builder both directly.
