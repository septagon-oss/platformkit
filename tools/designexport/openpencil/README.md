# OpenPencil adapter tooling

This directory owns the native boundary of the existing
[design export](../../../README.md#build-a-screen). The Go tokens, glyphs,
typed examples and stylesheet remain the source of truth. There is no second
component registry, page language or client-specific library here.

The implemented increment is a pinned SDK correction layer and native
conformance suite. It does not yet convert the export to a library, build the
editor or deploy anything. A passing native test is not a production release.

## Run the native checks

Use Node 24 or newer. From this directory, install the locked dependencies and
run the tests:

```sh
npm ci --ignore-scripts
npm test
```

The tests create disposable scene graphs and FIG buffers in memory. They do
not open your documents, connect to an editor or write generated design files.
CI and the tagged-tree workflow run the same command. `make check` remains the
Go and repository-policy gate; this suite is an additional native boundary.

`npm run test:stock` deliberately omits the corrections. It reproduces the
upstream native failures and is expected to exit nonzero; it is not a release
gate that should be made green by removing assertions.

## Understand the correction boundary

[register.mjs](register.mjs) checks the SDK versions and installs a process-local
Node loader. [corrections.mjs](corrections.mjs) checks the SHA-256 of each affected
upstream module before transforming it. A changed or already-corrected source
is an error. Installed dependencies remain untouched. The same source transform
can be used at a browser build boundary, but that integration is unfinished.

The correction uses native component/property identities and FIG override
paths, not plugin metadata. Repeated saves merge overrides by path. Reflow and
undo preserve the linked component relationship; they do not detach instances.
The conformance fixtures use a deterministic text measurer, so their exact
dimensions demonstrate layout state rather than real font shaping.
Editor fixtures use the normal graph subscriptions and public history actions.
Assertions cover the state after deferred notifications settle, not just the
immediate return from undo. Genuine authored master changes still propagate;
the property operation pauses component synchronization only while computing or
restoring its captured geometry. Ordinary authored parent resizing still
propagates new master dimensions to instances. Failed text measurement restores
grid sizing modes and explicitly frees the successfully built Yoga trees.

Supported behavior is deliberately narrower than the editor's entire API:
single-target, placed-instance text properties, their nested references, native
save/reopen, and exact undo/redo of the affected layout state. Identity
correspondence must be unambiguous. Editing inside a component master, instance
swaps, variant replacement and arbitrary imported documents require further conformance
before they can be advertised as supported. Unsupported cases must not be
silently treated as passing replacement contracts.
The reordered-instance tests prove that labels retain the correct identity;
they do not prove persistence of per-instance child order. The SDK currently
rehydrates children in master order.

## Release blockers

The browser build still needs these corrections, actual font/CanvasKit tests,
SVG vector fidelity, responsive and accessibility checks, and save/reopen in
the interactive editor. The snapshot converter and source-freshness gate are
unfinished. Product prototypes need actual runtime-state mappings and governed
end-to-end persistence tests in their owning product repository.
Programmatic batches with an unrelated master update already queued before a
property edit need further lifecycle work: wait for that preceding update to
settle before using the verified edit/history boundary. The current correction
does not make overlapping authored-change batches transactional.
Likewise, property-driven layout that resizes a neighboring master requires
downstream instance synchronization and history that are not implemented yet.
The edit is rolled back with an explicit error; it is not counted as supported
composition. This temporary guard prevents stale external instances while that
cross-component behavior remains a release requirement.

Run `npm audit --omit=dev --audit-level=high` before considering a native-tooling
or editor release. The 5 September 2026 audit fails with four high-severity
affected packages: `expr-eval` through the SDK and `image-size` through
`pptxgenjs`, with their dependent packages also counted. This is an unresolved
security gate, not an accepted exception. Do not run `npm audit fix --force`:
its proposed SDK downgrade invalidates the pinned corrections. Dependency
remediation and reachable browser-surface verification remain required.
The narrow SDK imports used by these tests do not load those vulnerable
packages; the import-boundary test guards that observation. It does not waive
the audit failure or establish safety for the broader editor and its tools.

Nothing in this directory changes the deployed editor or authorizes release.
The reference Go application's container does not install these dependencies.
OpenPencil attribution and license terms are recorded in the root
[NOTICE](../../../NOTICE).
