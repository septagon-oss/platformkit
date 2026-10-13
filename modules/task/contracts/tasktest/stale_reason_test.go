package tasktest_test

import (
	"reflect"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
)

// The Stale case is declined in writing, and a written reason is the one claim
// about a port no check reads: review 6 found this suite's reason attributing a
// revision check to "kit/crud's own PATCH", a check kit/crud does not have, and the
// harness accepted the sentence because it can require a reason and not a true one.
// The reason this suite gives now rests on facts a test can put to the kernel, so
// they are put to it here.
//
// Which facts, and why these and not the ones asked first. The row used to carry
// nothing that read like a revision, and that was the decisive fact; main grew
// contracts.Task.Revision to answer a different question — "somebody diffing these
// needs a number", a count of writes a proposal records so modules/change can
// refuse to apply one made against a row that has moved since. A row's own count is
// not a caller's quote: it says what the row is on, never what the caller believes.
// So the question moved from the row to the port, and the kernel is asked it now —
// whether any command of contracts.Service takes a value a caller could send a
// revision in, and whether kit/crud.Update compares one. Grow either and this
// reddens, which is the moment the skip's sentence stops being true and the case the
// harness generates has to be described instead. Testing a whole sentence of prose is
// out of anyone's reach; this is the part of it that is not, and it is what a false
// citation would have met had it existed when review 6 looked.
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
	write := reflect.TypeOf(crud.Update[*contracts.Task])
	for i := 0; i < write.NumIn(); i++ {
		if k := write.In(i).Kind(); k >= reflect.Int && k <= reflect.Uint64 {
			t.Errorf("crud.Update takes %s as argument %d, which a caller could use as the revision a row is compared against. The Stale skip says nothing below this port compares one", write.In(i), i)
		}
	}
	if !write.IsVariadic() {
		t.Errorf("crud.Update no longer takes the columns to write as its trailing argument, so what the Stale skip says it takes is out of date")
	}
}
