# Problem responses

`kit/problem` owns the RFC 9457 response representation and its private error
cause. It imports only the standard library and can be used without an HTTP
framework:

```go
p := problem.FromError(http.StatusServiceUnavailable, cause)
body, err := json.Marshal(p)
```

`FromError` retains the status and standard title, omits detail and validation
messages, and exposes the original cause through `errors.Is` and `errors.Unwrap`.
The cause is never serialized. Any status is accepted; unknown statuses use
`HTTP <status>` as their title. `New(status, detail)` remains the constructor for
deliberate public detail, with `NotFound` and `Conflict` conveniences.

The [Huma provider](providers/huma/huma.go) adapts framework errors to the same
`Problem` type. Configure it once where an API is constructed:

```go
import problemhuma "github.com/septagon-oss/platformkit/kit/problem/providers/huma"

huma.NewError = problemhuma.NewError
```

This replaces the removed `problem.HumaError` hook. PlatformKit's `kit/httpx`
already uses the new provider. Applications assigning the old hook should
change that import and assignment; the provider preserves statuses, public
validation messages, private causes and omission of submitted field values.
Core-only consumers need no Huma dependency. Provider consumers import Huma and
the shared core; the core does not import its provider.
