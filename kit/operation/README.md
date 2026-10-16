# Portable operation values

`Identity` names an admitted intent independently of its calls, events and actor.
Its ID is a case-sensitive token; its reference distinguishes occurrences when
scope or retention permits a token to be reused. References and tenant/person
identifiers are UUID strings parsed by the storage and transport boundaries.
The empty outcome means unknown; the three known `Outcome` values are protocol
values rather than translated labels.

This package uses only the standard library. `kit/request` aliases its values
and owns execution context; event transports carry them without importing the
request's trace context or any database adapter. [The receipt owner](../idempotency/README.md)
explains admission, completion, authorization and retention.

## Composition

**Reused** — the token grammar shared by request replay and event metadata.
**Added** — portable operation values because importing request context into a
transport would also import request trace ownership.
**Made reusable** — one identity, outcome vocabulary and token validator for
request, outbox, wire and future completion consumers.
