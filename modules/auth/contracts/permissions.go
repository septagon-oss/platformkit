package contracts

// PermissionRoleManage guards the two roles routes.
//
// There is no role:read beside it. A list of roles is a list of what everybody
// in the tenant may do, which is the same knowledge as the power to change it
// in every way that matters to an attacker planning one: reading it says which
// role to get into. The modules that split reading from writing — user, task —
// split them because somebody wanted to grant a colleague a look at a profile,
// and nobody has yet wanted to grant a look at the authorization table.
//
// It is not an operator permission. Roles are a tenant's own, edited inside
// that tenant's transaction under its own policy, and a customer deciding what
// their "editor" role grants is exactly the thing they should be able to do.
// What they may not do is name an operator permission in one, which SetRole
// refuses and the kernel would refuse anyway.
//
// The module.Permission value is in ../module.go with the manifest, which keeps
// kit/module out of every consumer's build graph — see modules/task.
const PermissionRoleManage = "role:manage"

// PermissionPasskeySignIn guards the one route that says whether a passkey may
// be the whole sign-in at this tenant — the difference between "this account
// answers with two things" and "this account answers with one thing that cannot
// be phished".
//
// It is not role:manage borrowed for the purpose. modules/auth/module.go:191
// already says borrowing that key for the session list "would be the wrong
// decision twice over", and both halves of that sentence apply harder here: the
// power to edit roles is not the power to change how every account in the tenant
// gets in, and an administrator who hands out the one should not find the other
// in the same grant.
//
// It is an ordinary permission rather than an operator one, for the reason
// migrations/000035_passkeys.up.sql gives for the row it guards: the decision
// belongs to the tenant whose people it changes, and the write happens inside
// that tenant's own transaction under its own policy.
const PermissionPasskeySignIn = "passkey:signin"

// Besides these two, this module's routes declare no permission, and the absence
// is a decision.
//
// Login, the two OIDC legs and the two public halves of the password flow are
// Public: somebody who cannot sign in is the only caller they are for. Logout,
// me and the password change are SignedIn: every one of them is about the
// caller themselves, where there is no resource to name a permission on. What
// this module mostly contributes to authorization is the answer rather than the
// question — it implements httpx.Authorizer, so the permissions every other
// module declares are resolved against the roles table here.
