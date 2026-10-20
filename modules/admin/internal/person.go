package internal

// person.go is how the frame names the person who is looking at it.
//
// tenancy.Principal carries an id and roles and must stay that way — every service
// in the kernel passes that value, and a display name on it would be a fact about a
// row carried by a type that has no row. So the name is reached the way this shell
// reaches every other cross-module fact it draws: a port this narrow, on the Shell,
// implemented at composition, exactly as Storybook and Locale are — a function of
// the request's context, with the transaction that context carries resolved by
// whoever owns the request.

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// Person is the two things a screen may call a caller by.
type Person struct {
	DisplayName string
	Email       string
}

// People is what the frame needs of the user module: one read of the caller's own
// account row, for the header line that names them.
//
// It is asked inside the request's own transaction, which is the tenant's, so the
// row Postgres resolves is the caller's own and a user of another tenant is simply
// not there. That is why this port takes no tenant argument and why no branch on a
// tenant is written in this package: row-level security has already answered the
// question, and answering it twice is how the two answers drift.
//
// The context is the request's, and the implementation reads the transaction off it
// — the same shape Deps.Storybook and Deps.Locale have, for the same reason: a
// renderer has no business opening anything, and the fake that has no database and
// the service that has one then answer the same question in the same way.
type People interface {
	Person(ctx context.Context, userID uuid.UUID) (Person, error)
}

// caller is who the header names: the display name this person chose, or the
// address they sign in with, or — when neither is known — the plain fact that
// somebody is signed in.
//
// Never an id fragment. "8258e4cc · admin" is what a machine knows this account by,
// printed where a greeting should be, and the id is of no use to the person reading
// the page. Every failure path — no port composed, no transaction on the request,
// the row gone, the read refused — answers "Signed in", because a header cannot fail
// a page and a fact about the session is the only thing still known. That is also
// why the roles left this line: the fallback would otherwise read "admin", a role
// standing where a person's name should be.
//
// The read costs one SELECT per signed-in page render, which is the shape the
// dashboard's counts and the roles and sessions screens already accept. There is no
// cache here because the frame has nowhere to keep one; if this is ever measured as
// too expensive, the answer is the fallback path this function already has.
func caller(ctx context.Context, people People, userID uuid.UUID) string {
	if people == nil || userID == uuid.Nil {
		return "Signed in"
	}
	person, err := people.Person(ctx, userID)
	if err != nil {
		return "Signed in"
	}
	return named(person)
}

// named is the formatting rule, in one place: name, else address, else the fact
// that a person is here at all. An empty Person is what a read of a row that is
// gone answers, so the empty case is a real one rather than a defensive branch.
func named(person Person) string {
	if line := strings.TrimSpace(person.DisplayName); line != "" {
		return line
	}
	if line := strings.TrimSpace(person.Email); line != "" {
		return line
	}
	return "Signed in"
}
