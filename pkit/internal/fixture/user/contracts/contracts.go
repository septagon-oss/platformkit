// Package contracts is a stand-in contract for pkit's composition fixture.
package contracts

// Service is the user capability the fixture's modules need.
type Service interface{ Name(id string) string }
