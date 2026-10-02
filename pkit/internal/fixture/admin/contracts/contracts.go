// Package contracts is a stand-in contract for pkit's composition fixture.
package contracts

// Console is what the after-everything admin module provides; nothing may need it.
type Console interface{ Resources() []string }
