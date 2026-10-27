package main

// The kept list is the only place a person can hand a copy back, and it hands over
// one number with the form: the revision printed beside the copy. Two of that
// control's answers at the address a browser posts to are asked nowhere else.
//
// A body whose revision is no number at all is a write over a copy the caller was
// never shown at, so it is refused with a reason, writes nothing, emits nothing and
// leaves the copy on its keeper's list. The page reads the field with a plain
// conversion and falls back to no revision when the text is not one, so this is the
// shape a truncated or rewritten form field arrives as, and the answer that keeps a
// copy is the service's, not the page's.
//
// And once the copy is gone, the very same form pressed again — a double press, or a
// Back button followed by the control — is answered the way the first press was. A
// kept copy is absent once it is given back, absence is what the person asked for, so
// the second press is silence: the same redirect, no error page, and nothing left
// behind that would trap the row on the list.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

func TestAGiveBackQuotingANumberThatIsNoNumberKeepsTheCopy(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans,
		Authenticate: c.auth.Authenticate, Role: app.All, Transport: memory.New(), Log: quiet()})
	keeper := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	kept, giveBack, title, revision := oneKeptCopy(t, cfg, keeper, "Reletter the returns shelf")

	for _, revisionText := range []string{"not-a-number", "1.5", ""} {
		code, location, fault := askForm(t, cfg, keeper, acmeHost, giveBack,
			"expectedRevision="+revisionText+"&module=task&kind=task")
		if code < http.StatusBadRequest || location != "" {
			t.Errorf("POST of the drawn give-back with expectedRevision=%q = %d (Location %q, %s); want a refusal that claims nothing",
				revisionText, code, location, fault)
		}
		if still := servedPage(t, cfg, keeper, acmeHost, kept); !strings.Contains(still, title) {
			t.Fatalf("the copy is gone from its keeper's list after a give-back quoting %q:\n%s", revisionText, still)
		}
	}

	if code, location, fault := askForm(t, cfg, keeper, acmeHost, giveBack,
		"expectedRevision="+revision+"&module=task&kind=task"); code != http.StatusSeeOther {
		t.Errorf("the keeper's own give-back after three refusals = %d (Location %q, %s); want the 303 to the grid",
			code, location, fault)
	}
}

func TestAGiveBackPressedAgainAfterItTookTheCopyIsSilence(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans,
		Authenticate: c.auth.Authenticate, Role: app.All, Transport: memory.New(), Log: quiet()})
	keeper := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	kept, giveBack, title, revision := oneKeptCopy(t, cfg, keeper, "Reletter the returns shelf")
	form := "expectedRevision=" + revision + "&module=task&kind=task"

	first, firstTo, fault := askForm(t, cfg, keeper, acmeHost, giveBack, form)
	if first != http.StatusSeeOther {
		t.Fatalf("the keeper's give-back = %d (%s), want the 303 to the grid", first, fault)
	}
	if again := servedPage(t, cfg, keeper, acmeHost, kept); strings.Contains(again, title) {
		t.Fatalf("the copy outlived the give-back that quoted its own revision %s:\n%s", revision, again)
	}

	second, secondTo, fault := askForm(t, cfg, keeper, acmeHost, giveBack, form)
	if second != first || secondTo != firstTo {
		t.Errorf("the same give-back pressed again = %d (Location %q, %s); want the first press's %d to %q, because a copy already given back is the state that was asked for",
			second, secondTo, fault, first, firstTo)
	}
	if gone := servedPage(t, cfg, keeper, acmeHost, kept); strings.Contains(gone, title) {
		t.Errorf("the copy is back on its keeper's list after a second press:\n%s", gone)
	}
}

// oneKeptCopy keeps a copy of a new task through the control the record screen drew,
// then reads off the kept list the three things a browser would send: the list's own
// address, the give-back's address and the revision printed beside the copy.
func oneKeptCopy(t *testing.T, cfg config.Config, keeper *http.Client, title string) (kept, giveBack, shown, revision string) {
	t.Helper()
	id := newTask(t, cfg, keeper, title, "normal")
	record := servedPage(t, cfg, keeper, acmeHost, "/app/task/tasks/"+id)
	if code, _, fault := askForm(t, cfg, keeper, acmeHost, drawnAddress(t, record, "/saved/save"),
		saveForm(t, record, id)); code != http.StatusSeeOther {
		t.Fatalf("keep a copy: %d (%s)", code, fault)
	}
	kept = drawnAddress(t, record, "/saved?module=")
	list := servedPage(t, cfg, keeper, acmeHost, kept)
	if !strings.Contains(list, title) {
		t.Fatalf("the kept list does not show the copy that was just kept:\n%s", list)
	}
	return kept, drawnAddress(t, list, "/remove"), title, quotedRevision(t, list)
}
