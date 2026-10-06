package internal_test

// The auth module's copy, held to the same gate ui/page holds its own refusal catalogue to
// (ui/page/catalogue_test.go, review_r3_verdict_copy_test.go): the keys are read out of the
// file that asks for them, not written down beside it, and the catalogue is read out of the
// module's own embedded messages. A list hand-copied here would be silent the day the page
// refuses with a fifth sentence — which is exactly how the attempt-limit refusal reached a
// Portuguese reader in English (review round 9, finding 2): the page's two translated
// sentences were tested and its two untranslated ones were not, and nothing counted them.

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/ui/page"
)

// confirmationPageKeys is every catalogue key the module's one page file can ask for: the
// refusal keys it names as constants, and the page's own lines, which it asks for by
// fragment through its copy reader. The file, not this list, is the truth.
func confirmationPageKeys(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "ui/page.go", nil, 0)
	if err != nil {
		t.Fatalf("read the module's page file: %v", err)
	}
	var keys []string
	ast.Inspect(file, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok {
			// words.text("title", …) reads auth.verify.title; the fragment is the
			// page's own line and the prefix is the copy reader's.
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "text" && len(call.Args) > 0 {
				if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					keys = append(keys, "auth.verify."+strings.Trim(lit.Value, `"`))
				}
			}
			return true
		}
		value, ok := node.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range value.Names {
			lit, ok := value.Values[i].(*ast.BasicLit)
			if !ok {
				continue
			}
			text := strings.Trim(lit.Value, `"`)
			if strings.HasPrefix(name.Name, "key") && strings.HasPrefix(text, "auth.") {
				keys = append(keys, text)
			}
		}
		return true
	})
	if len(keys) < 2 {
		t.Fatalf("the page names %d refusal keys, which means this gate stopped reading it", len(keys))
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

// TestEveryLineTheConfirmationPageAsksForIsCopiedInTheLanguagesTheModuleSpeaks.
func TestEveryLineTheConfirmationPageAsksForIsCopiedInTheLanguagesTheModuleSpeaks(t *testing.T) {
	speaks := locale.SelectLocale(xtext.Load("en", page.Catalogue(), auth.Catalogue()), "pt-PT")
	if speaks.Language != "pt-PT" {
		t.Fatalf("the merged composition answered %q, not pt-PT", speaks.Language)
	}
	for _, key := range confirmationPageKeys(t) {
		if got := speaks.Text(key, "\x00nothing"); got == "\x00nothing" {
			t.Errorf("the confirmation page refuses under %q, which the module's catalogue carries no "+
				"Portuguese sentence for, so a Portuguese reader is shown the English literal", key)
		}
	}
}

// TestTheModuleCatalogueCarriesNoCopyThePageCannotAskFor. Copy nothing renders goes stale in
// silence, the same rule ui/page/catalogue_test.go holds on its own file.
func TestTheModuleCatalogueCarriesNoCopyThePageCannotAskFor(t *testing.T) {
	body, err := fs.ReadFile(auth.Catalogue().FS, "pt-PT.json")
	if err != nil {
		t.Fatalf("read the module's catalogue: %v", err)
	}
	var shipped map[string]json.RawMessage
	if err := json.Unmarshal(body, &shipped); err != nil {
		t.Fatalf("the module's catalogue is not gotext JSON: %v", err)
	}
	wanted := map[string]bool{}
	for _, key := range confirmationPageKeys(t) {
		wanted[key] = true
	}
	for key := range shipped {
		if !strings.HasPrefix(key, "auth.verify.") {
			continue // a permission's label is asked for by ui/page's grant denial, not by this page
		}
		if !wanted[key] {
			t.Errorf("the module's catalogue carries %q, which no refusal on its page asks for", key)
		}
	}
}
