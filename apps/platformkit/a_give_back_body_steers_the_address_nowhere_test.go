package main

// After a give-back takes a copy, the kept list answers 303 with an address it
// builds itself: its own address plus the two words — module and kind — the
// submitted body carries, joined without escaping. The drawn control can only ever
// offer the pair of the row it drew; a truncated, bookmarked or hand-built body can
// carry anything there, so the answer has to stay where the mount put it. What a
// person may read off it: no scheme and no host, so it sends nobody off this
// installation; the list's own path, so the pair words are only ever query text of
// one page this mount serves; and no line break of the body, so a submitted form
// cannot write a header line into the answer to its own write. And the copy is gone
// in every case — words around the quoted number are no reason to refuse the write.

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestTheGiveBackSendsThePersonToThisInstallationsOwnKeptListWhateverTheBodyCarries(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	startSaved(t, cfg, c)
	keeper := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	list, _, _, revision := oneKeptCopy(t, cfg, keeper, "Where a give-back sends the person")
	listPath, err := url.Parse(list)
	if err != nil {
		t.Fatalf("the address the record drew for the kept list, %q: %v", list, err)
	}

	for _, pairWord := range []struct{ label, module string }{
		{"the pair the grid draws", "task"},
		{"a line break and a cookie", "task%0d%0aSet-Cookie%3a%20leak%3d1"},
		{"a host of its own", "%2f%2fevil.example"},
		{"another installation", "http%3a%2f%2fevil.example%2fkeep"},
		{"a fragment", "task%23frag"},
	} {
		kept, giveBack, title, shown := oneKeptCopy(t, cfg, keeper, "Where a give-back sends the person "+pairWord.label)
		if shown != revision {
			t.Fatalf("a copy's own list printed revision %s where the first one printed %s; this case quotes one number", shown, revision)
		}
		code, location, fault := askForm(t, cfg, keeper, acmeHost, giveBack,
			"expectedRevision="+shown+"&module="+pairWord.module+"&kind=task")
		if code != http.StatusSeeOther {
			t.Fatalf("give-back with module=%q = %d (%s); want the 303 the list answers a keeper with", pairWord.label, code, fault)
		}
		if strings.ContainsAny(location, "\r\n") {
			t.Errorf("the give-back's Location %q carries a line break; a submitted body may not write a header line into the answer to its own write", location)
		}
		to, err := url.Parse(location)
		if err != nil {
			t.Errorf("the give-back's Location %q is no address a browser could follow: %v", location, err)
			continue
		}
		if to.IsAbs() || to.Host != "" {
			t.Errorf("the give-back with module=%q sends the person to %q; want an address with no scheme and no host, because the page owns where a write returns to and the body owns only which pair is listed", pairWord.label, location)
		}
		if to.Path != listPath.Path {
			t.Errorf("the give-back with module=%q redirects to path %q; want the kept list's own %q, so the pair words are query text of one page this mount serves", pairWord.label, to.Path, listPath.Path)
		}
		if still := servedPage(t, cfg, keeper, acmeHost, kept); strings.Contains(still, title) {
			t.Errorf("the copy survived the give-back whose body carried %q; the write was asked for at the revision printed beside the copy", pairWord.label)
		}
	}
}
