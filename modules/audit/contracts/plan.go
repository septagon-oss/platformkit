package contracts

// Plan is the plan feature the audit trail is sold under, empty for an
// installation that gives it to everybody.
//
// It is a contract and not a Deps string because which features a product sells
// is the composition's decision and never the module's (see Deps.Feature's own
// reason), and a contract is keyed by a Go type: a bare `string` need would be
// answered by whichever module happened to put one, and Explain could not say
// whose answer it was printing.
//
// A composition that sells the trail to everybody composes no provider, and the
// routes stop asking: the same answer an empty Deps.Feature gives today.
type Plan string
