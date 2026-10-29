// Package sentinels holds the values a caller compares rather than updates.
package sentinels

import (
	"errors"
	"fmt"
)

var ErrMissing = errors.New("missing") // the classic sentinel
var ErrLate = fmt.Errorf("late")       // built with a message
var ErrRefused = refusalError{}        // answers through a type with Error
var Flag = true                        // a plain value written nowhere

// Regions is a table the composition reads and never edits.
// Immutable: read at startup, no writer exists in this tree.
var Regions = []string{"eu", "us"}

// Names is the same table, marked on the line beside it.
var Names = []string{"a", "b"} // Immutable: a table, never a counter.

type refusalError struct{} // an error type of this package

func (refusalError) Error() string { return "refused" }
