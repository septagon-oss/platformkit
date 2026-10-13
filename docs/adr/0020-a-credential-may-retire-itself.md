# 0020: A credential may retire itself, and no other

Status: accepted, 2026-10-13. It extends the bearer-key rule of `modules/auth`
(README, "Bearer tokens for a person's own integrations") and names the one
exception to `httpx.AnyCredential()`'s ban on credential-management doors.

## Problem

A device signs in with a scoped bearer key. When it signs out it should destroy
that key, but every door about a caller's own credentials is `httpx.SignedIn()`,
which refuses a scoped credential outright, and `tenancy.Principal` did not say
which key authenticated the request. A device holding only its key could not
revoke it.

## Decision

1. `tenancy.Principal.TokenID` is the id of the key that authenticated the
   request, set by the bearer resolution and `uuid.Nil` for every other caller.
2. `contracts.Tokens.RevokeOwnToken(ctx, tx, caller tenancy.Principal)` ends the
   row whose id is `caller.TokenID` *and* whose holder is `caller.UserID`. It
   takes a principal, not an id: the only way to put one on a context is the
   identity hook, so the command's input is "who authenticated" and nothing a
   request body could name.
3. `POST /api/v1/auth/tokens/revoke-self` (`auth-token-revoke-self`) declares
   `httpx.AnyCredential()`, takes no input and answers 204. It is the one
   credential command a scoped key may run without a management grant: what it
   spends is the caller's own key, and what it buys is a narrower world. Every
   other credential door stays `SignedIn()`.

## Two replay rules, on purpose

`RevokeToken` answers success for a key that is already revoked: it is a screen
naming a key it read out of a list, "must be stopped" is already true, and a
second event would be a second decision. `RevokeOwnToken` answers
`crud.ErrNotFound` for a key that is revoked or expired: it is a credential
spending itself, the caller's premise is that it holds a live key, and success
would be an assurance that is not true. Over HTTP the case cannot arrive — a dead
key is not recognised and the request never reaches the handler.

## Refusals

- A session (zero `TokenID`): 403 naming the absence of a key; nothing written.
- A dead, foreign or unknown key: not recognised by the kernel, 403
  `AUTH_ANONYMOUS`. Every refusal in `kit/httpx` is a 403, never a 401, and this
  door does not change that.
- In-process, a principal whose pair names no live row: `crud.ErrNotFound`, the
  same answer whether the id exists elsewhere or not, so the command is no
  key-id oracle.

## What pins it

The module's `wholeSurface` row, the published `x-platformkit-auth` in
`apps/platformkit/testdata/openapi.json`, `authtest.RunTokens` against the SQL
service and the fake, and the HTTP cases in
`modules/auth/internal/tokens_self_test.go`, which also re-pin that a scoped key
is refused at every other token door.
