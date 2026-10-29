package rest

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// TestEachReadDoorAsksItsCallerBeforeItsStatement: the item door, the list door, the
// count and the translation overview's page are the four reads this case moves onto a
// door that asks its caller; each answers an ended caller with an error wrapping its own
// cause, never a row, ErrNotFound or ErrInvalid. The versions route asks that item door
// too. No mounted route can ask, so the transaction here carries no handle to reach for.
func TestEachReadDoorAsksItsCallerBeforeItsStatement(t *testing.T) {
	ended, cancel := context.WithCancelCause(context.Background())
	cause := errors.New("the browser hung up")
	cancel(cause)
	s := Spec[*derivedOnly]{Module: "reads", Entity: "derived_only", Path: "/derived-only"}
	overview := &specSource[*derivedOnly]{spec: s}
	var tx db.Tx[db.Tenant]
	doors := map[string]func() error{
		"item door":            func() error { _, err := s.get(ended, tx, uuid.New()); return err },
		"list door":            func() error { _, _, err := s.list(ended, tx, crud.Query{}); return err },
		"count":                func() error { _, err := s.count(ended, tx); return err },
		"translation overview": func() error { _, _, err := overview.Page(ended, tx, 10, 0); return err },
	}
	for name, door := range doors {
		err := func() (err error) {
			defer func() {
				if p := recover(); p != nil {
					err = fmt.Errorf("the %s reached the database: %v", name, p)
				}
			}()
			return door()
		}()
		if err == nil || !errors.Is(err, cause) || errors.Is(err, crud.ErrNotFound) || errors.Is(err, crud.ErrInvalid) {
			t.Errorf("the %s for an ended caller = %v, want an error wrapping its cause and nothing else", name, err)
		}
	}
}
