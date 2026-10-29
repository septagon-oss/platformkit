package screen_test

// The host views a command names are the parents its dialog appears on, and a
// client generated against a published command renders it on those parents. The
// set belongs to the command's own identity: a command publishes no anchor, and a
// moved dialog leaves the parent it left and the parent it joined each holding
// their own reference, so only the command's identity hash can refuse the move.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/screen"
)

func TestAPublishedCommandRefusesMovingToAnotherHost(t *testing.T) {
	published := deleteCommandHostedOn(t, "app/note/custom/list")
	if err := deleteCommandHostedOn(t, "app/note/custom/detail").Compatible(published); err == nil {
		t.Error("a published command kept its identity while its dialog moved to another parent view")
	}
	if err := deleteCommandHostedOn(t, "app/note/custom/list").Compatible(published); err != nil {
		t.Errorf("the same command published twice = %v, want compatible", err)
	}
}

// deleteCommandHostedOn publishes app/note/resource/note/command/delete with the
// named view as its only host, alongside both candidate parents and the three
// operations that mount them.
func deleteCommandHostedOn(t *testing.T, host string) screen.Inventory {
	t.Helper()
	inventory, err := screen.Publish("note", 1,
		[]screen.View{
			screen.MustView(screen.ViewSpec{Ref: "app/note/custom/list", Cardinality: screen.One}),
			screen.MustView(screen.ViewSpec{Ref: "app/note/custom/detail", Cardinality: screen.One}),
			screen.MustView(screen.ViewSpec{Ref: "app/note/resource/note/command/delete", Cardinality: screen.One,
				Hosts: []screen.ViewRef{screen.ViewRef(host)}}),
		},
		[]screen.Mount{
			{Owner: "note", Document: true,
				Operation: screen.Operation{ID: "note-list", Surface: "app", Method: "GET", Path: "/app/note/list", Guard: "permission note:read"},
				Responses: []screen.Response{{View: "app/note/custom/list", Kind: "input"}}},
			{Owner: "note", Document: true,
				Operation: screen.Operation{ID: "note-read", Surface: "app", Method: "GET", Path: "/app/note/detail", Guard: "permission note:read"},
				Responses: []screen.Response{{View: "app/note/custom/detail", Kind: "input"}}},
			{Owner: "note", Document: true,
				Operation: screen.Operation{ID: "note-delete", Surface: "app", Method: "POST", Path: "/app/note/delete", Guard: "permission note:delete"},
				Responses: []screen.Response{{View: "app/note/resource/note/command/delete", Kind: "result"}}},
		})
	if err != nil {
		t.Fatalf("publish the delete command hosted on %s: %v", host, err)
	}
	return inventory
}
