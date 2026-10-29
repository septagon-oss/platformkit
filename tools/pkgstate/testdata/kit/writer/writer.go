// Package writer reaches into another package of the same tree.
package writer

import "fixture/kit/counted"

// Remember keeps what it is given.
func Remember(name string) {
	counted.Items = append(counted.Items, name)
}
