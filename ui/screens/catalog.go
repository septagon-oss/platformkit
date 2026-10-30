package screens

import (
	"context"
	"maps"
	"slices"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// Text is the seam a declared reading string is resolved through — one resource's
// words in one request's language. It is ui/resource's own seam, named here so a
// caller of the document and the renderer of the screens hand over the same thing:
// a page and the JSON a native shell reads must not answer one person's request in
// two languages. See ui/resource/hints.go for the key grammar it is asked with.
type Text = resource.Text

// CatalogVersion identifies the portable catalogue contract. Incompatible
// meaning or required-field changes require a version change; new optional
// fields preserve existing readers. Listed resources losing verbs are incompatible.
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

// Pictures describes the installation's authorized delivery templates. Source
// substitutes {id}; Widths lists ascending supported variants, with the stored
// frame used when no width is selected. An absent block offers no picture door.
type Pictures struct {
	Source string `json:"source"`
	// ComposedSource is optional and substitutes one canonical {reference} token.
	ComposedSource string `json:"composedSource,omitempty"`
	Widths         []int  `json:"widths,omitempty"`
}

// Entry is one resource as a shell sees it. There is no Readable field because
// an unreadable resource is not in the document — save a CommandsOnly one whose
// command the caller holds, listed with empty operations: what a caller may not
// look at, they are not offered. `writable` is the caller's guard narrowed by
// this resource's operation set: an entry that names no write and still says
// `writable` promises a door no route answers.
type Entry struct {
	// ReadContract identifies the nested session values instead of scalar fields.
	ReadContract *struct {
		Kind    string `json:"kind"`
		Version int    `json:"version"`
	} `json:"readContract,omitempty"`
	Input *InputEntry `json:"input,omitempty"`
	entity.Schema
	// Presentation carries optional declared hints beside Resolved for older readers.
	// Absent hints retain their documented defaults; it grants no capabilities.
	Presentation *EntryPresentation `json:"presentation,omitempty"`
	// Resolved is the presentation policy decided once on the server — titles,
	// column order, labels, tones, sort queries, command copy — for the request's
	// language, so no shell derives it again. It follows Presentation, which it is
	// computed from, and is absent only from a document an older server wrote; a
	// shell that finds a policyVersion it knows uses it whole, and one that does
	// not keeps reading the entry the way it always did.
	Resolved *present.Resolved `json:"resolved,omitempty"`
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
	// WritePath names a separate authorized write address. Empty means Path;
	// resources whose writes are withheld publish no alternative address.
	WritePath string `json:"write_path,omitempty"`
	// Commands are the doors this caller may open beyond the five: the
	// lifecycle routes the resource carries. A command the caller may not call
	// is absent for the same reason an unreadable resource is.
	Commands []Command `json:"commands,omitempty"`
	// Operations names available CRUD routes in their stable order. Nil means
	// all five and is omitted; an explicit empty set is printed for CommandsOnly.
	Operations []string `json:"operations,omitzero"`
	// Singleton says a tenant has one of these, at Path itself: a screen for
	// it is the record and its form, and never a list with a New button on it.
	Singleton bool `json:"singleton,omitempty"`
}

// Command describes an authorized lifecycle route. Path overrides the usual
// resource/item/verb address when the route lives elsewhere; an absent Path
// preserves that convention for existing readers.
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
	Fields      []entity.Field    `json:"fields,omitempty"`
	// Presentation carries optional declared hints beside Resolved for older readers.
	// Absent hints retain their documented defaults; it grants no capabilities.
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

// Describe returns the resources and commands authorized for this caller,
// using their declared language. No request authority means no visible entries.
func Describe(ctx context.Context, resources []httpx.Resource) Catalog {
	return DescribeLanguage(ctx, resources, nil)
}

// DescribeLanguage returns the authorized catalogue localized by text.
// Machine names, values, paths and predicates are unchanged; language identifies
// the resulting policy vocabulary.
func DescribeLanguage(ctx context.Context, resources []httpx.Resource, text Text) Catalog {
	return DescribeIn(ctx, resources, text, "")
}

// DescribeIn is DescribeLanguage that also says which language `text` speaks, so
// every entry's `resolved.language` names the tag the strings were resolved for.
// An empty tag is the declaration as written, printed as English.
func DescribeIn(ctx context.Context, resources []httpx.Resource, text Text, language string) Catalog {
	return DescribeInstalled(ctx, resources, text, language, Installed{})
}

// Installed names the optional capabilities composed by the application.
type Installed struct {
	Saving bool
}

// DescribeInstalled resolves policy against the capabilities this application wired.
func DescribeInstalled(ctx context.Context, resources []httpx.Resource, text Text, language string, installed Installed) Catalog {
	out := Catalog{Version: CatalogVersion, Resources: []Entry{}}
	for _, r := range resources {
		// r is this loop's copy, so narrowing its commands to the ones this
		// caller may call leaves Describe1 the pure function it is.
		r.Commands = r.CommandsFor(ctx)
		if r.CommandsOnly {
			if len(r.Commands) > 0 {
				out.Resources = append(out.Resources, commandsOnly(Localise(r, text), text, language))
			}
			continue
		}
		if !r.Readable(ctx) {
			continue
		}
		r = Localise(r, text)
		out.Resources = append(out.Resources, describeEntryInstalled(r, r.Writable(ctx), text, language, installed))
	}
	return out
}

// commandsOnly is the entry of a CommandsOnly resource for a caller who holds
// one of its commands — a key retiring itself reads nothing about keys. It
// names no screen and offers no operation, so a shell draws the commands and no
// list or form; the caller is told the resource exists because a door on it is
// theirs.
func commandsOnly(r httpx.Resource, text Text, language string) Entry {
	e := describeEntryInstalled(r, false, text, language, Installed{})
	e.Screen = ""
	return e
}

// Localise returns a detached copy with declared words passed through text.
// Machine values remain unchanged; nil text preserves the declarations.
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
	return DescribeEntry(r, writable, nil, "")
}

// DescribeEntry is Describe1 for one request: the resource is already localised
// (see Localise), and `text` and `language` reach the words the policy adds and
// name the tag printed as `resolved.language`. A nil text and an empty language
// are Describe1.
func DescribeEntry(r httpx.Resource, writable bool, text Text, language string) Entry {
	return describeEntryInstalled(r, writable, text, language, Installed{})
}

// DescribeEntryInstalled resolves one entry against the optional capabilities composed by the application.
func DescribeEntryInstalled(r httpx.Resource, writable bool, text Text, language string, installed Installed) Entry {
	return describeEntryInstalled(r, writable, text, language, installed)
}

func describeEntryInstalled(r httpx.Resource, writable bool, text Text, language string, installed Installed) Entry {
	command := slices.ContainsFunc(r.Commands, func(c httpx.Command) bool {
		return c.Run != nil && c.Auth.Declared()
	})
	writable = writable && (command || writeAvailable(r, httpx.CRUDCreate) ||
		writeAvailable(r, httpx.CRUDUpdate) || writeAvailable(r, httpx.CRUDDelete))
	e := Entry{Schema: r.Schema, Presentation: deviations(r.Present, r.Commands), Screen: r.Screen,
		Immutable: r.Immutable, Writable: writable, Singleton: r.Singleton,
		Operations: r.OperationWords()}
	if writable {
		e.WritePath = r.WritePath
	}
	for _, c := range r.Commands {
		e.Commands = append(e.Commands, Command{
			Bindings: maps.Clone(c.Bindings), Undo: authorizedUndo(r.Commands, c.Undo), HasResult: c.HasResult, Output: c.Output,
			Verb: c.Verb, Summary: c.Summary, Description: c.Description,
			Collection: c.Collection, Path: derived(r, c), Fields: c.Fields,
			Presentation: commandDeviations(c.Present),
		})
	}
	if r.InputRun != nil {
		e.Input = &InputEntry{Fields: r.Schema.Fields, Output: r.InputOutput, OutputList: r.InputList, Path: r.Schema.Path}
	}
	if r.Guided != nil {
		e.ReadContract = &struct {
			Kind    string `json:"kind"`
			Version int    `json:"version"`
		}{Kind: "guided-session", Version: 1}
	}
	e.Resolved = resolve(r, text, language, installed.Saving)
	return e
}

// resolve is the entry's presentation policy for the commands this caller may call.
func resolve(r httpx.Resource, text Text, language string, saving bool) *present.Resolved {
	commands := make([]present.CommandInput, 0, len(r.Commands))
	for _, c := range r.Commands {
		commands = append(commands, present.CommandInput{Verb: c.Verb, Summary: c.Summary, Undo: c.Undo, HasResult: c.HasResult, Hints: c.Present, Fields: c.Fields, Bindings: c.Bindings, Collection: c.Collection, Repeated: r.History != nil && r.History.RestoreVerb == c.Verb})
	}
	resolved := present.Resolve(present.Input{
		SessionCommands: r.GuidedCommands, SessionWords: sessionWords,
		Schema: r.Schema, Hints: r.Present, Immutable: r.Immutable, Singleton: r.Singleton,
		CommandsOnly: r.CommandsOnly, InputOnly: r.InputRun != nil, Operations: r.OperationWords(), Commands: commands,
		Language: language, Text: resource.PolicyText(resource.Text(text)), SavingAvailable: saving,
	})
	return &resolved
}

var sessionWords = map[string]string{
	"screens.session.step": "Step {current} of {total}", "screens.session.set": "Set {current} of {total}",
	"screens.session.completed_step": "Step {current} of {total}, completed", "screens.session.start_timer": "Start “{name}” {duration}",
	"screens.session.running_timer": "{name}: {duration} remaining", "screens.session.timer_ended": "{name} timer ended",
	"screens.session.timers": "Timers", "screens.session.cancel_timer": "Cancel {name} timer", "screens.session.restart_timer": "Start {name} again",
	"screens.session.done_set": "Done set", "screens.session.next_set": "Next set", "screens.session.next_step": "Next step",
	"screens.session.target_repetitions": "Repetitions", "screens.session.target_duration": "Duration", "screens.session.previous_step": "Previous step",
	"screens.session.rest": "Rest", "screens.session.rest_ended": "Rest ended", "screens.session.rest_added": "Added 15 seconds",
	"screens.session.up_next": "Up next", "screens.session.add_15": "+15 s", "screens.session.skip": "Skip rest",
	"screens.session.continue": "Continue", "screens.session.finish": "Finish session", "screens.session.finish_partial": "Finish with {completed} of {total} sets",
	"screens.session.completed": "Completed {completed} of {total} sets", "screens.session.end": "End of session",
	"screens.session.nothing_recorded": "Nothing recorded", "screens.session.navigation_only": "Next does not record progress.",
	"screens.session.completion_unavailable": "Set completion is unavailable.", "screens.session.finish_unavailable": "Finish is unavailable.",
	"screens.session.reload": "Reload", "screens.session.no_script": "Live countdown and sound need JavaScript.",
	"screens.session.storage_unavailable": "Timers will reset if you reload.", "screens.session.wake_unavailable": "Your screen may sleep.",
	"screens.session.enable_sound": "Enable sound", "screens.session.exit": "Exit session",
	"screens.session.keyboard": "Arrow keys change steps. Space starts a timer or skips rest.",
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
