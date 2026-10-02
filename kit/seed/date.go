package seed

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ResolveDate resolves the seed format's relative date expressions against one
// injected UTC instant. The caller chooses whether the owning field is a date
// or timestamp; the seeder never guesses from a field name.
func ResolveDate(now time.Time, expression string, dateOnly bool) (time.Time, error) {
	now = now.UTC()
	if dateOnly {
		now = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	}
	if len(expression) >= 3 && (expression[0] == '+' || expression[0] == '-') {
		unit := expression[len(expression)-1]
		amount, err := strconv.Atoi(expression[1 : len(expression)-1])
		if err != nil || amount < 0 {
			return time.Time{}, fmt.Errorf("seed: invalid relative date %q", expression)
		}
		if expression[0] == '-' {
			amount = -amount
		}
		switch unit {
		case 'd':
			return now.AddDate(0, 0, amount), nil
		case 'y':
			year := now.Year() + amount
			last := time.Date(year, now.Month()+1, 0, now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), time.UTC).Day()
			return time.Date(year, now.Month(), min(now.Day(), last), now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), time.UTC), nil
		}
	}
	if dateOnly {
		return time.Time{}, fmt.Errorf("seed: a date field cannot use weekday time %q", expression)
	}
	day, clock, ok := strings.Cut(expression, " ")
	if !ok {
		return time.Time{}, fmt.Errorf("seed: invalid relative date %q", expression)
	}
	weekdays := map[string]time.Weekday{
		"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday,
		"wednesday": time.Wednesday, "thursday": time.Thursday, "friday": time.Friday, "saturday": time.Saturday,
	}
	wanted, ok := weekdays[day]
	if !ok || len(clock) != 5 || clock[2] != ':' {
		return time.Time{}, fmt.Errorf("seed: invalid relative date %q", expression)
	}
	// Two digits each, and no sign. A leading minus parses as an int, and
	// time.Date would then have quietly moved the answer into the previous day —
	// a file that wrote monday -1:00 would have got Sunday 23:00 and no word
	// about it. A clock part that is not two digits is a typo, and a typo is
	// refused rather than normalised.
	if !twoDigits(clock[:2]) || !twoDigits(clock[3:]) {
		return time.Time{}, fmt.Errorf("seed: invalid clock %q in relative date %q", clock, expression)
	}
	hour, _ := strconv.Atoi(clock[:2])
	minute, _ := strconv.Atoi(clock[3:])
	if hour > 23 || minute > 59 {
		return time.Time{}, fmt.Errorf("seed: invalid clock %q in relative date %q", clock, expression)
	}
	days := (int(wanted) - int(now.Weekday()) + 7) % 7
	candidate := time.Date(now.Year(), now.Month(), now.Day()+days, hour, minute, 0, 0, time.UTC)
	if candidate.Before(now) {
		candidate = candidate.AddDate(0, 0, 7)
	}
	return candidate, nil
}

// twoDigits is a clock part as the grammar writes it: exactly two decimal
// digits, no sign.
func twoDigits(s string) bool {
	return len(s) == 2 && s[0] >= '0' && s[0] <= '9' && s[1] >= '0' && s[1] <= '9'
}
