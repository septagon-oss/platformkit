package internal

import (
	"net/http"
	"time"

	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

// The retry ladder, as a table rather than a formula, so that a reviewer can
// read the exact delay and a test can pin "the second attempt is available one
// minute after the first" instead of pinning an approximation of a curve.
//
// It belongs to this module and not to the broker: kit/events' own policy is
// five attempts and thirty seconds and then a dead letter, which is the right
// ladder for in-process handling and the wrong one for a customer's web server,
// which may be down for a weekend. Nothing here reads or copies that package.
var ladder = []time.Duration{
	time.Minute, 5 * time.Minute, 30 * time.Minute,
	2 * time.Hour, 6 * time.Hour, 12 * time.Hour,
	24 * time.Hour,
}

// delayBefore returns how long to wait before attempt+1, given that attempt just
// failed. Past the end of the table every hour is a day, which is the last rung:
// the window closes at three days, so attempts 8 to 12 are one per day.
func delayBefore(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	if attempt > len(ladder) {
		return ladder[len(ladder)-1]
	}
	return ladder[attempt-1]
}

// nextAttempt answers the two questions the outcome transaction asks: does this
// failed attempt get another, and when. Both halves of the brief's rule are
// checked, and either one stops it — the count, so a receiver that answers 500
// forever costs twelve requests, and the window, so a receiver that answers
// slowly-but-500 for a week stops being asked.
func nextAttempt(firstAttemptedAt time.Time, attempt int, now time.Time) (availableAt time.Time, retry bool) {
	if attempt >= contracts.MaxAttempts {
		return time.Time{}, false
	}
	if !now.Before(firstAttemptedAt.Add(contracts.FaultWindow)) {
		return time.Time{}, false
	}
	candidate := now.Add(delayBefore(attempt))
	// Never past the window: a row that could only be sent after the promise
	// expired would sit in the queue looking retryable.
	if !candidate.Before(firstAttemptedAt.Add(contracts.FaultWindow)) {
		return time.Time{}, false
	}
	return candidate, true
}

// retryable classifies an outcome. A transport error, a timeout, a 408, a 425, a
// 429 and any 5xx are "try again"; every other answer is one the receiver will
// keep giving, and an address that answers 404 to a POST will answer 404 for a
// week.
//
// A refusal of the address by this deployment's own policy is never retryable —
// see errAddressRefused — and that check comes first, because a refused dial is
// also a transport error and would otherwise join the ladder.
func retryable(status int, err error) bool {
	if err != nil {
		// Every failure to get an answer is worth another try — a connection
		// refused, a TLS handshake that died, a peer that hung up mid-body — and
		// exactly one is not: this deployment's own refusal of the address.
		return !AddressRefused(err)
	}
	switch {
	case status >= 200 && status < 300:
		return false
	case status == http.StatusRequestTimeout, status == http.StatusTooEarly, status == http.StatusTooManyRequests:
		return true
	case status >= 500:
		return true
	default:
		return false
	}
}

// retryReason is the sentence a log row carries when the answer was an HTTP
// status rather than a failure to connect.
func retryReason(status int) string {
	return http.StatusText(status)
}
