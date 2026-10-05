package transport_test

// The envelope's `app`, and the three-way comparison it turns AddressMismatch
// into.
//
// The address was never enough on its own: it says where a message was written
// down, and a publisher that believed it was publishing for another app can write
// at this app's address. The publisher's own claim now travels inside the document
// as a CloudEvents extension, so a delivery reads two copies of the fact and the
// two have to agree with the reader as well. The asymmetry is the rollout, written
// twice already in this tree (appname.Filters, appname.PreviousCookies): the old
// shape — an envelope with no app — is read for ever and never produced.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/events/transport"
)

func TestAnEnvelopeCarriesTheAppThatPublishedIt(t *testing.T) {
	tenant, id := uuid.New(), uuid.New()
	at := func() transport.Event {
		return transport.Event{ID: id, Name: "billing.plan.created", TenantID: tenant,
			Payload: []byte(`{"plan":"team"}`)}
	}

	// An app-less publisher's document is the bytes it always was: the extension is
	// absent, and the subject is the unscoped address.
	body, err := json.Marshal(at())
	if err != nil {
		t.Fatalf("marshal an app-less event: %v", err)
	}
	if strings.Contains(string(body), `"app"`) {
		t.Errorf("an app-less envelope carries %q; the deployment of one app names no app", `"app"`)
	}
	if !strings.Contains(string(body), `"subject":"platformkit.`+tenant.String()+`.billing.plan.created"`) {
		t.Errorf("the app-less envelope's subject moved: %s", body)
	}

	// A scoped publisher's document carries the extension and the scoped subject —
	// the same two calls the publisher's address comes from, which is the promise a
	// bridge reads: subscribe to the subject in the document, get these messages.
	scoped := at()
	scoped.App = appname.MustParse("collect")
	body, err = json.Marshal(scoped)
	if err != nil {
		t.Fatalf("marshal a scoped event: %v", err)
	}
	if !strings.Contains(string(body), `"app":"collect"`) {
		t.Errorf("a scoped envelope carries no app: %s", body)
	}
	if !strings.Contains(string(body), `"subject":"platformkit.collect.`+tenant.String()+`.billing.plan.created"`) {
		t.Errorf("the scoped envelope's subject is not its own app's address: %s", body)
	}

	var back transport.Event
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("unmarshal the scoped envelope: %v", err)
	}
	if back.App != appname.MustParse("collect") {
		t.Errorf("the round trip lost the app: %q came back", back.App)
	}

	// A document whose subject disagrees with the (app, tenantid, type) it carries is
	// refused rather than delivered: the two copies of the address are mutually
	// checking, which is what a delivery needs a comparison against.
	for _, mutate := range []struct {
		why   string
		patch func(m map[string]any)
		want  string
	}{
		{"the subject names another app", func(m map[string]any) {
			m["subject"] = "platformkit.academy." + uuid.New().String() + ".billing.plan.created"
		}, "is not the address of type"},
		{"the app member is not a slug", func(m map[string]any) { m["app"] = "Collect EU" }, "app"},
	} {
		var doc map[string]any
		if err := json.Unmarshal(body, &doc); err != nil {
			t.Fatalf("re-read the envelope to break it: %v", err)
		}
		mutate.patch(doc)
		broken, err := json.Marshal(doc)
		if err != nil {
			t.Fatalf("marshal the broken envelope: %v", err)
		}
		var ev transport.Event
		if err := json.Unmarshal(broken, &ev); err == nil {
			t.Errorf("%s: the document decoded anyway (%s)", mutate.why, broken)
		} else if mutate.want != "" && !strings.Contains(err.Error(), mutate.want) {
			t.Errorf("%s: the refusal was %q", mutate.why, err)
		}
	}
}

// TestAddressMismatchChecksTheDocumentItReads is the three-way comparison, one
// table read twice: the address, the app inside the document, the app reading.
func TestAddressMismatchChecksTheDocumentItReads(t *testing.T) {
	collect := appname.MustParse("collect")
	academy := appname.MustParse("academy")
	none := appname.Name("")
	tenant := uuid.New()
	const name = "billing.plan.created"
	scopedHere := appname.Subject(collect, tenant, name)
	scopedThere := appname.Subject(academy, tenant, name)

	for _, c := range []struct {
		why     string
		app     appname.Name
		subject string
		doc     appname.Name
		want    string // "" means the delivery may run
	}{
		{"its own document at its own address", collect, scopedHere, collect, ""},
		{"another app's document at this app's address", collect, scopedHere, academy, "is not the app reading it"},
		{"its own document at another app's address", collect, scopedThere, collect, "is not app collect's"},
		{"an app-less document at the pre-0074 address", collect, transport.Subject(tenant, name), none, "names no app"},
		{"a document naming no app at this app's scoped address", collect, scopedHere, none, "names no app"},
		{"an app-less document at the address of one app", none, transport.Subject(tenant, name), none, ""},
		{"an app-less document at the oldest address", none, transport.Filters(name)[1], none, ""},
		{"a scoped document read by an app-less deployment", none, scopedHere, collect, "is not the app reading it"},
		{"a reader whose name is not a slug", appname.Name("Collect EU"), scopedHere, collect, "is not an app name"},
		{"a document whose app is not a slug", collect, scopedHere, appname.Name("Collect EU"), "is not an app name"},
	} {
		ev := transport.Event{ID: uuid.New(), Name: name, TenantID: tenant, App: c.doc}
		err := transport.AddressMismatch(c.app, c.subject, ev)
		if c.want == "" {
			if err != nil {
				t.Errorf("%s: the delivery was refused (%v); it may run", c.why, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: nothing was refused", c.why)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: the refusal was %q, want it to name %q", c.why, err, c.want)
		}
		// The disagreement between two apps names the apps, not the address: an
		// operator has to learn which boundary disagreed, and a subject spelled out
		// is not that answer.
		if c.want == "is not the app reading it" && !strings.Contains(err.Error(), c.doc.String()) {
			t.Errorf("%s: the refusal %q does not name the app the document carries", c.why, err)
		}
	}
}
