# Module planning

`kit/moduleplan` checks caller-owned module definitions and explicit selections
before constructors run. Import it from the PlatformKit Go module; it depends
only on the standard library and needs no runtime, configuration or services.

```go
package main

import (
    "fmt"

    "github.com/septagon-oss/platformkit/kit/moduleplan"
)

func main() {
    order := []string{"identity", "files", "review"}
    requirements := map[string][]string{
        "identity": nil, "files": {"identity"}, "review": {"files"},
    }
    selected, err := moduleplan.Select(order, requirements,
        []string{"identity"}, []string{"review", "identity", "files"})
    if err != nil {
        panic(err)
    }
    fmt.Println(selected) // [identity files review]
}
```

[Validate](plan.go) requires every definition exactly once in construction order,
with each dependency before its consumer. `Select` also requires every mandatory
module and dependency explicitly in the selection. It returns a fresh slice in
construction order; failure returns no selection. Neither function modifies or
retains caller inputs or enables missing modules. Empty definitions and selections
are valid. Names are caller-owned strings; runtime naming rules remain separate.

The caller supplies the order and requirements; this package neither derives an
order nor verifies constructors against their declared dependencies.
[Manifest validation](../module/module.go) checks permissions, events, jobs and
navigation after typed constructors compose the application. Product provider
policies and configuration validation stay with the caller. A valid selection
does not establish a working application.

Run `go test ./kit/moduleplan` from the repository root to exercise the existing
[behavior cases](plan_test.go). The package is part of the existing Go module;
there is no separate `go.mod`, registry or dependency-discovery service.
