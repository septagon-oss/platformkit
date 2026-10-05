package rest

// display_test.go calls the row-name gate directly, for the same reason
// presentation_gate_test.go exists: a mount on an API with no router behind it
// panics for its own reasons, so a bare recover() there is satisfied by the
// wrong accident. What is under test is the sentence and the field it names —
// the key the entity's author has to find in the struct.

import (
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
)

// Named marks the field a row is called by; Comment is a second text column the
// row could be called by instead, which is why one mark has to mean one thing.
type Named struct {
	crud.Base
	Email   string `json:"email" ui:"display"`
	Comment string `json:"comment" gorm:"type:text"`
}

func (Named) TableName() string { return "rest_named" }

// TwoNames marks both of its string fields.
type TwoNames struct {
	crud.Base
	Email string `json:"email" ui:"display"`
	Alias string `json:"alias" ui:"display"`
}

func (TwoNames) TableName() string { return "rest_two_names" }

// TimedName asks a timestamp to title a record.
type TimedName struct {
	crud.Base
	CreatedAt time.Time `json:"createdAt" ui:"display"`
}

func (TimedName) TableName() string { return "rest_timed_names" }

func TestOneMarkedStringFieldIsNotAFault(t *testing.T) {
	t.Parallel()
	if got := displayFieldFault(crud.Fields[*Named]()); got != "" {
		t.Errorf("displayFieldFault = %q, want no fault for one marked string field", got)
	}
}

// TestATextColumnMayNameARow: the reference app's users are a `text` table, and
// a name does not become unreadable because the column is `text` rather than
// `varchar`. Refusing it would make the mark the only way to be called by name.
func TestATextColumnMayNameARow(t *testing.T) {
	t.Parallel()
	fields := []crud.Field{{Name: "displayName", Type: entity.TypeText, Display: true}}
	if got := displayFieldFault(fields); got != "" {
		t.Errorf("displayFieldFault = %q, want no fault for a marked text column", got)
	}
}

// TestNoMarkIsNoFault: an untagged entity is today's entity, and this delivery
// must not make every module in every client rewrite its structs.
func TestNoMarkIsNoFault(t *testing.T) {
	t.Parallel()
	if got := displayFieldFault(crud.Fields[*TwoNames]()); got == "" {
		t.Fatal("test fixture lost its two marks")
	}
	if got := displayFieldFault(crud.Fields[*TimedName]()); got == "" {
		t.Fatal("test fixture lost its mark")
	}
	quiet := []crud.Field{
		{Name: "id", Type: entity.TypeUUID, ReadOnly: true},
		{Name: "title", Type: entity.TypeString},
	}
	if got := displayFieldFault(quiet); got != "" {
		t.Errorf("displayFieldFault = %q for an entity that marks nothing, want \"\"", got)
	}
}

// TestASecondMarkIsRefusedByName — one row has one name, and the order two
// marks happen to be declared in is not a decision anybody made.
func TestASecondMarkIsRefusedByName(t *testing.T) {
	got := displayFieldFault(crud.Fields[*TwoNames]())
	if !strings.Contains(got, `"alias"`) || !strings.Contains(got, "second display field") {
		t.Errorf("displayFieldFault = %q, which does not name the field that added the second mark", got)
	}
}

// TestAMarkOnAFieldNoHeadingCanHoldIsRefusedByName — a marked `time` would
// title every record with a timestamp, and the mount refuses rather than a
// screen drawing it.
func TestAMarkOnAFieldNoHeadingCanHoldIsRefusedByName(t *testing.T) {
	got := displayFieldFault(crud.Fields[*TimedName]())
	if !strings.Contains(got, `"createdAt"`) || !strings.Contains(got, "not a string") {
		t.Errorf("displayFieldFault = %q, which does not name the marked field and its type", got)
	}
}

// The pairing this gate shares with widgetFault and presentationFault — that
// Spec.check and Singleton.check each call displayFieldFault at the site where
// they already call the other two — is asserted by display_mount_gate_test.go:
// it mounts both, on an entity that marks two fields, and refuses a recovered
// panic that names anything but the second mark, which is what a mount with no
// router behind it makes a bare recover() worth. What is pinned here is the
// sentence both sites panic with.
