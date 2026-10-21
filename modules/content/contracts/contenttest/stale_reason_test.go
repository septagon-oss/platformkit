package contenttest_test

import (
	"reflect"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/modules/content/contracts"
)

// The Stale case is declined in writing, and a written reason is the one claim
// about a port no check reads: the harness can require a Skip to have a sentence,
// not a true one. The reason this suite gives now rests on facts a test can put to
// the kernel, so they are put to it here. This is the same watch tasktest keeps,
// phrased for this port's own entity.
//
// Which facts, and why these and not the one asked first. The question used to be
// put to the row: content carried nothing that read like a revision, and that was
// decisive. Content now carries its own write count — contracts.Content.Revision,
// which every write of it moves and which numbers the versions modules/history
// keeps — and a row's own count is not a caller's quote: it says what the row is
// on, never what the caller believes. So the question moved from the row to the
// port, exactly as it did for a task row that grew the same count: does any command
// of contracts.Service take a value a caller could send a revision in, and does
// kit/crud.Update compare one? The refusal a stale quote earns is kit/rest's
// stale-tag rule over the form that quoted the number it read, proved there and in
// modules/history — a mount above this port, not a case below it. Grow the port's
// arguments, the base every entity embeds, or kit/crud.Update, and this reddens:
// that is the moment the skip's sentence stops being true and the case the harness
// generates has to be described instead.
func TestTheStaleSkipReasonStillStatesWhatTheKernelHas(t *testing.T) {
	svc := reflect.TypeFor[contracts.Service]()
	if svc.Kind() != reflect.Interface {
		t.Fatalf("contracts.Service is %s, not the interface whose commands the skip speaks for", svc.Kind())
	}
	for i := 0; i < svc.NumMethod(); i++ {
		method := svc.Method(i)
		for arg := 1; arg < method.Type.NumIn(); arg++ { // 0 is the receiver
			if k := method.Type.In(arg).Kind(); k >= reflect.Int && k <= reflect.Uint64 {
				t.Errorf("contracts.Service.%s takes %s as argument %d, which a caller could use as the "+
					"revision a row is compared against. The Stale skip says no command of this port is "+
					"given one: describe the case the harness generates and retire the skip",
					method.Name, method.Type.In(arg), arg)
			}
		}
	}
	// The base is where a revision would arrive for every entity at once, so the
	// row is still asked, of that: an entity that inherits a number a caller quotes
	// back has no room left to decline the case.
	if f, ok := porttest.RevisionField(reflect.TypeFor[crud.Base]()); ok {
		t.Errorf("crud.Base carries %s, which every entity would inherit as a revision a caller quotes back. The Stale skip says nothing below this port compares one: describe that case and retire the skip", f.Name)
	}
	write := reflect.TypeOf(crud.Update[*contracts.Content])
	for i := 0; i < write.NumIn(); i++ {
		if k := write.In(i).Kind(); k >= reflect.Int && k <= reflect.Uint64 {
			t.Errorf("crud.Update takes %s as argument %d, which a caller could use as the revision a row is compared against. The Stale skip says nothing below this port compares one", write.In(i), i)
		}
	}
	if !write.IsVariadic() {
		t.Errorf("crud.Update no longer takes the columns to write as its trailing argument, so what the Stale skip says it takes is out of date")
	}
}
