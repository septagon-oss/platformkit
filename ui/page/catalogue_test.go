package page

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
)

// raisedFaults is every sentence this package can put on a refusal page: the codes
// kit/httpx publishes a row in faultKeys for, and every verdict faultKey keys by
// status because the router, the recoverer or a guard wrote its sentence.
//
// Nothing in here is hand-written but `faultKeys` itself, which the first half
// reads. The verdicts are asked of `faultKey`, for every status a response can
// carry, so a status added to that switch joins this list in the commit that added
// it — and from both directions: its key is looked for in the file, and a key in the
// file that no verdict and no code asks for still fails below. A list of statuses
// written here could not say that, because the day a fourth status joined faultKey's
// switch the list would simply be silent about it — the mutation review round 3 ran.
func raisedFaults(t *testing.T) []string {
	t.Helper()
	keys := make([]string, 0, len(faultKeys)+4)
	for _, key := range faultKeys {
		keys = append(keys, key)
	}
	keys = append(keys, deniedPermissionKey) // the denial that names its permission, asked for in fault()
	for code := 100; code < 600; code++ {
		if key, lookup := faultKey("", code); lookup {
			keys = append(keys, key)
		}
	}
	// The parts of the refusal page are looked up by name rather than by a code,
	// and they are copy a person reads: a missing entry is the same defect one
	// sentence down.
	keys = append(keys, refusalParts...)
	slices.Sort(keys)
	return slices.Compact(keys)
}

// Every refusal this package can show is answered in the language the deployment
// ships. A key the code can ask for and the file does not carry is a refusal an
// English speaker and a Portuguese speaker read differently, with no test failing
// anywhere; a key the file carries and no code ever asks for is copy nobody
// renders, which goes stale in the same silence.
func TestTheCatalogueAnswersEveryRefusalThisPackageCanShow(t *testing.T) {
	t.Parallel()
	body, err := catalogues.ReadFile("messages/pt-PT.json")
	if err != nil {
		t.Fatal(err)
	}
	var shipped map[string]struct {
		Translation string `json:"translation"`
	}
	if err := json.Unmarshal(body, &shipped); err != nil {
		t.Fatalf("messages/pt-PT.json is not gotext JSON: %v", err)
	}
	raised := raisedFaults(t)

	for _, key := range raised {
		if message, ok := shipped[key]; !ok || message.Translation == "" {
			t.Errorf("the catalogue carries no Portuguese sentence for %q, which a refusal can be shown under", key)
		}
	}
	for key := range shipped {
		if !slices.Contains(raised, key) {
			t.Errorf("the catalogue carries %q, which no refusal in this package can ask for", key)
		}
	}
}

// The file, not a builder: a person who reads the deployment's Portuguese has to
// be able to find the sentence in the catalogue and have it be the one the page
// says. This is the whole chain — embed, Load, Select, Text — in one assertion.
func TestTheShippedSentencesAreTheOnesTheNegotiationAnswersWith(t *testing.T) {
	t.Parallel()
	messages := locale.SelectLocale(xtext.Load("en", Catalogue()), "pt-PT")
	if messages.Language != "pt-PT" {
		t.Fatalf("a Portuguese request was answered in %q", messages.Language)
	}
	if got := messages.Text("fault."+httpx.CodeDenied, "AUTH_DENIED: this operation requires note.write"); got != "Não pode fazer isto." {
		t.Errorf("the refusal said %q, not the sentence messages/pt-PT.json carries", got)
	}

	// The source language has an entry of its own: an English request is answered
	// with the sentence messages/en.json carries, in the language it declares. What
	// the guard's own line still contributes — the permission name, the subsystem,
	// the address — stays on the page beside it, which is asserted where the page is
	// actually rendered (fault_denied_permission_test.go, fault_outage_language_test.go,
	// serve_refusal_language_test.go), not here at the loader.
	english := locale.SelectLocale(xtext.Load("en", Catalogue()), "en-GB,en;q=0.9")
	if english.Language != "en" {
		t.Fatalf("an English request was answered in %q, which is what a catalogue with an English file must not do", english.Language)
	}
	if got := english.Text("fault."+httpx.CodeDenied, "AUTH_DENIED: this operation requires note.write"); got != "You may not do this." {
		t.Errorf("the English refusal said %q, not the sentence messages/en.json carries", got)
	}
}

// The copy this package writes itself — the labels, the parts of a denial, the
// confirmation the ask lands on — is in both catalogues: an English label glued into
// a Go file is the same defect as a Portuguese tenant served an English label, one
// language further out. This is the page's own list of what it asks for, so the gate
// is on the page rather than on a file: a label added to fault.go without its English
// fails here, in the commit that added it. That messages/en.json now carries a guard's
// key beside its own is the two-catalogue contract refusal_catalogues_test.go holds:
// the guard's sentence is kept on the page in the source language, so a shipped entry
// there costs nobody the permission or the number only the refusal knows.
func TestThePagesOwnLinesAreCopiedInBothCatalogues(t *testing.T) {
	t.Parallel()
	english := ownCopy()
	portuguese := map[string]string{}
	body, err := catalogues.ReadFile("messages/pt-PT.json")
	if err != nil {
		t.Fatal(err)
	}
	var shipped map[string]struct {
		Translation string `json:"translation"`
	}
	if err := json.Unmarshal(body, &shipped); err != nil {
		t.Fatal(err)
	}
	for key, message := range shipped {
		portuguese[key] = message.Translation
	}
	wanted := map[string]bool{deniedPermissionKey: true}
	for _, key := range refusalParts {
		wanted[key] = true
	}
	for key := range wanted {
		if strings.TrimSpace(english[key]) == "" {
			t.Errorf("the refusal page asks for %q and messages/en.json carries no English for it", key)
		}
		if strings.TrimSpace(portuguese[key]) == "" {
			t.Errorf("the refusal page asks for %q and messages/pt-PT.json carries no Portuguese for it", key)
		}
	}
}
