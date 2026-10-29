package fault_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestArchitectureCompositionCitationsNameTheCallTheirSentenceNames reads the
// composition sentences of ARCHITECTURE.md the way their reader does: where the
// prose names a call and cites the line it sits on, that line must hold the call.
// A citation may stay inside its file and still land on a neighbour's comment,
// which is the drift a shortened comment causes silently — every line below the
// block it shortens moves up, and the number in the document is the only witness.
//
// It walks the shorthand `:N` as a reader does, as a line of the last path the
// sentence named, and it asks nothing of a running application. Where the prose
// stops naming a claim this way, the case fails by that claim's name rather than
// passing on nothing.
func TestArchitectureCompositionCitationsNameTheCallTheirSentenceNames(t *testing.T) {
	root := filepath.Join("..", "..")
	doc, err := os.ReadFile(filepath.Join(root, "ARCHITECTURE.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc)

	// Each row is one claim of the implementation map: the sentence that makes it,
	// the file it cites and the code it says is there.
	for _, row := range []struct{ claim, path, code string }{
		{"The theme is chosen once, at", "apps/platformkit/app.go", "Theme(design.Default())"},
		{"the same pair the composition hands the shell at", "apps/platformkit/app.go", "Theme(design.Default())"},
		{"The reference product's own", "apps/platformkit/fault.go", "ui.Compose(design.Default())"},
		{"write one transport line", "apps/platformkit/product.go", "return app.Transports{Memory: memory.New, JetStream: eventnats.Connect}"},
		{"write one transport line", "apps/platformkit/product.go", "func transports() app.Transports {"},
		{"so a deployment without mail records everything", "apps/platformkit/main.go", "case deployment == pkit.Development:"},
		{"so a deployment without mail records everything", "apps/platformkit/main.go", "mail.host is empty"},
		{"A refusal a person must see raises a", "apps/platformkit/product.go", "notificationcontracts.Notice{"},
		{"the reference composition names `valkey.Connect`", "apps/platformkit/product.go", "return app.Caches{Valkey: valkey.Connect}"},
		{"the reference composition names `valkey.Connect`", "apps/platformkit/product.go", "func caches() app.Caches {"},
		{"and `Value`", "kit/cache/cache.go", "type Value struct {"},
		{"the adapter is `kit/cache/providers/valkey`", "kit/cache/providers/valkey/valkey.go", "func New("},
		{"the adapter is `kit/cache/providers/valkey`", "kit/cache/providers/valkey/valkey.go", "func Connect("},
	} {
		paragraph, ok := paragraphHolding(text, row.claim)
		if !ok {
			t.Errorf("ARCHITECTURE.md no longer says %q, so the citation of %s it made is unchecked", row.claim, row.path)
			continue
		}
		if holds, cited := sentenceCites(paragraph, row.path, row.code); !holds {
			t.Errorf("ARCHITECTURE.md says %q holds %q (%s), and %s", row.claim, row.code, row.path, cited)
		}
	}
}

// paragraphHolding returns the block around the first occurrence of claim: the
// prose between the blank lines that bound it, which is the unit a citation's
// shorthand continues within. The claim is matched word by word, because the
// document wraps its sentences.
func paragraphHolding(text, claim string) (string, bool) {
	words := strings.Fields(claim)
	for i, w := range words {
		words[i] = regexp.QuoteMeta(w)
	}
	at := -1
	if found := regexp.MustCompile(strings.Join(words, `\s+`)).FindStringIndex(text); found != nil {
		at = found[0]
	}
	if at < 0 {
		return "", false
	}
	start := 0
	if sep := strings.LastIndex(text[:at], "\n\n"); sep >= 0 {
		start = sep + 2
	}
	end := strings.Index(text[at:], "\n\n")
	if end < 0 {
		return text[start:], true
	}
	return text[start : at+end], true
}

// compositionCitation matches one code span holding a path, a line number, or both:
// the document cites `path/file.go:NN`, `path/file.go:NN-MM`, and shorthand `:NN`
// for a line of the path its sentence named last.
var compositionCitation = regexp.MustCompile(
	"`(?:(?P<path>[A-Za-z0-9_./-]+\\.(?:go|sql|md|json|ts|tsx|sh|py|mjs|js|ya?ml|yml|rego))(?::(?P<from>\\d+)(?:\\s*[-–]\\s*(?P<to>\\d+))?)?|:(?P<short>\\d+)(?:\\s*[-–]\\s*(?P<shortTo>\\d+))?)`")

// sentenceCites answers whether some citation of path in the paragraph lands on a
// line holding code, and describes the citations it tried when none does.
func sentenceCites(paragraph, path, code string) (bool, string) {
	var (
		current  string
		tried    []string
		external = filepath.Join("..", "..")
	)
	for _, m := range compositionCitation.FindAllStringSubmatch(paragraph, -1) {
		group := m[1]
		from, to := m[2], m[3]
		if group == "" {
			if m[4] == "" {
				continue // a named file with no line to check
			}
			group, from, to = current, m[4], m[5] // shorthand: the path named before it
		}
		current = group
		if from == "" || current != path {
			continue
		}
		lines, err := readLines(filepath.Join(external, current))
		if err != nil {
			tried = append(tried, err.Error())
			continue
		}
		start, _ := strconv.Atoi(from)
		end := start
		if to != "" {
			end, _ = strconv.Atoi(to)
		}
		if start < 1 || end > len(lines) {
			tried = append(tried, path+" has "+strconv.Itoa(len(lines))+" lines, and the citation names "+from)
			continue
		}
		if strings.Contains(strings.Join(lines[start-1:end], "\n"), code) {
			return true, ""
		}
		tried = append(tried, path+":"+from+" holds "+strings.TrimSpace(lines[start-1]))
	}
	if len(tried) == 0 {
		return false, "no citation of " + path + " is left in that sentence"
	}
	return false, "what it names instead: " + strings.Join(tried, "; ")
}
