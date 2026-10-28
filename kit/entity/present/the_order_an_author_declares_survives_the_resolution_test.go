package present_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
)

// TestTheOrderAnAuthorDeclaresSurvivesTheResolution asks that Resolve keeps the
// position a resource's author declared.
//
// Resolve copies the position a resource's author declared within its own
// grouping onto the resolved identity, and the catalogue writes that field for a
// shell that groups: ui/screens publishes the whole resolution as
// "resolved.identity.order". Each sentence is asked with its converse, so the
// rule cannot hold by ignoring the declaration on one hand or by inventing a
// position on the other — a dropped copy reaches the shell as 0, which is what
// "no opinion" already means, so every resource lands in registration order
// with nobody able to tell the two apart.
func TestTheOrderAnAuthorDeclaresSurvivesTheResolution(t *testing.T) {
	t.Parallel()

	schema := entity.Schema{Module: "m", Entity: "note", Fields: []entity.Field{
		{Name: "id", Type: entity.TypeUUID, ReadOnly: true},
		{Name: "title", Type: entity.TypeString},
	}}
	declared := present.Resolve(present.Input{Schema: schema,
		Hints: entity.EntryHints{Group: &entity.ResourceGroup{Key: "work", Label: "Work"}, Order: 7}})
	if got := declared.Identity.Order; got != 7 {
		t.Errorf("an entry whose author declared position 7 resolves order %d, want the declared position", got)
	}

	quiet := present.Resolve(present.Input{Schema: schema,
		Hints: entity.EntryHints{Group: &entity.ResourceGroup{Key: "work", Label: "Work"}}})
	if got := quiet.Identity.Order; got != 0 {
		t.Errorf("an entry that declared no position resolves order %d, want 0, which is what \"no opinion\" means", got)
	}
}
