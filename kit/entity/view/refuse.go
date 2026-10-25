package view

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/septagon-oss/platformkit/kit/entity/present"
)

// Refusal is why a page was not built, in three parts: the member that was wrong,
// what it should have held, and what it held. A caller branches on Field with
// errors.As or errors.Is and never parses a sentence, because a page that is not
// built is answered by the shell's own state, not by this package's prose. A refusal
// is always correctable: the caller reaches a page by changing what it passed.
type Refusal struct {
	Field string `json:"field"`
	Want  string `json:"want"`
	Got   string `json:"got"`
}

func (r Refusal) Error() string {
	msg := "view: no page: " + r.Field
	if r.Got != "" {
		msg += " " + r.Got
	}
	if r.Want != "" {
		msg += ", want " + r.Want
	}
	return msg
}

// Is reports whether target names the same member, so a caller may compare against a
// Refusal that carries only the member it branches on.
func (r Refusal) Is(target error) bool {
	t, ok := target.(Refusal)
	return ok && (t.Field == "" || t.Field == r.Field)
}

// refuse is what a caller of a pure builder can get wrong, and nothing else: no
// business rule, no permission grammar and no schema declaration is checked here,
// because each is refused by whoever owns it and a page grants nothing either way.
func refuse(in Input) error {
	if in.Policy.PolicyVersion != present.PolicyVersion {
		return Refusal{Field: "policyVersion", Got: strconv.Itoa(in.Policy.PolicyVersion),
			Want: "the revision this build reads, " + strconv.Itoa(present.PolicyVersion)}
	}
	switch in.State.Kind {
	case KindList, KindRecord, KindForm:
	default:
		return Refusal{Field: "kind", Got: quote(string(in.State.Kind)), Want: "list, record or form"}
	}
	if in.State.Language != "" && in.Policy.Language != "" && in.State.Language != in.Policy.Language {
		return Refusal{Field: "language", Got: quote(in.State.Language), Want: "the policy's " + quote(in.Policy.Language)}
	}
	if in.State.Now.IsZero() {
		return Refusal{Field: "builtAt", Want: "the instant the composition's clock read, so a relative phrase has an anchor"}
	}
	if in.Data.Counted && in.Data.Total < 0 {
		return Refusal{Field: "total", Got: strconv.FormatInt(in.Data.Total, 10), Want: "a count, or counted false"}
	}
	if err := refuseName(in.Resource); err != nil {
		return err
	}
	return refuseSurface(in)
}

// refuseSurface checks the page against what the resource answers and what the caller
// holds: a page for a route that answers nothing is a door with nothing behind it, a
// record page with no record is a fiction, and a form for a caller who may not write
// is a lie.
func refuseSurface(in Input) error {
	switch in.State.Kind {
	case KindList:
		if in.Data.Window.Limit <= 0 {
			return Refusal{Field: "window.limit", Got: strconv.Itoa(in.Data.Window.Limit),
				Want: "the page size the read asked for: this package keeps no page size"}
		}
		return refuseRoute(in, "list")
	case KindForm:
		if !in.State.Writable {
			return Refusal{Field: "writable", Want: "a write grant: this caller may not write"}
		}
		return refuseRoute(in, formVerb(in))
	case KindRecord:
		if len(in.Data.Records) == 0 {
			return Refusal{Field: "record", Want: "the row the page shows: a record page from no row is a fiction"}
		}
	}
	return nil
}

// refuseRoute is the one question a page may ask of a resource: whether it answers the
// verb this page is built around. A singleton answers no list and no create, so a
// create form for one is refused by the same line as a resource with no create route.
func refuseRoute(in Input, verb string) error {
	if in.Resource.offers(verb) {
		return nil
	}
	return Refusal{Field: "operations", Got: "no " + verb + " route", Want: "the " + verb + " route, or no page"}
}

// refuseName checks the one identity a Page carries: a resource is named
// `module/entity`, the shape a field's declared reference already uses, or is left
// blank for a page nobody registered.
func refuseName(d Description) error {
	if d.Resource == "" {
		return nil
	}
	if module, name, ok := strings.Cut(d.Resource, "/"); !ok || module == "" || name == "" || strings.Contains(name, "/") {
		return Refusal{Field: "resource", Got: quote(d.Resource), Want: `module/entity, or "" for a page nobody registered`}
	}
	return nil
}

// formVerb is what a form page is built around.
func formVerb(in Input) string {
	if in.State.Form.Create {
		return "create"
	}
	return "update"
}

func quote(s string) string { return cmp.Or(strconv.Quote(s), `""`) }

// sortedKeys is the lexicographic order of a map's keys, which is the only order a
// map may reach a Page through: nothing inside Build iterates a map for sequence.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
