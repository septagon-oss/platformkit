# Message selection and formatting

Import `github.com/septagon-oss/platformkit/kit/locale` to select a language and
format plain-text messages in a worker, command or web application. The package
has no dependencies outside the standard library and starts no services.

Implement `Messages.Select` and `Formatter.Text`, or explicitly select the
[x/text provider](providers/xtext/README.md). `SelectLocale` checks that selection
returns a content language and formatter; missing or incomplete configuration
panics. Pass preferences in priority order. The provider owns supported languages,
fallback, interpolation and plural rules; message keys remain stable identifiers.
The [executable example](locale_test.go) supplies a custom worker formatter.

Compose messages before use. Providers must support concurrent selections without
process-global state; do not mutate their catalogs while rendering. A formatter
returns text, not trusted HTML. Escape it through the consuming renderer, and
keep recipient lookup, delivery, request headers and persistence with their owners.

Existing `ui/page` names remain aliases or forwarding functions. A standalone
consumer imports this package directly; importing `ui/page` still includes that
package's web integration. Misconfiguration still panics, with text now naming
`locale` or `xtext` instead of `page`. Translation editing, publishing and
deployment remain separate operations; this contract performs none of them.

From the foundation repository, run the service-free checks:

```sh
go test -race ./kit/locale/...
go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./kit/locale
```

The dependency listing should contain only `kit/locale`. The provider's own guide
states its additional dependencies and verification.
