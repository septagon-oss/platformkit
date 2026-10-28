package present_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
)

// TestAResolvedPolicyNamesTheTagTheCallerTranslatedFor pins the one promise every
// reader of Resolved.Language leans on: the resolved object always names a tag, so a
// caller that translated for none still gets "en" rather than nothing. The reader this
// matters to is the page contract, which answers a shell's language choice with the
// policy's tag when the caller named none of its own — an empty policy tag would be an
// empty language on the page, which no catalogue holds words for. Both resolve paths
// are asked: the whole entry and the commands-only resolution, which stops early and
// could name a language on one path and not the other.
func TestAResolvedPolicyNamesTheTagTheCallerTranslatedFor(t *testing.T) {
	t.Parallel()

	schema := entity.Schema{Module: "m", Entity: "note", Fields: []entity.Field{
		{Name: "id", Type: entity.TypeUUID, ReadOnly: true},
		{Name: "title", Type: entity.TypeString},
	}}
	commands := []present.CommandInput{{Verb: "archive", Summary: "archive it"}}

	for _, path := range []struct {
		what         string
		commandsOnly bool
	}{
		{"a whole entry", false},
		{"a commands-only resolution", true},
	} {
		for _, c := range []struct{ asked, want string }{
			{"", "en"},
			{"pt-PT", "pt-PT"},
		} {
			got := present.Resolve(present.Input{Schema: schema, Commands: commands,
				CommandsOnly: path.commandsOnly, Language: c.asked})
			if got.Language != c.want {
				t.Errorf("%s resolved for %q names language %q, want %q", path.what, c.asked, got.Language, c.want)
			}
		}
	}
}
