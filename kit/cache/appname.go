package cache

// appname.go holds the one spelling of a shared name.
//
// CacheKey is appname.CacheKey and Slug is the grammar appname.Parse accepts, in
// the shape decision 0074 rule 7 names for "cache keys": every shared name this
// kernel writes — a cache key, a metric name, a stream subject — starts with the
// application that wrote it, because a fleet that shares a Valkey has one
// keyspace and two clients named "acme". T-0228 carries the same two
// declarations in kit/appname; this package carries them because kit/httpx needs
// a shared cache before that package lands.
//
// Adoption is a deletion, not a rewrite: parse with appname.Parse, build the key
// with appname.CacheKey, and TestTheKeySpellingIsOneSpelling must not notice —
// that case pins the bytes of a formed key, so a merge that changed a separator,
// dropped the app segment or shortened the uuid would be found there rather than
// as every entry written before it orphaned.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// separator is what T-0228 chose: a colon, which no host, no id and no
// namespace this kernel writes begins with.
const separator = ":"

// maxName is the length limit of an application name, as T-0228 sets it.
const maxName = 32

// grammar is the sentence Slug refuses with. It names the rule and an example
// and never quotes the value that arrived, because the value that arrives is
// often a client's own name and a refused configuration is read in a log by
// whoever comes next (T-0228's rule for error text).
const grammar = `an application name is lower-case letters, digits and single dashes, must begin and end with a letter or a digit, and is at most 32 characters — "platformkit", "billing-service"`

// CacheKey is the whole address of one entry: <app>:<tenant>:<name>.
//
// It takes no shortcut with the tenant: a uuid is thirty-six fixed characters,
// which is what makes a name appended after it unable to shift an earlier
// segment of the key. An empty app yields the empty first segment — the spelling
// of "this process named no application", which New refuses but this function,
// the one place the spelling lives, does not.
func CacheKey(app string, tenant uuid.UUID, name string) string {
	return strings.Join([]string{app, tenant.String(), name}, separator)
}

// Slug validates an application name against the grammar and returns it.
func Slug(app string) (string, error) {
	if app == "" {
		return "", fmt.Errorf("cache: this process names no application: every shared key is %s", grammar)
	}
	if len(app) > maxName {
		return "", fmt.Errorf("cache: an application name is at most %d characters: %s", maxName, grammar)
	}
	for i, r := range app {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-':
			if i == 0 || i == len(app)-1 || app[i-1] == '-' {
				return "", errors.New("cache: an application name may not begin, end or double up on a dash: " + grammar)
			}
		default:
			return "", fmt.Errorf("cache: an application name holds %q, which is not in it: %s", r, grammar)
		}
	}
	return app, nil
}
