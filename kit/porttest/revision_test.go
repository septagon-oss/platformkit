package porttest

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

// RevisionField is what a suite watches its own Stale decline against, so the
// reading has to answer the question rather than wave at anything: a revision a
// caller quotes back, whether it sits on the entity or on the base it embeds, and
// nothing else. A field that merely holds one of those words inside it is not one —
// a reading that flagged it would teach a suite to ignore the result, which is how a
// watch stops being one.

type revBase struct {
	ID        uuid.UUID
	UpdatedAt time.Time
}

type revEntity struct {
	revBase
	Title            string
	ColumnVersioning bool
	Conversion       float64
}

type revNamed struct {
	revBase
	Revision int64
}

type revQuoted struct {
	revBase
	ETag string
}

// revRowVersion is the revision one level down, on the embedded base rather than on
// the entity — which is where a kernel that grew the column would put it.
type revRowVersion struct {
	revStamp
	Title string
}

type revStamp struct {
	ID         uuid.UUID
	RowVersion int64
}

func TestRevisionFieldNamesOnlyARevision(t *testing.T) {
	for _, want := range []struct {
		typ   reflect.Type
		field string
	}{
		{reflect.TypeFor[revNamed](), "Revision"},
		{reflect.TypeFor[revQuoted](), "ETag"},
		{reflect.TypeFor[revRowVersion](), "RowVersion"},
	} {
		f, ok := RevisionField(want.typ)
		if !ok || f.Name != want.field {
			t.Errorf("RevisionField(%s) = %q, %v; want the %s", want.typ, f.Name, ok, want.field)
		}
	}
	for _, none := range []reflect.Type{reflect.TypeFor[revEntity](), reflect.TypeFor[revBase](), reflect.TypeFor[uuid.UUID]()} {
		if f, ok := RevisionField(none); ok {
			t.Errorf("RevisionField(%s) found %s; nothing there is a revision a caller quotes back", none, f.Name)
		}
	}
}
