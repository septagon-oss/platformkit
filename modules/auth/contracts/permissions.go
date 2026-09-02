package contracts

// This module declares no permissions, and the absence is a decision.
//
// Its four routes are Public (login, and the two OIDC legs) or SignedIn (logout
// and me): every one of them is about the caller themselves, where there is no
// resource to name a permission on. What this module contributes to
// authorization is the answer, not the question — it implements
// httpx.Authorizer, so the permissions every other module declares are resolved
// against the roles table here.
//
// The roles themselves have no management routes yet. They are seeded per tenant
// (Service.SeedRoles) and edited in the database; a roles screen is E3.2's, and
// the permission that guards it belongs in the commit that adds it.
