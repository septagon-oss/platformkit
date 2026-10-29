package internal

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/tours/contracts"
)

var errTerminal = errors.New("tour run is terminal")

// transition is the single decision function used after SQL locking and by the fake.
func transition(run *contracts.Run, action contracts.Action, steps []string) error {
	if action.ExpectedRevision != run.Revision {
		return fmt.Errorf("%w: expected revision %d, found %d", crud.ErrConflict, action.ExpectedRevision, run.Revision)
	}
	if action.At.IsZero() || action.At.Location() != time.UTC {
		return fmt.Errorf("%w: action time must be UTC", crud.ErrInvalid)
	}
	if run.Status == "completed" || run.Status == "dismissed" || run.Status == "exited" {
		return errTerminal
	}
	switch action.Kind {
	case "viewed":
		if run.Status != "reserved" || action.Step != run.CurrentStep {
			return crud.ErrConflict
		}
		run.Status = "active"
		run.ViewedAt = &action.At
	case "next":
		if run.Status != "active" || action.Step != run.CurrentStep {
			return crud.ErrConflict
		}
		appendUnique(&run.Done, action.Step)
		advance(run, steps, action.At)
	case "back":
		if run.Status != "active" {
			return crud.ErrConflict
		}
		if i := slices.Index(steps, run.CurrentStep); i > 0 {
			run.CurrentStep = steps[i-1]
		}
	case "later":
		if run.Status != "active" {
			return crud.ErrConflict
		}
		due := action.At.Add(24 * time.Hour)
		run.Status, run.LaterUntil = "later", &due
	case "resume":
		if run.Status == "paused" {
			run.Status = "active"
			break
		}
		if run.Status != "later" || run.LaterUntil == nil || action.At.Before(*run.LaterUntil) {
			return crud.ErrConflict
		}
		run.Status, run.LaterUntil = "active", nil
	case "pause":
		if run.Status != "active" {
			return crud.ErrConflict
		}
		run.Status = "paused"
	case "exit":
		run.Status = "exited"
	case "dismiss":
		run.Status = "dismissed"
		run.DismissedAt = &action.At
	case "unavailable":
		if run.Status != "active" || action.Step != run.CurrentStep {
			return crud.ErrConflict
		}
	default:
		return fmt.Errorf("%w: unknown tour action %q", crud.ErrInvalid, action.Kind)
	}
	run.Revision++
	run.UpdatedAt = action.At
	return nil
}

func appendUnique(values *[]string, value string) {
	if !slices.Contains(*values, value) {
		*values = append(*values, value)
	}
}

func advance(run *contracts.Run, steps []string, at time.Time) {
	for i, id := range steps {
		if id != run.CurrentStep {
			continue
		}
		if i+1 == len(steps) {
			run.Status, run.CompletedAt = "completed", &at
			return
		}
		run.CurrentStep = steps[i+1]
		return
	}
}
