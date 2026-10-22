package screens

import (
	"context"
	"maps"
	"net/http"
	"slices"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// Text is the seam a declared reading string is resolved through — one resource's
// words in one request's language. It is ui/resource's own seam, named here so a
// caller of the document and the renderer of the screens hand over the same thing:
// a page and the JSON a native shell reads must not answer one person's request in
// two languages. See ui/resource/hints.go for the key grammar it is asked with.
type Text = resource.Text

// CatalogVersion is the shape of the document at /api/v1/app/resources — the
// contract a shipped native shell renders from — and it is the one number in
// this repository that cannot be revised by the person who changes it.
//
// It goes up when an existing key changes meaning, when a key becomes required,
// and whenever a shell that does not know the change could still parse the
// document and draw the wrong screen. It does not go up for an added optional
// key: an older shell ignores what it has not read, which is the only reason any
// of this can move forward without a flag day.
//
// What the number cannot do is protect the build already installed: the shell in
// somebody's pocket parses what it was written to parse. It protects it by what it
// refuses to do to it — additive, optional, never a change of meaning — and moves
// for a listed resource losing a verb, whose button that build drew and whose
// address now answers nothing. A verb hidden by a new entry strands nobody.
const CatalogVersion = 3

// Catalog is the machine-readable form of what a shell shows: every resource
// the caller may read, its schema, and whether the caller may write it. A shell
// that is not a browser — the native one — generates its screens from this the
// way a browser shell generates them from httpx.Resources, by the same rules.
type Catalog struct {
	// Version is CatalogVersion at the moment the document was written.
	Version   int       `json:"catalogVersion"`
	Resources []Entry   `json:"resources"`
	Pictures  *Pictures `json:"pictures,omitempty"`
}

// Pictures is this installation's door to the bytes of a stored picture, said
// once for the whole document rather than once per field that holds an id.
//
// Source is a template and not a base path: the literal `{id}` is replaced with
// the field's own value — the lower-case hyphenated UUID text — and nothing else
// in it is substituted. It is published for the same reason `screen`,
// `write_path` and a command's `path` are: a shell that builds an address out of
// another component's rules is guessing, and this repository has already paid for
// one such guess once.
//
// Widths is the set of resized variants this door serves, ascending; a request
// for a width outside it is refused by the door and names the set back. Absent
// Widths, or a width the picture was never planned at, the answer is the stored
// frame at its own size. A shell that fetches no variant fetches Source.
//
// An absent `pictures` block means this installation composed no module that
// serves a record's pictures. Every picture field then draws the still state its
// own renderer chooses — which is an answer, not a mistake, and the reason a
// resource with a picture column can mount in an installation that has no file
// module at all. A key is on the wire because somebody declared it, so the
// composition writes this and a resource-bearing document never invents it.
type Pictures struct {
	Source string `json:"source"`
	// ComposedSource is optional and substitutes one canonical {reference} token.
	ComposedSource string `json:"composedSource,omitempty"`
	Widths         []int  `json:"widths,omitempty"`
}

// Entry is one resource as a shell sees it. There is no Readable field because
// an unreadable resource is not in the document — save the one whose command the
// caller holds, listed with empty operations: what a caller may not look at, they
// are not offered. `writable` is the caller's guard narrowed by
// this resource's operation set: an entry that names no write and still says
// `writable` promises a door no route answers.
type Entry struct {
	entity.Schema
	// Name is the resource's own two words — the singular and the plural its author
	// declared — carried so a shell that is not a browser can label a shelf of rows
	// in the reader's language instead of spelling the entity's code name. Printed
	// only when both words are declared, so an entry a shell read before this key
	// existed is read exactly as it was.
	Name *entity.Name `json:"name,omitempty"`
	// Presentation is this resource as its author said it reads — the words, the
	// icon, the group, which field names a row and which block each field belongs
	// to. It is declared here, immediately after the embedded schema and before
	// Screen, because encoding/json hoists an embedded struct's keys at the
	// embed's position and writes fields in declaration order: this is where the
	// entry's `presentation` block comes from, which is also why it goes last on
	// Command below.
	//
	// An entry nobody hinted has no `presentation` key at all, so every shell
	// already installed reads what it read before this field existed and
	// CatalogVersion stays where it is.
	Presentation *EntryPresentation `json:"presentation,omitempty"`
	// Screen is the workspace address of the generated screen — /app/task/tasks
	// — stated because the kernel composed it and a shell cannot derive it any
	// more. It used to derive the screen's path from the API's, by cutting
	// "/api/v1" off one and pasting in where it believed the shell to be; the
	// moment a resource's reads and writes stood on different surfaces, that
	// derivation was a guess. It is still optional, so a build already installed
	// keeps working from the address it can derive.
	Screen    string   `json:"screen,omitempty"`
	Immutable []string `json:"immutable,omitempty"`
	Writable  bool     `json:"writable"`
	// WritePath is the address the writes of this resource are answered at, for
	// the one resource whose writes do not answer at its own Path: a price list is
	// read by the tenant that pays for it on the workspace surface and written by
	// the installation on the control plane. It is printed on the same rule as a
	// command's Path — only when the derivation is no longer true, so an entry a
	// shell could always read the same way still reads that way — and only for a
	// caller who may write, because this document is what this caller may do, and
	// an address they cannot reach is neither true nor theirs to be told.
	WritePath string `json:"write_path,omitempty"`
	// Commands are the doors this caller may open beyond the five: the
	// lifecycle routes the resource carries. A command the caller may not call
	// is absent for the same reason an unreadable resource is.
	Commands []Command `json:"commands,omitempty"`
	// Operations names the routes this resource offers, in list/read/create/
	// update/delete order, printed by the same rule as write_path and a command's
	// path: only when a shell could no longer derive the answer. An entry that
	// offers all five prints nothing, exactly as it did before this key existed.
	//
	// A shell that has not read this key reads an entry that hides a verb as if it
	// did not — which is why CatalogVersion moves when a resource the document had
	// already listed hides one, and why apps/platformkit's ratchet case refuses to
	// let that move be a memory. An absent key means all five, so it is the shell's
	// next build that has to require it, reading this field.
	//
	// An empty list, printed as [], is a resource this caller may reach only through
	// one of its commands — a CommandsOnly registration, or a readable one whose rows
	// this caller is refused: nothing to read or write, only those commands to call
	// (see DescribeLanguage). omitzero
	// keeps nil — all five — absent, so no entry printed before reads otherwise.
	Operations []string `json:"operations,omitzero"`
	// Singleton says a tenant has one of these, at Path itself: a screen for
	// it is the record and its form, and never a list with a New button on it.
	Singleton bool `json:"singleton,omitempty"`
}

// Command is one lifecycle route as a shell sees it. It carries no permission
// for the same reason Entry carries no readable flag: the document is what this
// caller may do, not a description of the API's guards.
//
// Path is carried only when the derivation is no longer true. A command the
// installation owns lives on the control plane, whose address does not start
// from the resource's own, so POST {Path}/{id}/{verb} would send a caller to an
// address nothing answers at. Absent means what it always meant: derive it that
// way, which is what every command that shares its resource's surface is still
// reachable by — so a document written before this key existed is read the same
// way it always was.
type Command struct {
	Bindings    map[string]string `json:"bindings,omitempty"`
	Undo        string            `json:"undo,omitempty"`
	HasResult   bool              `json:"hasResult,omitzero"`
	Output      []entity.Field    `json:"output,omitempty"`
	Verb        string            `json:"verb"`
	Summary     string            `json:"summary,omitempty"`
	Description string            `json:"description,omitempty"`
	Collection  bool              `json:"collection,omitempty"`
	Path        string            `json:"path,omitempty"`
	// Method is the verb the Path above answers, printed on the same rule as
	// Path itself: absent means POST, which is what every command kit/rest
	// mounts answers with and what a build that predates this key already sends.
	// A command mounted with another verb — a replace at its resource's own
	// address — says which, because a shell that posted to it would meet a 405
	// at an address this document just promised it.
	Method string `json:"method,omitempty"`
	// Self says the command writes the caller's own row, so a device that has the
	// person's token and nothing else knows it may offer it: the same fact
	// kit/httpx declares on its own command, carried here unchanged.
	Self   bool           `json:"self,omitempty"`
	Fields []entity.Field `json:"fields,omitempty"`
	// Presentation is this command as its author described it: the word on the
	// button, whether it is the action to offer, what a person reads before it
	// runs. It sits last so the six keys a shell has always read keep their
	// positions, and it is absent for every command nobody described.
	Presentation *CommandPresentation `json:"presentation,omitempty"`
}

func authorizedUndo(commands []httpx.Command, verb string) string {
	for _, command := range commands {
		if command.Verb == verb && !command.Present.System {
			return verb
		}
	}
	return ""
}

// Describe is the catalog for this caller in the language its author wrote it
// in: the readable resources, in the order given, each saying whether this
// caller may write it. The two questions are the ones the closures on the
// resource ask — the same Authorizer, the same operator rule — so the document
// cannot promise a screen the API would refuse.
//
// It is the exported entry point the signature has always carried, and it is
// `DescribeLanguage` with no language: an installation that composes no copy
// catalogue, and a caller that has no request to negotiate one from, are served
// the declaration as written. A route that answers a person's request asks
// `DescribeLanguage` — the same document, read in the caller's language.
func Describe(ctx context.Context, resources []httpx.Resource) Catalog {
	return DescribeLanguage(ctx, resources, nil)
}

// DescribeLanguage is the same document read in one language: every *word* a
// person reads — an entry's singular, a field's label, a command's
// confirmation — is resolved through `text`, with the declared literal as the
// fallback, which is what decision 0085's catalogue contract means by "resolved
// server-side for the request's language". The vocabulary names (an icon, a
// tone, a visibility, a field name) are not words and are never resolved: see
// ui/resource/hints.go. A nil `text` is Describe.
//
// Each resource is copied before it is translated: the schema and the hints ride
// on the registration, which every request reads, so writing a translated word
// into them would let one request's Accept-Language decide the next one's screen.
func DescribeLanguage(ctx context.Context, resources []httpx.Resource, text Text) Catalog {
	out := Catalog{Version: CatalogVersion, Resources: []Entry{}}
	for _, r := range resources {
		// r is this loop's copy, so narrowing its commands to the ones this
		// caller may call leaves Describe1 the pure function it is.
		r.Commands = r.CommandsFor(ctx)
		if r.CommandsOnly {
			if len(r.Commands) > 0 {
				// A key retiring itself reads nothing about keys: the resource mounts
				// no read route at all, so the entry is the command alone, for the one
				// caller that command admits.
				out.Resources = append(out.Resources, commandsNotRows(Localise(r, text)))
			}
			continue
		}
		// A caller who may use one of this resource's commands is told the resource
		// exists even where its rows stay unreadable: CommandsFor asks each command's
		// guard, not the resource's, and an account command (rest.CommandOptions.Self)
		// is the case where that answer is yes while the rows are none of the caller's
		// business. What such a caller is given is the name and those commands, and
		// nothing else — see commandsNotRows.
		if !r.Readable(ctx) {
			if len(r.Commands) == 0 {
				continue
			}
			// The row schema names fields of rows this caller may not look at, and the
			// screen is the list of them: both go before the resource is translated.
			r.Schema.Fields, r.Immutable = nil, nil
			out.Resources = append(out.Resources, commandsNotRows(Localise(r, text)))
			continue
		}
		r = Localise(r, text)
		out.Resources = append(out.Resources, Describe1(r, r.Writable(ctx)))
	}
	return out
}

// commandsNotRows is the entry a caller is given for a resource they may reach only
// through one of its commands, which is the case two different ways: a CommandsOnly
// resource mounts no read route at all, and a readable resource can be readable to
// everybody but this caller. Either way the entry names no screen, describes no row
// and offers no operation, so a shell draws the commands and no list or form.
//
// The operation list prints empty rather than absent for the one reason that
// matters to a build in a pocket: an absent list means all five, so leaving it out
// of an entry the document carries for a command's sake would hand the shell five
// doors to draw on the very routes that answer this caller nothing. Writable
// follows the same rule — with no create, update or delete on offer, no guard on a
// write route makes a write possible here.
func commandsNotRows(r httpx.Resource) Entry {
	e := Describe1(r, false)
	e.Screen, e.Operations = "", []string{}
	return e
}

// Localise is a resource with every declared word read through the seam, and the
// same resource untouched when there is no seam to read it through. Describe
// applies it; a caller that has a resource and no request — a golden file, a
// shell rendering one entry — asks for it directly.
//
// Everything it writes is a copy. A resource is a registration, shared by every
// request the process serves, and a translated word written into it would be one
// request's Accept-Language deciding the next one's screen.
func Localise(r httpx.Resource, text Text) httpx.Resource {
	if text == nil {
		return r
	}
	words := resource.WordsFor(r.Schema, text)
	r.Present = words.Entry(r.Present)
	r.Schema.Fields = words.Fields(r.Schema.Fields)
	commands := slices.Clone(r.Commands)
	for i, c := range commands {
		commands[i].Present = words.Command(c.Verb, c.Present)
		commands[i].Fields = words.CommandFields(c.Verb, c.Fields)
	}
	r.Commands = commands
	return r
}

// Describe1 is one entry, from a resource and the answer to "may this caller
// write it". It is the pure half of Describe, and what the golden test builds
// from without an authorizer.
//
// The flag is the caller's guard narrowed by the operation set: the guard says who
// is asking, the set says whether anything here answers a write at all, and
// `writable` is what this caller may *do*. See Entry.
func Describe1(r httpx.Resource, writable bool) Entry {
	command := slices.ContainsFunc(r.Commands, func(c httpx.Command) bool {
		return c.Run != nil && c.Auth.Declared()
	})
	writable = writable && (command || writeAvailable(r, httpx.CRUDCreate) ||
		writeAvailable(r, httpx.CRUDUpdate) || writeAvailable(r, httpx.CRUDDelete))
	e := Entry{Schema: r.Schema, Presentation: deviations(r.Present, r.Commands), Screen: r.Screen,
		Immutable: r.Immutable, Writable: writable, Singleton: r.Singleton,
		Operations: r.OperationWords()}
	// Both words or neither: a shelf labelled with a singular at three rows is the
	// wrong word, and no word is better than a wrong one — see the field's rule.
	if r.Name.One != "" && r.Name.Other != "" {
		name := r.Name
		e.Name = &name
	}
	if writable {
		e.WritePath = r.WritePath
	}
	for _, c := range r.Commands {
		e.Commands = append(e.Commands, Command{
			Bindings: maps.Clone(c.Bindings), Undo: authorizedUndo(r.Commands, c.Undo), HasResult: c.HasResult, Output: c.Output,
			Verb: c.Verb, Summary: c.Summary, Description: c.Description,
			Collection: c.Collection, Path: derived(r, c), Method: verb(c), Fields: c.Fields,
			Self:         c.Self,
			Presentation: commandDeviations(c.Present),
		})
	}
	return e
}

// verb is the command's method as the document states it: nothing for the one
// every command already answers with, and the verb itself for the rare one that
// does not — the same economy as derived below, so an entry a shell could read
// before this key existed is byte-for-byte what it was.
func verb(c httpx.Command) string {
	if c.Method == "" || c.Method == http.MethodPost {
		return ""
	}
	return c.Method
}

// derived is the command's address when a shell could not work it out itself,
// and empty when it could. The empty answer is the whole point: this document is
// a contract a build in somebody's pocket parses, and the entries that still
// follow the rule must read exactly as they did before the rule stopped always
// being true.
func derived(r httpx.Resource, c httpx.Command) string {
	if c.Endpoint == "" || c.Endpoint == r.Schema.Path+itemish(r, c)+"/"+c.Verb {
		return ""
	}
	return c.Endpoint
}

func itemish(r httpx.Resource, c httpx.Command) string {
	if c.Collection || r.Singleton {
		return ""
	}
	return "/{id}"
}
