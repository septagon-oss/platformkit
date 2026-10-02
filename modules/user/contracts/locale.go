package contracts

import (
	"context"
	"errors"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// LocalePolicy answers which languages the tenant of this transaction is served
// in, right now, from the row that declares them.
//
// The set is another module's fact — the tenant module stores it and the operator
// declares it — so this module asks instead of reading a table it does not own.
// `modules/tenant`'s `contracts.Service` answers this interface as it stands;
// composition hands one to the other and neither imports the other.
//
// The answer is read under a lock on the tenant's row. A form's select box is a
// picture of the set as the page was drawn; the command needs the set as it is at
// the moment the write lands, because an operator can narrow it while that page
// sits open, and a choice nobody serves is a preference that silently stops
// meaning anything.
type LocalePolicy interface {
	CurrentLanguages(ctx context.Context, tx db.Tx[db.Tenant]) (tenancy.Languages, error)
}

// ErrNoSignIn is what the account commands answer when the transaction carries no
// person. It is not a caller's mistake to repair through the body: the door is a
// signed-in one and a command that cannot say who is asking writes nothing, so it
// publishes nothing either. The route that mounts the command answers it as a 401;
// the command itself only refuses.
var ErrNoSignIn = errors.New("user: no person is signed in for this transaction")

// LocalePolicyFunc adapts a function to LocalePolicy, the way AdministrationFunc
// adapts one to Administration. Composition with a tenant module passes that
// module's Service directly; a test that has no tenant table passes the set it
// wants offered and nothing else.
//
// An adapter with no Ask is refused the way the module refuses the other two: it
// would answer "this tenant serves no language" — which looks like a narrow set
// and behaves like a door that refuses every choice — so it panics at the call and
// errors at the method, and nothing is inferred from its absence.
type LocalePolicyFunc struct {
	Ask func(ctx context.Context, tx db.Tx[db.Tenant]) (tenancy.Languages, error)
}

func (f *LocalePolicyFunc) CurrentLanguages(ctx context.Context, tx db.Tx[db.Tenant]) (tenancy.Languages, error) {
	if f == nil || f.Ask == nil {
		return tenancy.Languages{}, errors.New("user: LocalePolicyFunc has nothing to ask")
	}
	return f.Ask(ctx, tx)
}

// Language is one person's answer to "which language should this be written in",
// with the revision it is held at.
//
// It is a type and not the two fields on a User row because it is what the account
// door reads and writes: the choice, plus the number that makes a second write from
// a stale page a conflict rather than a loss. A user body carries no revision — a
// caller could pick one — so the pair travels on its own, and the two routes that
// speak it answer with it and nothing else.
type Language struct {
	// Locale is the chosen tag, empty for "nobody chose: negotiate".
	Locale string `json:"language" maxLength:"35" doc:"Language this person chose, empty while the browser and the tenant decide" example:"pt-PT"`
	// Revision is what to send back as expectedRevision. It moves once per accepted
	// change, and a change that changes nothing leaves it where it was.
	Revision int64 `json:"revision" doc:"Revision this choice is held at; send it back as expectedRevision" example:"2"`
}
