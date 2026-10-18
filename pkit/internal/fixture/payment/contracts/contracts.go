// Package contracts is a stand-in contract for pkit's composition fixture.
package contracts

// Provider takes a payment.
type Provider interface{ Charge(minor int64) string }
