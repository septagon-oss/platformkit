package rest_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

// A patch body is read field by field where a drawing is declared, so every ordinary
// field of that body reaches the entity through the same token the body wrote. A bare
// number beyond what a float64 carries exactly must therefore still arrive as the
// digits it is, and a number that is no integer at all must still be refused rather
// than rounded: the door that keeps tokens is the door that must refuse what a token
// cannot name.

func TestAPatchBareNumberKeepsTheDigitsItSent(t *testing.T) {
	for _, fake := range []bool{false, true} {
		t.Run(fmt.Sprintf("fake=%t", fake), func(t *testing.T) {
			w := precisionFixture(t, fake)
			created := w.request("POST", precisionPath, `{"title":"bare","amountCents":9007199254740993,"currency":"EUR","priority":7,"control":11}`, "", host)
			precisionStatus(t, created, 201)
			rowID := uuid.MustParse(id(t, created.Body.String()))

			changed := w.request("PATCH", precisionPath+"/"+rowID.String(), `{"amountCents":9007199254740995}`, "", host)
			precisionStatus(t, changed, 200)
			assertPrecisionBody(t, changed.Body.Bytes(), "9007199254740995")
			if row := w.read(rowID); row.AmountCents != 9007199254740995 {
				t.Fatalf("stored amount = %d, want the digits 9007199254740995 the body wrote", row.AmountCents)
			}
		})
	}
}

func TestAPatchBareNumberThatNamesNoInt64IsRefusedWithoutAMove(t *testing.T) {
	for _, fake := range []bool{false, true} {
		t.Run(fmt.Sprintf("fake=%t", fake), func(t *testing.T) {
			w := precisionFixture(t, fake)
			created := w.request("POST", precisionPath, `{"title":"bare","amountCents":9007199254740993,"currency":"EUR","priority":7,"control":11}`, "", host)
			precisionStatus(t, created, 201)
			rowID := uuid.MustParse(id(t, created.Body.String()))
			path := precisionPath + "/" + rowID.String()
			snapshot := func() string { data, _ := json.Marshal(w.read(rowID)); return string(data) }
			before := snapshot()
			events := len(w.events())

			for _, body := range []string{
				`{"amountCents":1.5}`, `{"amountCents":9007199254740993.5}`,
				`{"amountCents":9223372036854775808}`, `{"amountCents":-9223372036854775809}`,
			} {
				got := w.request("PATCH", path, body, "", host)
				if got.Code != 422 {
					t.Fatalf("PATCH %s = %d %s, want 422", body, got.Code, got.Body.String())
				}
			}

			if after := snapshot(); after != before {
				t.Fatalf("row moved on a refused patch: before %s, after %s", before, after)
			}
			if got := len(w.events()); got != events {
				t.Fatalf("outbox holds %d event(s) after %d refusals, want the %d already there", got, 4, events)
			}
		})
	}
}
