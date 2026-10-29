package present

import "github.com/septagon-oss/platformkit/kit/entity"

// PolicyVersion is the revision of the resolved object's meaning. It moves when
// an existing key changes meaning, never for an added key; CatalogVersion does
// not move with it.
const PolicyVersion = 1

// Resolved is one entry's presentation policy for one request's language. It
// holds no record values and no viewer clock: those stay rendering.
type Resolved struct {
	// Views holds the visible control decision for each offered screen.
	Views         []ResolvedView `json:"views"`
	PolicyVersion int            `json:"policyVersion"`
	// Language is the tag the strings were resolved for after English fallback.
	Language string           `json:"language"`
	Identity ResolvedIdentity `json:"identity"`
	Titles   ResolvedTitles   `json:"titles"`
	Layout   ResolvedLayout   `json:"layout"`
	// Fields lists every schema field in schema order, hidden ones included.
	Fields   []ResolvedField   `json:"fields"`
	List     ResolvedList      `json:"list"`
	Commands []ResolvedCommand `json:"commands"`
	// Deck is absent unless the entry declares a swipe deck.
	Deck    *ResolvedDeck   `json:"deck,omitempty"`
	Summary ResolvedSummary `json:"summary"`
	// Saving is absent unless this installation composed the saving capability and
	// this entry declared itself savable, so a shell draws a save control only for
	// a present one. `savable` is the key, because what it answers is the
	// reader's question and not the author's.
	Saving *ResolvedSaving `json:"savable,omitempty"`
}

// ResolvedSaving is one entry's saving declaration as a screen reads it: the pair
// that names the copy, the two fields of the record that carry the source's key and
// version, and the four words the control shows, every one already resolved for
// language. It names no address and no principal: where a control posts is the
// composition's route table and who is asking is the request's.
type ResolvedSaving struct {
	Module       string              `json:"module"`
	Kind         string              `json:"kind"`
	KeyField     string              `json:"keyField"`
	VersionField string              `json:"versionField"`
	Words        ResolvedSavingWords `json:"words"`
}

// ResolvedSavingWords are ResolvedSaving's four words with a fallback filled in for
// every word the author left empty. This type is the only place those four
// fallbacks are written, so no shell invents a fifth spelling of its own.
type ResolvedSavingWords struct {
	Save      string `json:"save"`
	Saved     string `json:"saved"`
	Remove    string `json:"remove"`
	ListTitle string `json:"listTitle"`
}

// ResolvedGroup is the navigation slot of an entry; both strings are resolved.
type ResolvedGroup struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// ResolvedIdentity names the resource and where it sits in navigation.
type ResolvedIdentity struct {
	Singular string `json:"singular"`
	Plural   string `json:"plural"`
	// Description and EmptyDescription are "" when nobody wrote them.
	Description      string `json:"description"`
	EmptyDescription string `json:"emptyDescription"`
	// Icon is a name entity.Icons admits, or "".
	Icon string `json:"icon"`
	// Group is absent when the entry names none.
	Group *ResolvedGroup `json:"group,omitempty"`
	// Order is the declared position within the group; 0 is "no opinion".
	Order int `json:"order"`
}

// ResolvedTitles says how a row and a record are named and which fields stand beside
// the name. Candidates are field names tried in order; the first whose value is
// non-blank names the row (see Title).
type ResolvedTitles struct {
	// ListCandidates are drawn from fields the list shows, RecordCandidates from
	// fields the record shows: the two sets differ, so the lists may.
	ListCandidates   []string `json:"listCandidates"`
	RecordCandidates []string `json:"recordCandidates"`
	// EmptyTitle is what names a row when no candidate and no id has a value.
	EmptyTitle string `json:"emptyTitle"`
	// Preview, Status, Cover and Thumbnail are field names, "" when none.
	Preview   string `json:"preview"`
	Status    string `json:"status"`
	Cover     string `json:"cover"`
	Thumbnail string `json:"thumbnail"`
	// Summary is the ordered fields that read beside the title.
	Summary []string `json:"summary"`
}

// ResolvedSection is a declared block of the record with the fields that name it.
type ResolvedSection struct {
	Key    string   `json:"key"`
	Label  string   `json:"label"`
	Fields []string `json:"fields"`
}

// ResolvedLayout is membership of each surface, by field name, in reading order.
type ResolvedLayout struct {
	Forms       *ResolvedForms    `json:"forms,omitempty"`
	Columns     []string          `json:"columns"`
	Overview    []string          `json:"overview"`
	Information []string          `json:"information"`
	Sections    []ResolvedSection `json:"sections"`
	Create      []string          `json:"create"`
	Update      []string          `json:"update"`
}

// ResolvedOption is one value of an enum with its words and tone. Tone is always a name
// entity.Tones admits; "neutral" when the author named none.
type ResolvedOption struct {
	// Picture is absent for an option the author drew with no artwork.
	Picture      *entity.ChoicePicture `json:"picture,omitempty"`
	Group        string                `json:"group"`
	NoPreference bool                  `json:"noPreference"`
	Value        string                `json:"value"`
	Label        string                `json:"label"`
	Tone         string                `json:"tone"`
	// Done marks a value the author declared finished; only a status field has any.
	Done bool `json:"done"`
}

// ResolvedReference is the resource a field's value points at, as `module/entity`.
type ResolvedReference struct {
	Resource string `json:"resource"`
}

// ResolvedMoney says how an int64 minor-unit amount reads.
type ResolvedMoney struct {
	CurrencyField string `json:"currencyField"`
	Scale         int    `json:"scale"`
}

// ResolvedTime is the temporal reading of a field. Kind is "date" (a calendar day in
// the viewer's zone) or "instant" (a moment, shown as UTC unless the client
// holds a zone). Relative asks for a clock-relative phrase; the phrase is
// rendering and is computed with Ago.
type ResolvedTime struct {
	Kind     string `json:"kind"`
	Relative bool   `json:"relative"`
}

// ResolvedReading is how a value of the field is shown. Missing, True and False are the
// words for an absent value and the two booleans, resolved for the language; ""
// means show nothing.
type ResolvedReading struct {
	// Format is the effective name entity.Formats admits, or "" for the plain
	// reading of the type.
	Format  string `json:"format"`
	Missing string `json:"missing"`
	True    string `json:"true"`
	False   string `json:"false"`
	// Precision is the decimal places of the type: 0 for an int, null (as
	// stored) for everything else.
	Precision *int           `json:"precision"`
	Time      *ResolvedTime  `json:"time,omitempty"`
	Money     *ResolvedMoney `json:"money,omitempty"`
}

// ResolvedEditable says whether a create or an update form takes the field. It is a
// property of the schema; the caller's grant is the entry's `writable`.
type ResolvedEditable struct {
	Create bool `json:"create"`
	Update bool `json:"update"`
}

// ResolvedField is one schema field's policy.
type ResolvedField struct {
	// Choice is absent for a field that offers no answers to choose.
	Choice *ResolvedChoice     `json:"choice,omitempty"`
	Figure entity.ChoiceFigure `json:"figure"`
	Name   string              `json:"name"`
	Label  string              `json:"label"`
	Help   string              `json:"help"`
	// Section is a declared section key, or "".
	Section string `json:"section"`
	// OnList and OnRecord are the effective surface membership, after
	// visibility and `hideList` have been reconciled (Field.OnList).
	OnList   bool             `json:"onList"`
	OnRecord bool             `json:"onRecord"`
	Editable ResolvedEditable `json:"editable"`
	// Control is the effective widget a form draws; Kind is how a read draws.
	Control string          `json:"control"`
	Kind    string          `json:"kind"`
	Reading ResolvedReading `json:"reading"`
	// Options is [] for a field that is not an enum.
	Options []ResolvedOption `json:"options"`
	// Reference is absent when the author guessed no relationship.
	Reference *ResolvedReference `json:"reference,omitempty"`
}

// ResolvedSortOption is one order a list offers: Query is the exact `sort` parameter.
type ResolvedSortOption struct {
	Query string `json:"query"`
	Label string `json:"label"`
	Field string `json:"field"`
	Desc  bool   `json:"desc"`
}

// ResolvedFilter describes one field a list may be filtered on: the parameter is
// `filter=<Field>:<value>`, with the values listed when the set is closed.
type ResolvedFilter struct {
	Field  string           `json:"field"`
	Label  string           `json:"label"`
	Kind   string           `json:"kind"`
	Values []ResolvedOption `json:"values"`
}

// ResolvedList is how a list is ordered and narrowed.
type ResolvedList struct {
	// DefaultSort is the exact `sort` value to send: `-createdAt` where the
	// schema has it, else "" to omit the parameter.
	DefaultSort string               `json:"defaultSort"`
	Sorts       []ResolvedSortOption `json:"sorts"`
	Filters     []ResolvedFilter     `json:"filters"`
}

// ResolvedConfirmation is the copy read before a command runs.
type ResolvedConfirmation struct {
	Title        string `json:"title"`
	Body         string `json:"body"`
	ConfirmLabel string `json:"confirmLabel"`
}

// ResolvedCommand is one command the caller may call, as a button reads. A command
// the caller may not call is not listed.
type ResolvedCommand struct {
	Fields      []ResolvedField `json:"fields"`
	Verb        string          `json:"verb"`
	Label       string          `json:"label"`
	Visible     bool            `json:"visible"`
	System      bool            `json:"system"`
	Primary     bool            `json:"primary"`
	Destructive bool            `json:"destructive"`
	// Confirmation is absent when the command asks nothing first.
	Confirmation   *ResolvedConfirmation `json:"confirmation,omitempty"`
	SuccessMessage string                `json:"successMessage"`
}

// ResolvedDeckDirection is one gesture of the deck, already narrowed to the commands
// the caller may run.
type ResolvedDeckDirection struct {
	Command string `json:"command"`
	Label   string `json:"label"`
	Tone    string `json:"tone"`
	Icon    string `json:"icon"`
	Skip    bool   `json:"skip"`
	Undo    string `json:"undo"`
}

// ResolvedDeck is the swipe deck's policy: Queue and Order use the list's grammar.
type ResolvedDeck struct {
	Undoable   bool                             `json:"undoable"`
	Queue      []string                         `json:"queue"`
	Order      string                           `json:"order"`
	Directions map[string]ResolvedDeckDirection `json:"directions"`
}

// ResolvedSummary carries the declared sub-count and trend of the entry's cards.
type ResolvedSummary struct {
	// Highlight is absent when none is declared; its label is resolved.
	Highlight  *entity.EntryHighlight `json:"highlight,omitempty"`
	TrendField string                 `json:"trendField"`
}
