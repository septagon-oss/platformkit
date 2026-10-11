// Package plantedliterals spells three shared names in the shapes a caller
// reaches for and the census's patterns do not look for: a whole name in one
// literal, the namespace literal instead of the constant, and a join written out
// with different operands. The file is the census's counter-example — see
// census_literal_spellings_test.go. go build and go vet ignore testdata, and the
// tree scan skips it, so this is never compiled into anything.
package plantedliterals

import (
	"strings"

	"github.com/google/uuid"
)

// sessionCookie is the session cookie name spelled whole.
const sessionCookie = "__Host-session"

// CookieName returns it.
func CookieName() string { return sessionCookie }

// Subject forms an event address from the namespace literal rather than from
// transport.SubjectPrefix.
func Subject(tenant uuid.UUID) string {
	return "platformkit." + tenant.String() + ".billing.plan.created"
}

// Durable forms a consumer name from app, module and event with its own join.
func Durable(app, module, event string) string {
	return strings.Join([]string{app, module, strings.ReplaceAll(event, ".", "-")}, "-")
}
