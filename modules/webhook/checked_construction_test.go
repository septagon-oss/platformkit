package webhook_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/webhook"
)

// catalogue names the events an endpoint may be told about. Its list is never read:
// the refusal below is that this module may not invent one.
type catalogue struct{}

func (catalogue) EventTypes() []string { return []string{"task.created"} }

// TestNewCheckedNamesTheRefusalAndNewPanicsWithTheSameSentence: the key signs and seals every attempt
// and the catalogue is what says an endpoint may ask for an event at all.
func TestNewCheckedNamesTheRefusalAndNewPanicsWithTheSameSentence(t *testing.T) {
	rows := []struct {
		name string
		deps webhook.Deps
		want string
	}{
		{"a secret read as something shorter", webhook.Deps{SecretKey: []byte("three-bytes")}, "webhook: the secret key is 11 bytes"},
		{"a key of the right width and no event list", webhook.Deps{SecretKey: make([]byte, 32)}, "webhook: a Catalogue is required"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			_, err := webhook.NewChecked(r.deps)
			if err == nil {
				t.Fatal("NewChecked built a delivery queue out of nothing")
			}
			if !strings.Contains(err.Error(), r.want) {
				t.Errorf("refusal = %q, want it to contain %q", err, r.want)
			}
			var got any
			func() {
				defer func() { got = recover() }()
				webhook.New(r.deps)
			}()
			if s, ok := got.(string); !ok || s != err.Error() {
				t.Errorf("panicked with %#v, want the string the checked form answered %q", got, err)
			}
		})
	}
}

// TestNewCheckedBuildsFromItsRequiredDependenciesAlone: the key and the list, and
// the schedules, the retention and the alerts are the deployment's to leave unset.
func TestNewCheckedBuildsFromItsRequiredDependenciesAlone(t *testing.T) {
	m, err := webhook.NewChecked(webhook.Deps{SecretKey: make([]byte, 32), Catalogue: catalogue{}})
	if err != nil {
		t.Fatalf("NewChecked with a key and a catalogue: %v", err)
	}
	if m.Name != "webhook" {
		t.Errorf("manifest name = %q, want webhook", m.Name)
	}
	if len(m.Jobs) == 0 {
		t.Error("the delivery and retention sweeps are both missing from the manifest")
	}
}
