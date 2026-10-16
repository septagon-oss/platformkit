package pkit_test

// The refusal rule of the machine-readable view, pinned for the fourth cause.
// TestDescribeRefusesWhatValidateRefuses covers Use, Choose and Deployment —
// refusals the resolver answers before any module's build runs. Build is the
// cause that arrives later: the resolution succeeded, every module's build ran,
// and one of them refused. The rule has to hold there too — resolved:false,
// no modules, no choices, no roles, and the problem's sentence is Validate's
// own — or a caller that reads the document after a build refusal would read
// the graph of a composition that does not exist.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/user"
)

func TestDescribeOfACompositionWhoseBuildFailsCarriesNoComposition(t *testing.T) {
	for _, tc := range []struct {
		what string
		mod  *pkit.Module
		said string
	}{
		{"a build that returns an error",
			pkit.NewModule("broken", func(w *pkit.Wiring) (module.Module, error) {
				return module.Module{}, errBoom
			}),
			"broken: boom"},
		{"a provider that never put its contract",
			pkit.NewModule("mute", func(w *pkit.Wiring) (module.Module, error) {
				return module.Module{Name: "mute"}, nil
			}, pkit.Provides[muteContract]()),
			"mute declares it provides pkit_test.muteContract and did not put it once"},
	} {
		app := pkit.NewApp("collect").Use(user.Module, tc.mod).
			Roles(pkit.Role{Name: "member", Grants: []string{"task.read"}})
		doc, out, err := described(t, app, dev)
		if err == nil {
			t.Errorf("%s: Describe answered without a refusal:\n%s", tc.what, out)
			continue
		}
		if doc.Resolved {
			t.Errorf("%s: a build refusal says resolved=true:\n%s", tc.what, out)
		}
		if len(doc.Modules) != 0 {
			t.Errorf("%s: the refusal still carries %d modules — a partial graph is a misleading one:\n%s",
				tc.what, len(doc.Modules), out)
		}
		if len(doc.Roles) != 0 || len(doc.Choices) != 0 {
			t.Errorf("%s: the refusal carries roles or choices: %+v", tc.what, doc)
		}
		if len(doc.Problems) != 1 || doc.Problems[0].Cause != "Build" {
			t.Errorf("%s: the problems are %+v, want one caused by Build", tc.what, doc.Problems)
			continue
		}
		said := strings.TrimPrefix(app.Validate(dev).Error(), "pkit: collect: Build: ")
		if doc.Problems[0].Sentence != said || said != tc.said {
			t.Errorf("%s: the problem says %q, want Validate's %q (and that to be %q)",
				tc.what, doc.Problems[0].Sentence, said, tc.said)
		}
	}
}

type muteContract interface{ Mute() }

var errBoom = errBoomType{}

type errBoomType struct{}

func (errBoomType) Error() string { return "boom" }
