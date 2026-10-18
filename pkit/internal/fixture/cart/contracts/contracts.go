// Package contracts is a stand-in contract for pkit's composition fixture.
package contracts

// Service is the cart capability the fixture's modules need.
type Service interface{ Total() int64 }

// Extension is one thing a module adds to the cart; the cart takes every one.
type Extension struct{ Module string }
