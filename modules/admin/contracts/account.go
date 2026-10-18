package contracts

// account.go is the shell's account surface as a port: the two questions the
// header's language control asks of whoever owns people, and the counted form of
// the write. They are declared here rather than in the shell because a port is the
// consumer's declaration of what it needs, and the module that answers it must be
// able to name it without importing the shell.

import (
	"context"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
)

// AccountLanguage is what the shell asks of the module that owns people. It is a
// port and not an import: the user module decides what a language choice means,
// publishes the event that records it, and refuses a tag this tenant does not
// serve. This module reads the answer and passes a form's through.
//
// Choose takes no user id, because the command it reaches reads who is asking from
// the transaction — that is the half that keeps an account setting from becoming a
// way to edit a colleague — and an id handed to the shell would be an id the
// browser wrote.
//
// Preferred answers the stored choice, empty when nobody chose. This port carries no
// expected revision: `Choose` is the intent it reads as written, and two of those
// that arrive together leave the row at whichever landed last, each told what it
// now holds by the row it gets back. The counted door is AccountLanguageVersion —
// the one a form drawn from a read has to reach, and see it for why a second writer
// of this field is ordinary rather than hypothetical. The counter itself and what
// made it worth adding are modules/user/migrations/000073_user_language_revision.up.sql.
type AccountLanguage interface {
	Preferred(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) (locale string, err error)
	Choose(ctx context.Context, tx db.Tx[db.Tenant], tag string) error
}

// AccountLanguageVersion is the account door for a composition whose choice is
// counted: the same read, plus the number the read was taken at, and the same write
// with that number brought back.
//
// It is a second port rather than more methods on AccountLanguage because the two
// doors differ in what a lost write costs. `Choose` is an intent — "answer me in
// Portuguese" — and the loser of two simultaneous intents is told which language it
// now holds by the row it gets back. The control in the header is not an intent: it
// is a picture of what the person's row said when the page was drawn, and a form
// posted from an old picture writes an old choice over a new one. That is the
// difference between a write that changes nothing and a write that quietly takes
// somebody's later choice away, and only the revision decides which happened.
//
// A composition that wires no version here keeps the door it always had: the form
// is drawn without a revision and `Choose` answers an intent, which is what that
// port promises and what its documentation says. What it does not get is a form that
// names a revision — the route refuses one rather than guessing which revision the
// submission meant to bring, so the weaker door is reached only by a caller who
// wrote the POST by hand and is told, in their own language, what is missing.
type AccountLanguageVersion interface {
	// Revision answers what revision this person's choice is held at, to draw into
	// the form and bring back with the next choice. A caller that draws a choice and
	// this number together takes this one first: read the other way round, a change
	// that lands between the two reads leaves the form carrying an old choice at the
	// revision the command will accept, which is a lost write the server agreed to.
	Revision(ctx context.Context, tx db.Tx[db.Tenant]) (int64, error)
	// ChooseAt is Choose at the revision the form was drawn at. A revision the row
	// has left behind is a conflict that writes nothing and publishes nothing.
	ChooseAt(ctx context.Context, tx db.Tx[db.Tenant], tag string, expectedRevision int64) error
}
