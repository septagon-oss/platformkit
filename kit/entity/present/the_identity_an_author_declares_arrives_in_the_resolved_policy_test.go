package present_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
)

// TestTheIdentityAnAuthorDeclaresArrivesInTheResolvedPolicy asks that Resolve
// carries every half of an entry's identity onto the policy a shell reads: the two
// words, the two descriptions, the icon name, the grouping key with its label, and
// the position within the group.
//
// The seven ride together because they are one hand-written struct literal, and
// each half an entry gains is one more line somebody has to remember. The served
// document carries the same declaration twice — raw as `presentation`, decided as
// `resolved` — so a dropped copy is two fields in one document that no longer
// agree, and nothing on the wire says which is the mistake: a shell that groups
// from resolved.identity.group lists the resource with everything else while the
// module believes it named a place, and a dropped position reads as 0, which is
// also what "no opinion" means. Each half is asked with its converse, so the case
// cannot pass by ignoring the declaration on one hand or inventing one on the other.
func TestTheIdentityAnAuthorDeclaresArrivesInTheResolvedPolicy(t *testing.T) {
	t.Parallel()

	schema := entity.Schema{Module: "m", Entity: "user", Fields: []entity.Field{
		{Name: "id", Type: entity.TypeUUID, ReadOnly: true},
		{Name: "title", Type: entity.TypeString},
	}}
	// The two words are asked as the pair a module really writes (modules/user says
	// "person"/"people") rather than as what the entity name would derive, so a copy
	// that drops either one cannot hide behind the derivation.
	full := entity.EntryHints{
		Singular: "person", Plural: "people", Description: "What the firm wrote down",
		EmptyDescription: "Nothing has been written down yet.", Icon: "document",
		Group: &entity.ResourceGroup{Key: "practice", Label: "Practice"}, Order: 7,
	}
	declared := present.Resolve(present.Input{Schema: schema, Hints: full}).Identity

	if declared.Singular != full.Singular {
		t.Errorf("an entry whose author declared singular %q resolves %q, want the declared word", full.Singular, declared.Singular)
	}
	if declared.Plural != full.Plural {
		t.Errorf("an entry whose author declared plural %q resolves %q, want the declared word", full.Plural, declared.Plural)
	}
	if declared.Description != full.Description {
		t.Errorf("an entry whose author declared description %q resolves %q, want the declared sentence, which no other field of the policy carries", full.Description, declared.Description)
	}
	if declared.EmptyDescription != full.EmptyDescription {
		t.Errorf("an entry whose author declared %q for an empty list resolves %q, want the declared sentence", full.EmptyDescription, declared.EmptyDescription)
	}
	if declared.Icon != full.Icon {
		t.Errorf("an entry whose author declared icon %q resolves %q, want the declared name", full.Icon, declared.Icon)
	}
	if declared.Group == nil {
		t.Error("an entry whose author declared a group resolves no group, want the declared key and label")
	} else if declared.Group.Key != full.Group.Key || declared.Group.Label != full.Group.Label {
		t.Errorf("an entry whose author declared group %q/%q resolves %q/%q, want both halves, a shell groups by the key and prints the label",
			full.Group.Key, full.Group.Label, declared.Group.Key, declared.Group.Label)
	}
	if declared.Order != full.Order {
		t.Errorf("an entry whose author declared position %d resolves order %d, want the declared position", full.Order, declared.Order)
	}

	// The converse: with nothing declared, resolution derives the two words and
	// writes no icon, no group and no position, so a default cannot masquerade as
	// a decision a module made.
	unhinted := present.Resolve(present.Input{Schema: schema}).Identity
	if unhinted.Singular != "user" || unhinted.Plural != "users" {
		t.Errorf("an entry that declared no words resolves %q/%q, want the derived %q/%q",
			unhinted.Singular, unhinted.Plural, "user", "users")
	}
	if unhinted.Icon != "" || unhinted.Group != nil || unhinted.Order != 0 {
		t.Errorf("an entry that declared nothing resolves icon %q, group %#v, order %d, want none of the three",
			unhinted.Icon, unhinted.Group, unhinted.Order)
	}
}
