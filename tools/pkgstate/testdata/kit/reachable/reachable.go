// Package reachable holds variables whose contents change with no assignment
// to the variable itself.
package reachable

var Names []string       // a slice: a caller appends to what it reads
var Index map[string]int // a map: a caller writes into what it reads
var Store *State         // a pointer: a caller sets a field through what it reads
var Notices chan string  // a channel: a caller sends on what it reads
var Rows Table           // reaches through the name it was declared with
var Default = Shape{10}  // written only where it is declared
var Count = 7            // a plain value, written only where it is declared

type Table []int // what Rows is declared as

type (
	Shape struct{ Width int } // nothing reaches through it
	State struct{ Open bool } // what Store points at
)
