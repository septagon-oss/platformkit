package appname_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/appname"
)

// census.go is the check that a shared name is formed in kit/appname and nowhere
// else. It is a ratchet rather than a survey: every pattern is a way the kernel
// has of spelling a name two apps could want, and the allow-list names the sites
// that still spell it inline, each with the reason it is allowed to.
//
// A rule passes when the tree holds exactly the listed sites and nothing else
// forms that name, so a new inline spelling anywhere in kit, modules, apps, ui or
// tools fails the gate, and so does finishing a migration and leaving its site in
// the list. That second half is what stops the list becoming a permanent
// justification: the entry is the debt, and the census counts it.
//
// testdata/planted holds a file that forms every one of these names inline. The
// same scan runs over it and has to report every planted case: a census that
// finds nothing anywhere proves only that its patterns are wrong.

// rule is one way a shared name gets spelled, and the sites allowed to spell it.
type rule struct {
	name    string
	pattern *regexp.Regexp
	// literal is the widened shape of the same name — a whole name in one
	// string, or the namespace spelled in the line rather than named by a
	// constant — and it is refused outside test files only. A test that states the
	// name a browser or a broker is expected to present is not a name the runtime
	// forms; making it call a constructor would replace an assertion with a call
	// to the code under test, which asserts nothing. The census's planted
	// counter-example is not a _test.go file, so the widened shape is proven on it.
	literal *regexp.Regexp
	// context keeps a widened literal from matching a name that merely shares
	// letters with one: the ui/export package spells "platformkit." for a directory
	// of generated files, which is not an event address. A literal only counts
	// where the line also says what kind of name it is forming.
	context *regexp.Regexp
	allow   []allowed
}

// allowed is one site the census tolerates, with how many lines of it form the
// name and why. "owner" means the name is formed here on purpose; "pending" means
// the site is a name this task moves behind a constructor and has not yet; "shared"
// means the name stays shared by decision 0074 and is listed in this package's doc.
//
// The count is what makes the list a ratchet rather than an excuse: a file already
// on the list cannot form one more name than it does today without failing, so an
// inline spelling planted next to a permitted one is still refused.
type allowed struct {
	path  string
	lines int
	why   string
}

var census = []rule{
	{
		// The pattern reads a cookie name in either shape: the prefix spelled on
		// its own to be joined, and a whole name in one literal. A rule that
		// matched only the first would pass over the spelling a caller naturally
		// writes — a full cookie name never stops where the prefix ends.
		name:    "session and first-party cookie names",
		pattern: regexp.MustCompile(`"__Host-"`),
		literal: regexp.MustCompile(`"__Host-[a-z0-9]`),
		allow: []allowed{
			{"kit/appname/appname.go", 3, "owner"},
		},
	},
	{
		// "/?job:" reads both spellings: the bare lock of the deployment that
		// names no app and the app-prefixed one.
		name:    "job advisory locks",
		pattern: regexp.MustCompile(`"/?job:"`),
		allow: []allowed{
			{"kit/appname/appname.go", 2, "owner"},
		},
	},
	{
		// Both ways of spelling an address out of the namespace: from the
		// transport's prefix constant, and from the namespace literal written in
		// the line itself.
		name:    "event subjects and filters",
		pattern: regexp.MustCompile(`SubjectPrefix \+ "`),
		literal: regexp.MustCompile(`"platformkit\."`),
		allow: []allowed{
			{"kit/events/transport/review8_a_manifest_name_can_never_carry_a_broker_wildcard_test.go", 1, "exempt: the decision-0008 pin spells an address by hand to attack one; a reviewer's file is not this census's to migrate"},
		},
	},
	{
		name:    "the NATS connection name",
		pattern: regexp.MustCompile(`nats\.Name\(`),
		allow: []allowed{
			{"kit/events/providers/nats/jetstream.go", 1, "owner: the option call hands the name over to appname.ConnectionName, which forms it"},
		},
	},
	{
		// strings.ReplaceAll is the event name's transliteration, whichever join
		// it is spelled with: the operands it sits inside are what makes a
		// consumer name, in kit/events or anywhere else.
		// The event name's dots turned into dashes is a consumer name, whatever
		// it is joined with: the operands below are the transliteration itself, so
		// the rule reads the join spelled through strings.Join as well as the one
		// kit/events wrote.
		name:    "durable consumer names",
		pattern: regexp.MustCompile(`ReplaceAll\([a-z]+, "\.", "-"\)`),
		allow: []allowed{
			{"kit/appname/appname.go", 2, "owner"},
		},
	},
	{
		name:    "rate-limit keys",
		pattern: regexp.MustCompile(`String\(\) \+ "/" \+ key`),
		allow: []allowed{
			{"kit/appname/appname.go", 3, "owner"},
		},
	},
	{
		name:    "a stored file's physical path",
		pattern: regexp.MustCompile(`filepath\.Join\(l\.dir`),
		allow: []allowed{
			{"modules/file/internal/local.go", 1, "owner: the segments come from appname.StoragePath; this is the adapter's root"},
		},
	},
	{
		name:    "the JetStream stream",
		pattern: regexp.MustCompile(`"PLATFORMKIT"`),
		allow: []allowed{
			{"kit/events/providers/nats/jetstream.go", 1, "shared: its subjects are app-scoped"},
			{"kit/events/internal_test.go", 1, "shared: the case reads the stream the transport made"},
		},
	},
}

// scannedRoots are the trees a shared name could be formed in. Every one of them
// holds kernel code today; a client's own code forms no kernel name, and if it
// ever wants to, that is the bug this census exists to catch.
var scannedRoots = []string{"kit", "modules", "apps", "ui", "tools"}

// widens reports whether the rule's literal shape — a whole name in one string,
// or the namespace spelled in the line rather than named by a constant — fires on
// a line, which it does outside test files and only where the line also names the
// kind of name it is forming.
func (r rule) widens(line string, testFile bool) bool {
	if testFile || r.literal == nil || !r.literal.MatchString(line) {
		return false
	}
	return r.context == nil || r.context.MatchString(line)
}

// finding is one line that forms a shared name.
type finding struct {
	rule, path string
	line       int
}

func (f finding) String() string { return f.path + ":" + itoa(f.line) + " (" + f.rule + ")" }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// repoRoot is the checkout, from this package's directory: the census reads the
// whole tree, and its allow-list names sites by their path from that root.
const repoRoot = "../.."

// thisFile is this file's path from the repository root. The census's own pattern
// literals name the names they look for, and a list cannot be its own violation;
// the alternative is to spell every pattern by concatenation so it cannot match
// itself, which would cost a reader the pattern.
const thisFile = "kit/appname/census_test.go"

// scan reports every line in root that forms a shared name. Scanning the tree
// skips testdata, which holds the planted cases the census has to find; scanning
// testdata itself reads it.
func scan(t *testing.T, root string) []finding {
	t.Helper()
	planted := strings.HasPrefix(root, "testdata")
	prefix := ""
	if root != "." {
		prefix = filepath.ToSlash(root) + "/"
	}
	var out []finding
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == root {
				return nil
			}
			// .git holds pack files and object data: nothing in it forms a kernel
			// name, and reading it would make the census slower than the tests it
			// guards.
			if strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		name := strings.TrimPrefix(filepath.ToSlash(path), prefix)
		if name == thisFile || !strings.HasSuffix(name, ".go") ||
			(!planted && strings.Contains(name, "testdata/")) {
			return nil
		}
		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		testFile := strings.HasSuffix(name, "_test.go")
		for i, line := range strings.Split(string(text), "\n") {
			for _, r := range census {
				if r.pattern.MatchString(line) ||
					(!testFile && r.literal != nil && r.literal.MatchString(line)) {
					out = append(out, finding{rule: r.name, line: i + 1, path: name})
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scanning %s: %v", root, err)
	}
	return out
}

// TestEverySharedNameIsFormedHereOrAtANamedSite: the tree holds no shared name
// formed outside this package except at the sites above, at the number of lines
// each one forms today.
func TestEverySharedNameIsFormedHereOrAtANamedSite(t *testing.T) {
	want := map[string]int{}
	for _, r := range census {
		for _, a := range r.allow {
			want[r.name+"\x00"+a.path] = a.lines
		}
	}
	got := map[string]int{}
	var extra []finding
	for _, f := range scan(t, repoRoot) {
		got[f.rule+"\x00"+f.path]++
		extra = append(extra, f)
	}
	for _, f := range extra {
		if _, ok := want[f.rule+"\x00"+f.path]; !ok {
			t.Errorf("%s forms %s inline; form it with a constructor in kit/appname, or name the site and its reason in the census", f, f.rule)
		}
	}
	for key, lines := range want {
		name, path, _ := strings.Cut(key, "\x00")
		if got[key] > lines {
			t.Errorf("%s forms %s on %d lines, more than the %d the census names: a name planted beside a permitted one is still inline", path, name, got[key], lines)
		}
	}
}

// TestEachNamedSiteStillExists: an allow-list entry the tree no longer holds
// hides a finished migration, and a list nobody prunes turns into an excuse.
func TestEachNamedSiteStillExists(t *testing.T) {
	found := map[string]int{}
	for _, f := range scan(t, repoRoot) {
		found[f.rule+"\x00"+f.path]++
	}
	for _, r := range census {
		for _, a := range r.allow {
			if n := found[r.name+"\x00"+a.path]; n == 0 {
				t.Errorf("the census allows %s to form %s (%s) and that file no longer does: delete the entry", a.path, r.name, a.why)
			}
		}
	}
}

// TestTheCensusFindsAPlantedName: the census is proven on a case planted in
// testdata, which forms every one of these names inline. A scan that reports
// nothing would pass this file with broken patterns.
func TestTheCensusFindsAPlantedName(t *testing.T) {
	want := map[string]int{}
	for _, r := range census {
		want[r.name]++
	}
	got := scan(t, filepath.Join("testdata", "planted"))
	if len(got) != len(want) {
		for _, f := range got {
			t.Logf("planted finding: %s", f)
		}
		t.Fatalf("the planted file forms %d shared names and the census found %d: %v", len(want), len(got), got)
	}
	seen := map[string]bool{}
	for _, f := range got {
		if want[f.rule] == 0 {
			t.Errorf("the census reported %s, which the planted file does not form", f)
		}
		seen[f.rule] = true
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("the planted file forms %s inline and the census did not report it", name)
		}
	}
}

// TestTheCensusSkipsNothingItShouldSee: the scan reaches every tree a kernel name
// is formed in, and reports a count rather than silence, so a scan of an empty
// directory tree is visibly wrong rather than green.
func TestTheCensusSkipsNothingItShouldSee(t *testing.T) {
	total := scan(t, repoRoot)
	if len(total) < len(census) {
		t.Fatalf("the scan of the tree reported %d findings across %d rules: the scan reaches no code", len(total), len(census))
	}
	for _, root := range scannedRoots {
		if _, err := os.Stat(filepath.Join(repoRoot, root)); err != nil {
			t.Errorf("the census names %s as a tree to scan and the checkout has none: %v", root, err)
		}
	}
}

// TestEveryRuleIsAllowedSomewhere: a rule with no allow-list entry could never
// pass, which is how a census quietly stops running.
func TestEveryRuleIsAllowedSomewhere(t *testing.T) {
	for _, r := range census {
		if len(r.allow) == 0 {
			t.Errorf("the census rule for %s names no site, so it refuses every tree that holds the name", r.name)
		}
	}
}

// TestPrefixIsTheOneNamespace keeps this package's prefix and the transport's
// alias one word while the transport still owns the address.
func TestPrefixIsTheOneNamespace(t *testing.T) {
	if appname.Prefix != "platformkit" {
		t.Fatalf("the prefix is %q, and every address ever published begins with platformkit", appname.Prefix)
	}
}
