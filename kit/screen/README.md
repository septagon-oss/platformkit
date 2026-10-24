# Published screen contracts

`kit/screen` owns immutable view and anchor values, their canonical snapshot,
and compatibility checks. It imports only the standard library. References
identify controls; they grant no authority and execute no operation.

## Composition

**Reused** — `pkit.NewApp`, `kit/app` dry route registration, `rest.Spec`,
`kit/entity/present`, `ui/resource`, `ui/forms` and `ui/page` supply composition,
resolved controls and their actual rendering.
**Added** — immutable descriptors, route validation, a hashed snapshot and typed
references connect those owners; the existing resource catalogue describes a
person's available resources and cannot describe every custom screen.
**Made reusable** — any composed application can call `App.Screens`, and a
consumer can resolve a `ViewRef` or `AnchorRef` without selectors or route guesses.

## Names and bindings

A generic view is `app/<owner>/resource/<entity>/<kind>`, where kind is `list`,
`detail`, `create`, `edit` or `command/<verb>`. A custom view is
`<surface>/<owner>/custom/<name>`. Surface is `app`, `public` or `ops`.
Names encode UTF-8 bytes with uppercase percent escapes; only letters, digits,
underscore and hyphen remain literal. Empty names and control characters fail.

Anchors append `#action.<name>`, `#field.<path>` or `#command.<verb>`.
Field path components are separate: `field.values.title` and
`field.values%2Etitle` name different controls. `ParseView` and `ParseAnchor`
refuse noncanonical encodings instead of normalizing them.

An anchor declares its visible control kind, supported `Activated`, `Committed`
and `Submitted` interactions, and a binding. Navigation targets a view; value
bindings name an operation and field path; submission targets an operation;
display bindings identify read-only data. Fixed form/query inputs participate
in identity, including a review button's verdict. These are capabilities to
observe, not claims that a domain command succeeded.

Generic descriptors and markup consume resolved presentation controls.
Permission filtering removes a control and its anchor together. Structural
publication has no actor or tenant and includes all composed controls. Custom
owners return fresh descriptor values from their `contracts` package, put them
in `module.Module.Views`, and bind each `page.Route.View` to its real response.
Redirects, protocol handlers, exports and isolated previews declare a reason
instead of a screen. Static asset mounts are not screen operations.

`Publish` validates unique names, owners, routes, host views and operation
targets, then sorts and copies the inventory. Repeated rows cannot publish
anchors because version one has no typed instance binding.

## Artifact and generation

The reference composition's reviewed artifact is
[screens.json](../../ui/screens/testdata/screens.json), beside the resource
catalogue. It carries schema version 1, the presentation policy version,
SHA-256 identities and a SHA-256 content hash. JSON is canonical, sorted,
two-space indented and ends with a newline. No record, grant result or clock
enters the artifact. A retained reference cannot change its semantic binding;
the exporter checks both the local artifact and the merge-base artifact.

After reviewing an intentional structural change, run from the repository root:

```sh
go run ./apps/platformkit screens -config apps/platformkit/testdata/composition.yaml -update
go run ./tools/screenrefs
```

The exporter mounts development and production through the actual composition
and requires equal contracts. Without `-update` it only checks. `make generate`
checks the artifact and writes the typed reference constants; `make
check-generated` checks references before generation, refusing missing or edited
files without repairing them. The handwritten descriptor
API does not depend on generated names. `ReferenceContractHash` records which
artifact generated the constants.

`make check-screen-render` uses the existing browser runner to check exact
anchor counts, visibility, positive bounds and permission projection. It is
part of `make check`; install the [browser prerequisites](../../CONTRIBUTING.md#verify-at-the-relevant-boundary).

## Limits

This contract supplies screen identity and supported interactions. Tour copy,
eligibility, progress, persistence, scheduling and completion belong to their
later owners. Native rendering and repeated-instance anchors are not provided.
Control availability on a particular response still depends on that request's
grants and state; absence is not permission to guess a fallback selector.
