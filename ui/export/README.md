# Design export

`ui/export` produces the artefacts design tools and the admin gallery read
from the same composition a shell serves. `Export(theme, examples, extra...)`
is the content-addressed snapshot — palette, glyphs, stylesheet and every
captured example's Props, schema, slots and observed HTML — and
`ExportWithLayout` adds the source layout declarations. `ExportTokens` and
`TokenExport.DTCG` project the scales into the DTCG interchange format.
`ProjectProps` and `ProjectReplacement` apply a typed proposal to a capture
and return the projected snapshot; [ui/source](../source/source.go) persists
an accepted one into Go. `Storybook` is one application's authorized
composition, which [modules/admin](../../modules/admin/README.md) serves at
`/app/admin/_gallery`.

Nothing here is needed to serve a page: `ui/page` depends on [ui](../ui.go),
and the package gate refuses this package in its closure. Start with
[tools/designexport](../../tools/designexport/README.md) for the command line
and `go test ./ui/export` for the snapshot, token and proposal contracts.
