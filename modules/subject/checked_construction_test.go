package subject_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/modules/subject"
	contracts "github.com/septagon-oss/platformkit/modules/subject/contracts"
)

// foreignService satisfies the module's own Service interface by inheriting it, so a
// composition can hand in a service this package did not build.
type foreignService struct{ contracts.Service }

// TestNewWithCheckedNamesTheRefusalAndNewPanicsWithTheSameSentence: the export bundle is one entry per
// module, so a part nobody owns and two parts claiming one name are both a person's
// words that would go missing from the copy they are owed.
func TestNewWithCheckedNamesTheRefusalAndNewPanicsWithTheSameSentence(t *testing.T) {
	rows := []struct {
		name string
		deps subject.Deps
		want string
	}{
		{"a service the module did not build", subject.Deps{Service: foreignService{}}, "a foreign one has no queue to walk"},
		{"a part with no module to attribute it to", subject.Deps{Parts: []personaldata.Part{{}}}, "a personal-data part with no module name"},
		{"two parts under one module name", subject.Deps{Parts: []personaldata.Part{{Module: "task"}, {Module: "task"}}}, "two personal-data parts named task"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			_, err := subject.NewWithChecked(r.deps)
			if err == nil {
				t.Fatal("NewWithChecked built an erasure that loses a part")
			}
			if !strings.Contains(err.Error(), r.want) {
				t.Errorf("refusal = %q, want it to contain %q", err, r.want)
			}
			var got any
			func() {
				defer func() { got = recover() }()
				subject.NewWith(r.deps)
			}()
			if s, ok := got.(string); !ok || s != err.Error() {
				t.Errorf("panicked with %#v, want the string the checked form answered %q", got, err)
			}
		})
	}
}

// TestNewWithCheckedBuildsFromItsRequiredDependenciesAlone: with nothing composed
// the doors still exist, and the parts are the contributions the plan collected
// rather than anything this module could invent.
func TestNewWithCheckedBuildsFromItsRequiredDependenciesAlone(t *testing.T) {
	m, err := subject.NewWithChecked(subject.Deps{})
	if err != nil {
		t.Fatalf("NewWithChecked with an empty Deps: %v", err)
	}
	if m.Name != "subject" {
		t.Errorf("manifest name = %q, want subject", m.Name)
	}
	if m, err = subject.NewWithChecked(subject.Deps{
		Parts: []personaldata.Part{{Module: "task"}, {Module: "file"}},
	}); err != nil {
		t.Fatalf("NewWithChecked with two attributed parts: %v", err)
	}
	if m.Name != "subject" {
		t.Errorf("manifest name = %q, want subject", m.Name)
	}
}
