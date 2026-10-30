// Package events writes events to a transactional outbox and delivers them at
// least once. Tenant events commit with tenant state; delivery claims and handler
// work commit together, so failed work remains retryable. Periodic scheduling is
// owned by kit/jobs; see README.md for delivery limits. The [Publish] example
// declares an event, commits it in a tenant transaction and relays it in memory.
package events
