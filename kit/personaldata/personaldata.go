// Package personaldata is the one contract by which this application answers for
// a natural person: which of its own tables name them, what of theirs it would
// hand back, and what it does to those rows when asked to stop holding them.
//
// It is in the kernel and not a module because the manifest speaks to it before
// any module is built (ADR 0013). A person's rows sit in eight owners' tables and
// house rule 3 forbids one reading another's, so "this person, everywhere" is
// answered by a contract each owner implements for its own rows and one module
// that composes them.
//
// The operations are not symmetric: Export is a read that publishes nothing, in
// whatever shape the module's contracts already use, and Erase removes what the
// rows *say* while keeping the rows the audit trail must keep (ADR 0020), inside
// the caller's transaction — a half-erased person is not an erased person.
package personaldata

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
)

// Subject is the person a request is about, and the strings that must stop naming
// them anywhere in the application.
//
// The strings are here beside the id rather than recovered by each contributor,
// because the two halves of the question have different owners: only the orchestrator
// can read the name and address the person used (they live in modules/user, which no
// other module may read), and only each owner knows which of its own columns may have
// copied them. A contract of Erase(ctx, tx, uuid.UUID) would leave every module to
// guess what "erased" means for a tasks.description, and a guess in a contract is a
// rule nobody can change.
type Subject struct {
	ID uuid.UUID

	// Email, DisplayName and Handle are the person's identifiers as they were
	// written before the erasure. An empty one is absent and not a match term:
	// replacing "" would rewrite every column it is applied to.
	Email       string
	DisplayName string
	Handle      string
}

// Token is what a scrub leaves in free text where an identifier was. It is derived
// from the id the audit rule requires to survive, so two mentions of one person still
// read as two mentions of the same person, and neither of them reads as a person.
func (s Subject) Token() string {
	return "<erased " + shortID(s.ID) + ">"
}

// shortID is the fragment of a person's id a token carries: the first eight hex digits
// of the uuid, enough to tell two people's placeholders apart and not enough to be an
// id anybody can act on.
func shortID(id uuid.UUID) string { return id.String()[:8] }

// shortToken is Token with the word taken out, for a row whose column has no room for
// the whole placeholder: see widths in scrub.go, which is why a replacement has to have
// a fallback at all. It keeps the same fragment of the id, so two mentions of one
// person still read as two mentions of the same person; it only says less.
func (s Subject) shortToken() string { return "<" + shortID(s.ID) + ">" }

// ErasedEmail is the value users.email and any other address column takes. It is
// unique per tenant, so the partial unique index on lower(email) still holds after the
// replacement and the real address is released for reuse, and its domain is reserved
// by RFC 2606: it can never become somebody's mailbox.
func (s Subject) ErasedEmail() string {
	return "erased-" + s.ID.String() + "@erased.invalid"
}

// Identifiers are the strings an erase or a scrub must find nothing of, longest
// first: a name that is a substring of the address is removed with the address and
// not after it, or the tail of the address would survive its own replacement. It is
// the strings alone; Known is the strings with the rule each one is found by.
func (s Subject) Identifiers() []string {
	known := s.Known()
	out := make([]string, 0, len(known))
	for _, k := range known {
		out = append(out, k.Value)
	}
	return out
}

// Identifier is one string a person is known by, and where a scrub may take it for
// them.
type Identifier struct {
	// Value is the string as the person's own row carried it before the erasure.
	Value string

	// Login says Value is a handle rather than a name or an address: a string picked
	// out of a namespace of short lower-case words, every one of which is also
	// somebody else's ordinary word.
	Login bool
}

// Known are the strings an erasure must stop finding, longest first, each with the
// rule that finds it.
//
// The rule splits by where the string came from rather than by what it looks like,
// because the three are not three spellings of one thing. An address and a name are
// what a person is called, so a whole-word match over the whole tenant finds them
// wherever somebody wrote about them — which is the case the scrub runs over the
// tenant and not over the person's own rows for. A handle is drawn from the same
// characters as the language: the schema accepts "will" as a handle, and "the deploy
// will run on Monday" is a colleague's sentence about a deployment. So a login is
// taken for the person where it is written as one — an @-mention, the one form that
// cannot be prose — and as a bare word only in a row that carries the person's own id.
// scrub.go owns the matching; the split lives here because only the Subject knows
// which field a string came out of.
func (s Subject) Known() []Identifier {
	candidates := []Identifier{
		{Value: s.Email},
		{Value: s.DisplayName},
		{Value: s.Handle, Login: true},
	}
	out := make([]Identifier, 0, len(candidates))
	for _, c := range candidates {
		if c.Value = strings.TrimSpace(c.Value); c.Value == "" {
			continue
		}
		if !holds(out, c.Value) {
			out = append(out, c)
		}
	}
	longestFirst(out)
	return out
}

// holds reports whether any identifier already carries this exact string. A person whose
// display name is spelled exactly like their handle is named by the wider rule, not the
// narrower one: the two strings are one string, and leaving a name in somebody's notes is
// the failure this list exists to prevent.
func holds(ids []Identifier, value string) bool {
	for _, id := range ids {
		if id.Value == value {
			return true
		}
	}
	return false
}

// Data is one module's answer for its own rows. A module contributes it as a Part and
// never calls it itself.
type Data interface {
	// Export answers for this module's rows about one person of one tenant, as the
	// JSON a part of that person's bundle. The shape is the module's: a contract
	// that dictated field names would be one more place the kernel knows what a
	// person's email field is called.
	Export(ctx context.Context, tx db.Tx[db.Tenant], subject Subject) (json.RawMessage, error)

	// Erase removes what this module's rows say about the person, inside the
	// caller's transaction, and returns no receipt: the orchestrator verifies the
	// result against the module's own Declaration before it commits.
	Erase(ctx context.Context, tx db.Tx[db.Tenant], subject Subject) error
}

// Part is one module's Data, named by that module's own manifest string so the bundle
// can say which module wrote which part. A contributor that names a module other than
// its own is a part nobody can attribute, so kit/module refuses it.
type Part struct {
	Module string
	Data   Data
}

// Table is a module's claim about one table in the SQL it owns: which column holds a
// person's own id, which other columns name a person, which text columns an identifier
// may have been copied into, and which uuid columns name a row and not a person.
//
// The last of the four is what makes the claim complete. A rule that looked for
// columns named user_id or actor would be over-broad — it names columns the kernel has
// no business knowing — and under-broad — it goes stale the day a module names one
// sent_to_id. Listing the uuid columns that name a row instead means the module states
// which of its own columns are people and which are not, and Check refuses one it
// cannot find a decision for.
type Table struct {
	Name string

	// Person is the column holding this person's own row id, and it is empty only
	// for a table that references rows by uuid and names no person at all.
	Person string

	// About names the other columns that point at a natural person: an author, an
	// actor, the person who minted a token.
	About []string

	// Text names the text and jsonb columns an identifier may have been copied
	// into, and is therefore the list the erasure re-reads to prove itself.
	Text []string

	// NotPerson names the uuid columns that name a row of some other entity: a
	// file, an event, a plan.
	NotPerson []string
}

// Declaration is a module's PersonalData. Exactly one of its two halves is set.
type Declaration struct {
	// Tables are the tables this module owns that carry a uuid a reader could
	// resolve to a person — including the ones that resolve to a row.
	Tables []Table

	// None is why no table this module creates carries such a column. It is a
	// sentence, because a bare "none" is a checkbox and a sentence is a claim
	// somebody can read and disagree with.
	None string
}

// None is the declaration of a module that holds no person column at all, with the
// reason it holds none. Check refuses it if the module's SQL says otherwise.
func None(reason string) Declaration {
	return Declaration{None: reason}
}

// Implements is the declaration of a module whose tables carry person columns.
func Implements(tables ...Table) Declaration {
	return Declaration{Tables: tables}
}

// exemptColumns are the two uuid columns every table in this repository has and
// neither of which is a fact about a person: the row's own id, and the tenant
// db.Tenant already bounds it with. Nothing else is exempt.
var exemptColumns = []string{"id", "tenant_id"}

// textTypes are the column types an identifier can be copied into. A Text claim on a
// uuid column is the claim misfiled, and Check refuses it rather than scrubbing
// nothing and reporting success.
var textTypes = []string{"text", "varchar", "character", "bpchar", "char", "json", "jsonb", "citext"}

// Declares reports whether this table claims one column of the table.
func (t Table) Declares(column string) bool {
	return column == t.Person || contains(t.About, column) ||
		contains(t.Text, column) || contains(t.NotPerson, column)
}

// Check returns every way this declaration fails against the SQL it claims to
// describe, and it reads the module's own migration bytes rather than a live schema:
// a deployment that never boots one still gets the refusal, and a claim that has
// drifted from the file that earned it is the failure the erasure below cannot
// survive. An empty declaration — no Tables, no reason — is itself the first refusal.
//
// A nil files is checked as a module with no SQL of its own, which is the state
// modules/admin and modules/web are in.
func (d Declaration) Check(module string, files fs.FS) []string {
	var bad []string
	add := func(format string, args ...any) {
		bad = append(bad, fmt.Sprintf("module %q: %s", module, fmt.Sprintf(format, args...)))
	}
	schema, err := ReadMigrations(files)
	if err != nil {
		return []string{fmt.Sprintf("module %q: its migrations cannot be read: %s", module, err)}
	}

	if d.None != "" && len(d.Tables) > 0 {
		add("declares both a reason for holding no person column and %d tables that hold one", len(d.Tables))
		return bad
	}
	if d.None != "" && strings.TrimSpace(d.None) == "" {
		add("declares none with no reason; \"none\" is a claim somebody has to be able to read")
	}
	if d.None != "" {
		for _, held := range schema.PersonColumns() {
			add("declares none, but %s gives %s.%s a uuid, which its reason (\"%s\") does not account for",
				held.File, held.Table, held.Column, d.None)
		}
		return bad
	}
	if len(d.Tables) == 0 {
		add("declares no personal-data table and no reason for holding none; a module that holds no " +
			"person column says why, and one that holds them says which")
	}

	seen := map[string]Table{}
	for _, t := range d.Tables {
		if t.Name == "" {
			add("declares a personal-data table with no name")
			continue
		}
		if _, twice := seen[t.Name]; twice {
			add("declares table %q twice", t.Name)
			continue
		}
		seen[t.Name] = t
		sql, created := schema[t.Name]
		if !created {
			add("declares table %q, which none of its own migrations creates; a module names a table "+
				"here only when its own SQL owns it", t.Name)
			continue
		}
		for _, claim := range claims(t) {
			if claim.duplicate {
				add("files %s.%s as both %s and %s; a column nobody can file is a column nobody can write an erasure for",
					t.Name, claim.column, claim.kind, claim.also)
				continue
			}
			column, carried := sql[claim.column]
			if !carried {
				add("declares %s.%s as %s, which its migrations do not carry", t.Name, claim.column, claim.kind)
				continue
			}
			switch claim.kind {
			case kindPerson, kindAbout, kindNotPerson:
				if !strings.HasPrefix(column.Type, "uuid") {
					add("declares %s.%s as %s, which its migrations give the type %q",
						t.Name, claim.column, claim.kind, column.Type)
				}
			case kindText:
				if !isTextType(column.Type) {
					add("declares %s.%s as %s, which its migrations give the type %q",
						t.Name, claim.column, claim.kind, column.Type)
				}
			}
		}
	}

	// The completeness rule: the SQL owns the question of which columns carry a
	// uuid, and every one of them has to appear in the declaration in one of the
	// four roles. This is what refuses a module that added a person column and
	// thought about something else.
	for _, held := range schema.PersonColumns() {
		t, declared := seen[held.Table]
		if !declared {
			add("%s gives %s.%s a uuid that no personal-data table of this module names",
				held.File, held.Table, held.Column)
			continue
		}
		if !t.Declares(held.Column) {
			add("%s gives %s.%s a uuid, which its declaration files as none of person, about, text or not-person",
				held.File, held.Table, held.Column)
		}
	}
	return bad
}

type claimKind string

const (
	kindPerson    claimKind = "the person"
	kindAbout     claimKind = "a person it is about"
	kindText      claimKind = "text an identifier may sit in"
	kindNotPerson claimKind = "a uuid naming a row and not a person"
)

type claim struct {
	column string
	kind   claimKind

	// duplicate marks a column the Table files twice, in two roles at once, and
	// also names the second role so the refusal can say both.
	duplicate bool
	also      claimKind
}

// claims flattens one Table into the claims it makes, marking a column claimed in two
// roles at once: a column that is both a person and a row reference is a column
// nobody can write an erasure for.
func claims(t Table) []claim {
	var out []claim
	seen := map[string]claimKind{}
	add := func(column string, kind claimKind) {
		if column = strings.TrimSpace(column); column == "" {
			return
		}
		if first, twice := seen[column]; twice {
			out = append(out, claim{column: column, kind: first, duplicate: true, also: kind})
			return
		}
		seen[column] = kind
		out = append(out, claim{column: column, kind: kind})
	}
	add(t.Person, kindPerson)
	for _, c := range t.About {
		add(c, kindAbout)
	}
	for _, c := range t.Text {
		add(c, kindText)
	}
	for _, c := range t.NotPerson {
		add(c, kindNotPerson)
	}
	return out
}

func isTextType(columnType string) bool {
	end := 0
	for end < len(columnType) && columnType[end] >= 'a' && columnType[end] <= 'z' {
		end++
	}
	return contains(textTypes, columnType[:end])
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func longestFirst(ids []Identifier) {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && len(ids[j].Value) > len(ids[j-1].Value); j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
}
