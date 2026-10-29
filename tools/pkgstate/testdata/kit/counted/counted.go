// Package counted holds the variables written after their declaration.
package counted

var Counter int        // written by this package itself, after it was declared
var Limit = 40         // nothing writes it, so nothing reports it
var Items []string     // written by another package of the same tree
var Hidden int         // written only where a local of the same name hides it
var clock = newClock() // arrives from a call, so its shape is not read here

func newClock() int { return 0 }

func Bump() { Counter++ } // the write the report finds

// hidden writes the local, not the package variable of the same name.
func hidden() int {
	Hidden := 1
	Hidden++
	return Hidden
}
