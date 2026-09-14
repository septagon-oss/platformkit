// Package delivery owns the retry ladder and retention shared by event adapters.
// It is internal so applications cannot choose incompatible broker/outbox policy.
package delivery

import "time"

// MaxDeliveries bounds handler attempts, not terminal-record persistence.
// Once exhausted, transports retry recording failure until it commits or their
// context ends. The broker retains that recovery work across restarts.
const MaxDeliveries = 5

// Backoff bounds retry frequency. Package tests shorten the waits without
// changing the ladder's shape; production code treats it as immutable.
var Backoff = []time.Duration{time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second}

// Keep preserves one week of broker delivery and published outbox history.
// Unpublished rows, terminal failures and their claims have separate recovery rules.
const Keep = 7 * 24 * time.Hour
