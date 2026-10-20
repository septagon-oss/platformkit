# 0021: Named slices are contract values

Status: accepted.

## Problem

A picture provider supplies one finite `contracts.Templates` declaration set.
The slice contribution rule names the element and allows zero contributors; it
cannot express a required provider of this whole named contract.

## Decision

`pkit.Needs[T]` and `Optional[T]` treat an unnamed slice (`[]E`) as all contributed
`E` values, read through `pkit.All[E]`. A named slice is one typed contract value,
provided with `Provides[T]` and read through `Get[T]`, like any named struct.
Missing required providers and ambiguous providers fail before resources open;
optional missing values remain zero. Aliases retain the rule of the aliased
type. `Contributes[T]` contributes one T and does not imply a provider.

`modules/picture` consumes the named declaration set; `tenantcontracts.Languages`
keeps its existing struct contract. No existing need of a named slice predates the
picture consumer in this tree. `pkit/named_slice_test.go` pins required-provider
refusal and whole-value delivery; existing contribution cases pin `[]E` behavior.
