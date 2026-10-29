package main

// The kept list reads two words off its address — which pair of slugs it is a list
// of — and those two words reach three places: the rows it asks the service for, the
// heading over the grid, and every pager and give-back link the grid draws. A caller
// writes them, so what they cannot do is decide what the page is drawn from. This
// case asks the list four ways: at the pair the copy belongs to, at another pair
// nobody kept anything under, and at two filters that name no pair at all.
//
// The first two together say the filter is a filter and not decoration: a copy kept
// under one pair is listed at that pair and answered at no other, so a crafted
// address cannot read a person's copies back out of a page built for a pair they were
// never shown. The last two say a filter that is no pair at all is refused rather
// than drawn: a filter nothing can be listed under is not the same request as one
// with no filter, whose answer is every copy the person kept.

import (
	"net/http"
	"strings"
	"testing"
)

func TestTheKeptListDrawsOnlyThePairItWasAskedForAndRefusesAFilterThatIsNoPair(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	startSaved(t, cfg, c)
	keeper := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	const title = "The pair the list filters on"
	kept, _, _, _ := oneKeptCopy(t, cfg, keeper, title)

	if list := servedPage(t, cfg, keeper, acmeHost, kept); !strings.Contains(list, title) {
		t.Fatalf("the kept list of the pair the copy was kept under shows no card for it:\n%s", list)
	}

	for _, ask := range []struct{ label, filter string }{
		{"another kind of the same module", "?module=task&kind=note&offset=0"},
		{"another module entirely", "?module=media&kind=asset&offset=0"},
	} {
		code, _, list := askNavigate(t, cfg, keeper, acmeHost, "/app/keeping/saved"+ask.filter)
		if code == http.StatusOK && strings.Contains(list, title) {
			t.Errorf("the kept list asked for %s shows the copy kept under task/task; the pair words name which rows are listed, not which are hidden behind the heading", ask.label)
		}
	}

	for _, ask := range []struct{ label, filter string }{
		{"a kind in capitals", "?module=task&kind=Task&offset=0"},
		{"a kind of wildcards", "?module=task&kind=%2a&offset=0"},
		{"a module with a path in it", "?module=task%2ftask&kind=task&offset=0"},
		{"a module that is a whole address", "?module=https%3a%2f%2fevil.example&kind=task&offset=0"},
	} {
		where := "/app/keeping/saved" + ask.filter
		code, _, body := askNavigate(t, cfg, keeper, acmeHost, where)
		if code == http.StatusOK {
			t.Errorf("GET %s (%s) = 200 and a page; a filter that names no pair at all is not the request with no filter, whose answer is every copy this person kept:\n%s",
				where, ask.label, keeps(body))
			continue
		}
		if code < http.StatusBadRequest || code > 499 {
			t.Errorf("GET %s (%s) = %d; a filter this installation cannot name is the caller's mistake, so the answer is a client fault and not a server one", where, ask.label, code)
		}
		if strings.Contains(body, "/remove") {
			t.Errorf("GET %s (%s) = %d and still drew a give-back form; a page a filter was refused on holds nothing to write", where, ask.label, code)
		}
		if strings.Contains(body, title) {
			t.Errorf("GET %s (%s) = %d and still listed the copy kept under task/task; a refusal answers the filter it was asked with and no other", where, ask.label, code)
		}
	}
}
