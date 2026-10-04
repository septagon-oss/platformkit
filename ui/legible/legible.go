// Package legible reads a served document and says which of its strings a
// localisation formatter reached.
//
// It is the measuring half of the pseudo-locale gate: [Scan] hands back every
// string a person could read in a response body, each one tagged as copy a
// catalogue answered or not, [MarkDatum] adds what the application itself holds as
// a value a person typed, and [Exempt] answers what the gate may not count as
// copy — an id, an amount, a timestamp, a file name. [Report] prints the whole
// verdict, named strings and declined strings together. Neither holds a page list,
// a route, a tenant or a module name: a rule that exempted by position would report
// a flattering number that no change could lower, so the predicate looks only at what
// a string *is*, and what it cannot tell from the string it is told.
//
// The package reads documents and never writes one. It parses with
// golang.org/x/net/html and starts no services.
package legible

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// String is one string a person reads in a served document.
type String struct {
	// Path locates the string in the document, element by element from the root:
	// `html[1] > body[2] > main[1] > div[1]`. A text node's path is its parent's
	// with `#text` on the end. The index counts *element* siblings, so a text node
	// or a comment moving cannot renumber a path a reviewer diffed.
	Path string
	// Text is the string as it reads once the parser resolved its character
	// references, with the whitespace of the source left in.
	Text string
	// Attribute names the attribute the string came from — `alt`, `title`,
	// `aria-label`, `placeholder`. Empty means a text node.
	Attribute string
	// Reached says the string carries the pseudo-locale's mark, and so went through
	// a catalogue. A string that merely looks accented does not count; the mark is
	// what the provider that produced it says is a mark (see Reached).
	Reached bool
	// Datum says this application holds the string as something a person typed — a
	// task's title, a page's body, a plan's name — because MarkDatum was handed the
	// value. Such a string is a sentence, and so is never copy: no catalogue can hold
	// what a row stores. Only the write that stored it can say, which is why this
	// arrives as data rather than being guessed from the string's shape.
	Datum bool
}

// Reached decides whether one collected string came from a catalogue. The
// pseudo-locale provider owns that shape — it wrote it — so the scan is handed the
// decision rather than a second copy of the delimiters, and this package names no
// locale at all.
type Reached func(text string) bool

// Attributes are the element attributes whose value a person reads. The set is the
// gate's: a replacement string, a caption, an accessible name, a prompt.
var Attributes = []string{"alt", "title", "aria-label", "placeholder"}

// skip lists the elements whose children are not a document's copy. `<title>` is
// absent on purpose: what a tab says is copy, and `aria-hidden` is absent too —
// hiding a node from assistive technology does not make an English label
// translatable, and honouring it would hand out a way to switch this gate off.
var skip = map[string]bool{"script": true, "style": true, "template": true, "noscript": true}

// Scan collects every string a person reads in one response body. It is a pure
// function of the bytes it was handed — no clock, no locale, no file — so a case
// can assert the whole list against a literal.
//
// reached may be nil, which collects everything as unreached: useful for a caller
// that wants the strings and not the verdict.
func Scan(body []byte, reached Reached) ([]String, error) {
	document, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("legible: the document cannot be parsed: %w", err)
	}
	var out []String
	var walk func(node *html.Node, path string)
	walk = func(node *html.Node, path string) {
		switch node.Type {
		case html.TextNode:
			if canon(node.Data) != "" {
				out = append(out, String{Path: path + "#text", Text: node.Data,
					Reached: reaches(reached, node.Data)})
			}
		case html.ElementNode:
			for _, attribute := range Attributes {
				for _, have := range node.Attr {
					if have.Key != attribute || canon(have.Val) == "" {
						continue
					}
					out = append(out, String{Path: path, Text: have.Val, Attribute: attribute,
						Reached: reaches(reached, have.Val)})
				}
			}
		}
		if node.Type == html.ElementNode && skip[node.Data] {
			return
		}
		for i, child := 0, node.FirstChild; child != nil; child = child.NextSibling {
			next := path
			if child.Type == html.ElementNode {
				i++
				next = step(path, child.Data, i)
			}
			walk(child, next)
		}
	}
	// Walking from the document node puts the root element at `html[1]`, which is
	// how every path here is spelled: no path starts above the root element.
	walk(document, "")
	return out, nil
}

// MarkDatum tags the collected strings that read back as one of the given values —
// the sentences a person typed into this application, handed over by whoever wrote
// them. Copy and data are both sentences, so no rule over a string's shape tells them
// apart: "Pump room inspection" and "Back to the workspace" are the same kind of text
// and only one of them can ever join a catalogue. The caller that stored the first is
// the only witness the second has, so a gate hands over what it typed through the same
// doors a person uses.
//
// A value matches the whole string, whitespace folded, never a word inside it:
// exempting every line that mentions a task's title would exempt the sentence written
// around it, which is copy. Marking copy that a catalogue already reached changes no
// verdict — reached strings are neither violation nor exempt.
func MarkDatum(collected []String, values []string) []String {
	if len(values) == 0 {
		return collected
	}
	typed := make(map[string]bool, len(values))
	for _, value := range values {
		if key := canon(value); key != "" {
			typed[key] = true
		}
	}
	out := make([]String, len(collected))
	for i, s := range collected {
		s.Datum = typed[canon(s.Text)]
		out[i] = s
	}
	return out
}

// Violations are the strings a person reads that went around the catalogue: not
// marked, not the kind of string that is data rather than copy, and not a value this
// application stores. They are the denominator's other half, and the list a
// translation task works through.
func Violations(collected []String) []String {
	var out []String
	for _, s := range collected {
		if !s.Reached && !s.Datum && !Exempt(s.Text) {
			out = append(out, s)
		}
	}
	return out
}

// Exempted are the unmarked strings the number does not count — ids, amounts,
// timestamps, and the values MarkDatum recognised. They are never hidden from a
// reviewer: Report prints them, because a number that quietly chose not to count
// something is not a measurement.
func Exempted(collected []String) []String {
	var out []String
	for _, s := range collected {
		if !s.Reached && (s.Datum || Exempt(s.Text)) {
			out = append(out, s)
		}
	}
	return out
}

// Report renders one document's strings as lines, labelled by whatever the caller
// calls the document — the gate names one `GET /app/task/tasks/{id}` — and never by
// anything read off the page, because an attribution a document could write about
// itself is one a page could fake.
//
// The first list names every string that went around a catalogue as
// `TEXT <label> <path> "text"`; the second names every string the number declined to
// count as `DATA <label> <path> "text" <why>`, where why is `typed here` for a value
// the application stores and `data` for a shape Exempt recognised. In document order,
// so a reviewer reads a page top to bottom.
func Report(label string, collected []String) (unreachable, declined []string) {
	for _, s := range collected {
		switch {
		case s.Reached:
		case s.Datum:
			declined = append(declined, line("DATA", label, s, "typed here"))
		case Exempt(s.Text):
			declined = append(declined, line("DATA", label, s, "data"))
		default:
			unreachable = append(unreachable, line("TEXT", label, s, ""))
		}
	}
	return unreachable, declined
}

// line is one report line: its kind, the document, the path, the text quoted with its
// whitespace flattened, the attribute it came from when it came from one, and — for a
// DATA line — the rule that declined to count it.
func line(kind, label string, s String, why string) string {
	out := kind + " " + label + " " + s.Path + " \"" + strings.Join(strings.Fields(s.Text), " ") + "\""
	if s.Attribute != "" {
		out += " in " + s.Attribute
	}
	if why != "" {
		out += " " + why
	}
	return out
}

func step(path, name string, index int) string {
	mark := name + "[" + strconv.Itoa(index) + "]"
	if path == "" {
		return mark
	}
	return path + " > " + mark
}

func reaches(reached Reached, text string) bool {
	return reached != nil && reached(canon(text))
}
