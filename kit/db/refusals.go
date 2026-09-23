package db

import "fmt"

// These four refusals are named exactly as the rules are, and the name is printed inside
// the sentence. A rule is refused from a table an operator can read and, where it
// judges, except; these are refused because of a fact of *this* database — a table that
// is not here, a key the window cannot walk, an expansion the ledger has not seen, a
// drain past the bound a migration gives itself — and none of them is exceptable, because
// a marker could no more excuse a missing table than a contract half's own expand. The
// word in front of them is therefore `refusal` and not `rule`: `rule` belongs to the
// table a file may answer with `allow=`, and the two vocabularies must not be confused by
// a prefix.
//
// The id is printed because it is quoted: a runbook, a log line or a review that names
// one has to lead somewhere in the repository that refused it. An id nothing prints is a
// second, unenforced name for the same refusal, and it drifts from the sentence it stands
// for with no gate noticing — so the code that refuses prints the id, the table in
// README.md names it, and kit/db/refusal_names_test.go walks each one through the door
// that refuses it and reads it back off the message.
const (
	refusalTableMissing     = "data-table-missing"
	refusalKeyNotPrimaryKey = "data-key-not-primary-key"
	// refusalMissingExpansion is the release rule — the contract half whose `expand=`
	// version is not in the installation's history yet — refused after the ledger is read
	// rather than from the file's text, which is why it is here and not in the rule table.
	refusalMissingExpansion = "contract-without-expansion"
	// refusalBackfillBudget is the report of a drain a migration ran for itself and
	// could not finish inside the bound; it travels as ErrBackfillBudget, so a script
	// matches it with errors.Is and reads the id off the same sentence.
	refusalBackfillBudget = "backfill-exceeds-install-budget"
)

// refusal is one of the ids above with its sentence, in the shape the rule table uses for
// its own: name first, so the thing an operator greps for is the first thing the line
// says, then what was found and what to do about it.
func refusal(id, found, instead string) error {
	return fmt.Errorf("refusal %s: %s; %s", id, found, instead)
}
