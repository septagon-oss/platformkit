package contracts

// The three permissions a proposal has, and why they are three.
//
// Reading a proposal and putting one forward are different grants because a
// person who may watch is not a person who may flood the queue. Deciding is a
// third, distinct from proposing, so an installation can let a team submit and
// only its reviewers decide.
//
// Neither grant is the four-eyes rule. A grant two people both hold is exactly
// the case that rule is about, so the rule is an actor check — the reviewer and
// the applier must not be the proposer — run inside every command, not a
// permission anybody can hold.
//
// The list the manifest declares is in ../module.go, which keeps kit/module out
// of this package's build graph.
const (
	PermissionChangeRead    = "change:read"
	PermissionChangePropose = "change:propose"
	PermissionChangeDecide  = "change:decide"
)
