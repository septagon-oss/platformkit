package tasktest_test

import (
	"reflect"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
)

// The Stale case is declined in writing, and a written reason is the one claim
// about a port no check reads: review 6 found this suite's reason attributing a
// revision check to "kit/crud's own PATCH", a check kit/crud does not have, and the
// harness accepted the sentence because it can require a reason and not a true one.
// The reason this suite gives now rests on two facts a test can put to the kernel,
// so they are put to it here: nothing stored on a task reads as a revision, and
// kit/crud.Update takes no argument a caller could hand a revision to and still
// takes the columns to write as its trailing argument. Grow either and this reddens
// — which is the moment the skip's sentence stops being true and the case the
// harness generates has to be described instead. Testing a whole sentence of prose
// is out of anyone's reach; this is the part of it that is not, and it is what a
// false citation would have met had it existed when review 6 looked.
func TestTheStaleSkipReasonStillStatesWhatTheKernelHas(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeFor[contracts.Task](), reflect.TypeFor[crud.Base]()} {
		if f, ok := porttest.RevisionField(typ); ok {
			t.Errorf("%s carries %s, which reads like a revision a caller quotes back. The Stale skip says a task carries none and declines the case the harness would generate: describe that case and retire the skip", typ, f.Name)
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
