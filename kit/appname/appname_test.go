package appname_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
)

var (
	collect = appname.MustParse("collect")
	academy = appname.MustParse("academy")
	tenantA = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	tenantB = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	fileKey = uuid.MustParse("33333333-3333-3333-3333-333333333333")
)

// TestTwoAppsNeverShareAName: the acceptance the whole package exists for. Every
// constructor takes two apps' names and produces two different strings, so one
// app can never receive, claim, lock or overwrite the other's.
func TestTwoAppsNeverShareAName(t *testing.T) {
	cases := []struct {
		what string
		a, b string
	}{
		{"subject", appname.Subject(collect, tenantA, "cart.checked_out"), appname.Subject(academy, tenantA, "cart.checked_out")},
		{"filter", appname.Filter(collect, "cart.checked_out"), appname.Filter(academy, "cart.checked_out")},
		{"durable", appname.Durable(collect, "cart", "cart.checked_out"), appname.Durable(academy, "cart", "cart.checked_out")},
		{"job lock", appname.JobLock(collect, "purge"), appname.JobLock(academy, "purge")},
		{"cookie", appname.Cookie(collect, "session", true), appname.Cookie(academy, "session", true)},
		{"plain cookie", appname.Cookie(collect, "session", false), appname.Cookie(academy, "session", false)},
		{"rate limit", appname.RateLimitKey(collect, tenantA, "writes"), appname.RateLimitKey(academy, tenantA, "writes")},
		{"cache", appname.CacheKey(collect, tenantA, "home"), appname.CacheKey(academy, tenantA, "home")},
		{"storage", appname.StoragePath(collect, tenantA, fileKey), appname.StoragePath(academy, tenantA, fileKey)},
		{"source", appname.Source(collect, "cart"), appname.Source(academy, "cart")},
		{"connection", appname.ConnectionName("platformkit-worker", collect), appname.ConnectionName("platformkit-worker", academy)},
	}
	for _, c := range cases {
		if c.a == c.b {
			t.Errorf("%s: two apps name it the same: %q", c.what, c.a)
		}
		if !strings.Contains(c.a, "collect") || !strings.Contains(c.b, "academy") {
			t.Errorf("%s: %q and %q do not carry the app they name", c.what, c.a, c.b)
		}
	}
}

// TestTheSubjectOfOneTenantIsNotReachableFromAnother: within one app the tenant
// stays the boundary the address fixes, so an app-scoped address is no looser
// than the tenant-scoped one it replaces.
func TestTheSubjectOfOneTenantIsNotReachableFromAnother(t *testing.T) {
	a := appname.Subject(collect, tenantA, "cart.checked_out")
	b := appname.Subject(collect, tenantB, "cart.checked_out")
	if a == b {
		t.Fatalf("two tenants share the address %q", a)
	}
	if got := strings.Count(a, "."); got != 4 {
		t.Errorf("%q has %d tokens after the prefix; the address is app, tenant, module, event", a, got)
	}
}

// TestAFilterMatchesOneAppsTenantsAndNoOthers: a NATS `*` matches exactly one
// token, so the app token is fixed and the tenant token is the wildcard.
func TestAFilterMatchesOneAppsTenantsAndNoOthers(t *testing.T) {
	f := appname.Filter(collect, "cart.checked_out")
	if f != "platformkit.collect.*.cart.checked_out" {
		t.Fatalf("the filter is %q", f)
	}
	for _, subject := range []string{
		appname.Subject(collect, tenantA, "cart.checked_out"),
		appname.Subject(collect, tenantB, "cart.checked_out"),
	} {
		if !matched(f, subject) {
			t.Errorf("%s names this app's tenant and %q does not match it", subject, f)
		}
	}
	for _, subject := range []string{
		appname.Subject(academy, tenantA, "cart.checked_out"),
		appname.Subject(collect, tenantA, "cart.added"),
	} {
		if matched(f, subject) {
			t.Errorf("%q matches %s, which is another app's tenant or another event", f, subject)
		}
	}
}

// TestTheWindowsAnswerEveryAddressTheRolloutPublishes: a subscription answers
// this build's address and the two older ones, newest first, and the older ones
// are what delivery's app check exists to police.
func TestTheWindowsAnswerEveryAddressTheRolloutPublishes(t *testing.T) {
	got := appname.Filters(collect, "cart.checked_out")
	want := []string{
		"platformkit.collect.*.cart.checked_out",
		"platformkit.*.cart.checked_out",
		"platformkit.cart.checked_out",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the filter set is %q, want %q", got, want)
	}
	if appname.PreviousSubject(tenantA, "cart.checked_out") != "platformkit."+tenantA.String()+".cart.checked_out" {
		t.Error("the previous address lost the tenant token it is named for")
	}
	if strings.Contains(appname.OldestSubject("cart.checked_out"), tenantA.String()) {
		t.Error("the oldest address names no tenant, and this one does")
	}
}

// TestADurableCarriesNoDot: a consumer name may not hold a dot, so the durable is
// a transliteration of the address and not the address, and the three parts are
// joined by a plus because that is the character no app slug, module name or event
// name can hold — see TestADurableNameBelongsToExactlyOneApp.
func TestADurableCarriesNoDot(t *testing.T) {
	d := appname.Durable(collect, "cart", "cart.checked_out")
	if d != "collect+cart-cart-checked_out" {
		t.Errorf("the durable is %q", d)
	}
	// The app is a prefix and nothing else: 000031 moves the handled ledger and
	// the dead letters by prefixing the stored name, because a durable row holds
	// no module and no event name to rebuild from, so the scoped name has to be
	// the unscoped one with "<app>+" in front of it. A second '+' behind the app
	// would break that and the migration with it.
	for _, name := range []struct{ module, event string }{
		{"cart", "cart.checked_out"},
		{"task", "task.updated"},
		{"file", "file.uploaded.v2"},
	} {
		unscoped, scoped := appname.Durable("", name.module, name.event), appname.Durable(collect, name.module, name.event)
		if scoped != "collect+"+unscoped {
			t.Errorf("%s/%s: %q is not the unscoped durable %q with the app prefixed",
				name.module, name.event, scoped, "collect+"+unscoped)
		}
	}
	if strings.ContainsAny(d, ".>*/ ") {
		t.Errorf("%q holds a character a JetStream consumer name refuses", d)
	}
	if appname.Durable(collect, "cart", "cart.checked_out") == appname.Durable(collect, "cartx", "cart.checked_out") {
		t.Error("two modules' subscriptions share one durable, so one acknowledges for the other")
	}
}

// TestCookiesAreReadUnderBothNames: a jar from before the app prefix still spends
// its session; the new name is the one a response sets.
func TestCookiesAreReadUnderBothNames(t *testing.T) {
	if got := appname.Cookie(collect, "session", true); got != "__Host-collect-session" {
		t.Errorf("the secure cookie is %q", got)
	}
	if got := appname.Cookie(collect, "session", false); got != "collect-session" {
		t.Errorf("the plain cookie is %q, and two apps on one localhost port share __Host-less names", got)
	}
	for _, legacy := range appname.PreviousCookies("session") {
		if legacy == appname.Cookie(collect, "session", true) || legacy == appname.Cookie(collect, "session", false) {
			t.Errorf("%q is both the name still read and the name written", legacy)
		}
	}
}

// TestAnIdentityKeyCannotForgeItsPrefix: both identifiers are fixed-length and sit
// in front of the caller's own text, so a key that starts with another app's slug
// still counts in its own bucket.
func TestAnIdentityKeyCannotForgeItsPrefix(t *testing.T) {
	honest := appname.RateLimitKey(collect, tenantA, "writes")
	spoof := appname.RateLimitKey(academy, tenantA, "collect/writes")
	if honest == spoof {
		t.Errorf("a caller's key reached another app's bucket: %q", honest)
	}
	cache := appname.CacheKey(collect, tenantA, "home")
	if !strings.HasPrefix(cache, "collect:"+tenantA.String()+":") {
		t.Errorf("the cache key %q does not name both owners in its first two segments", cache)
	}
	if strings.Contains(strings.TrimPrefix(cache, "collect:"), "/") {
		t.Errorf("the cache key %q reads like a path", cache)
	}
}

// TestAStoredFileNamesBothOwnersButTheKeyStaysTheCallers: the adapter's path gains
// app and tenant; the persisted key is still the bare UUID the row holds.
func TestAStoredFileNamesBothOwnersButTheKeyStaysTheCallers(t *testing.T) {
	got := appname.StoragePath(collect, tenantA, fileKey)
	if got != "collect/"+tenantA.String()+"/"+fileKey.String() {
		t.Errorf("the stored path is %q", got)
	}
	if strings.Count(got, "/") != 2 {
		t.Errorf("%q is not three segments", got)
	}
}

// TestAnAppNamesThatIsNotASlugReachesNoSubscriber: a Name built by conversion
// rather than Parse is refused at every boundary that reads an app back, so the
// address it forms belongs to nobody rather than to a wildcard.
//
// The zero Name is not in this list. It is the deployment that hosts one app and
// names no slug — see TestTheUnsetAppSegmentKeepsTheNamesOneAppAlreadyUses — and a
// name that was set and is broken is a different thing: it forms an address no
// subscription of any app answers to, which is the failure mode that keeps a
// malformed slug from becoming a broker wildcard.
func TestAnAppNamesThatIsNotASlugReachesNoSubscriber(t *testing.T) {
	for _, bad := range []appname.Name{"Collect", "col-lect-", "col.lect", "col/lect", "collect..", appname.Name(strings.Repeat("a", 33))} {
		if bad.Valid() {
			t.Errorf("%q reports itself valid", string(bad))
		}
		subject := appname.Subject(bad, tenantA, "cart.checked_out")
		if matched(appname.Filter(academy, "cart.checked_out"), subject) {
			t.Errorf("%q forms %s, which another app's filter matches", string(bad), subject)
		}
		if !strings.HasPrefix(subject, "platformkit..") {
			t.Errorf("%q forms %q; an invalid name must not reach a token of an address", string(bad), subject)
		}
	}
	if !collect.Valid() || collect.String() != "collect" {
		t.Error("a parsed slug does not read back as itself")
	}
}

// TestTheUnsetAppSegmentKeepsTheNamesOneAppAlreadyUses: the zero Name is the
// deployment of one app. Every constructor answers with the name that deployment
// already writes — the subject, the cookie, the job lock, the limit key, the
// stored path, the broker connection — so the app segment appears exactly when a
// second app could share the name, and no existing jar, volume or consumer is
// renamed by a kernel that has not been told it hosts two.
//
// This is not a way to skip the rule: a process that hosts two apps has to name
// both for their names to differ, and kit/app refuses a slug it cannot parse.
func TestTheUnsetAppSegmentKeepsTheNamesOneAppAlreadyUses(t *testing.T) {
	var none appname.Name
	if none.String() != "" {
		t.Errorf("the zero Name reads %q", none.String())
	}
	if got := appname.Subject(none, tenantA, "cart.checked_out"); got != "platformkit."+tenantA.String()+".cart.checked_out" {
		t.Errorf("the unset app forms subject %q", got)
	}
	if got := appname.Filter(none, "cart.checked_out"); got != "platformkit.*.cart.checked_out" {
		t.Errorf("the unset app forms filter %q", got)
	}
	if got := strings.Join(appname.Filters(none, "cart.checked_out"), " "); got != "platformkit.*.cart.checked_out platformkit.cart.checked_out" {
		t.Errorf("the unset app filters %q", got)
	}
	if got := appname.Durable(none, "cart", "cart.checked_out"); got != "cart-cart-checked_out" {
		t.Errorf("the unset app forms durable %q", got)
	}
	if got := appname.JobLock(none, "purge"); got != "job:purge" {
		t.Errorf("the unset app takes lock %q", got)
	}
	if got := appname.Cookie(none, "session", true); got != "__Host-session" {
		t.Errorf("the unset app names its session cookie %q", got)
	}
	if got := appname.RateLimitKey(none, tenantA, "writes"); got != tenantA.String()+"/writes" {
		t.Errorf("the unset app forms limit key %q", got)
	}
	if got := appname.StoragePath(none, tenantA, fileKey); got != fileKey.String()[:2]+"/"+fileKey.String() {
		t.Errorf("the unset app stores a file at %q", got)
	}
	if got := appname.ConnectionName("platformkit-worker", none); got != "platformkit-worker" {
		t.Errorf("the unset app names its connection %q", got)
	}
}

func TestParseRefusesWhatASubjectTokenCookieOrPathSegmentCouldNotHold(t *testing.T) {
	for _, bad := range []string{"", "Collect", "collect_", "col-lect-", "-collect", "col--lect", "collect.", "col/lect", "col lect", strings.Repeat("a", 33)} {
		if _, err := appname.Parse(bad); err == nil {
			t.Errorf("Parse(%q) accepted it", bad)
		} else if !strings.Contains(err.Error(), "slug") {
			t.Errorf("Parse(%q) refused with %q, which names no grammar to fix", bad, err)
		}
	}
	for _, good := range []string{"collect", "a", "collect-2", "shelf-ui", strings.Repeat("a", 32)} {
		n, err := appname.Parse(good)
		if err != nil {
			t.Errorf("Parse(%q) refused: %v", good, err)
			continue
		}
		if n.String() != good {
			t.Errorf("Parse(%q) read back as %q", good, n)
		}
	}
}

func TestMustParsePanicsWhereATypoIsABootError(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustParse accepted a name no app is named collect-")
		}
	}()
	appname.MustParse("collect-")
}

// TestTheConnectionNameNamesTheProcessAndEveryAppItHosts: one process hosts many
// apps, and an operator reading connectionz has to see which will stop.
func TestTheConnectionNameNamesTheProcessAndEveryAppItHosts(t *testing.T) {
	if got := appname.ConnectionName("platformkit-worker", collect, academy); got != "platformkit-worker/collect+academy" {
		t.Errorf("the connection name is %q", got)
	}
	if got := appname.ConnectionName(""); got != "platformkit" {
		t.Errorf("a process that names itself nothing is %q", got)
	}
	if got := appname.ConnectionName("platformkit-worker"); got != "platformkit-worker" {
		t.Errorf("a host of no apps is %q", got)
	}
}

// TestTheAppAttributeIsOneWord: every record a request, job or event leaves names
// the app the same way, so one query joins them.
func TestTheAppAttributeIsOneWord(t *testing.T) {
	if appname.App != "app" {
		t.Errorf("the attribute is %q", appname.App)
	}
	if appname.Prefix == appname.App {
		t.Error("the namespace and the attribute are one word")
	}
}

// matched answers as NATS does: `*` matches exactly one token, `>` only ever
// trailing, tokens separated by a dot. The subject token count is fixed by the
// event grammar, so no planted wildcard can widen a filter — which is what
// kit/events/transport already pins, restated here for the app segment.
func matched(filter, subject string) bool {
	f := strings.Split(filter, ".")
	s := strings.Split(subject, ".")
	if len(f) != len(s) {
		return false
	}
	for i := range f {
		if f[i] != "*" && f[i] != s[i] {
			return false
		}
	}
	return true
}
