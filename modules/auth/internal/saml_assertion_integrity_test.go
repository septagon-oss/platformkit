package internal_test

// What the IdP signed is what signs a person in, and nothing beside it: an edited
// document, and a document that carries a second assertion the IdP never wrote next
// to the one it did, are both refused with nothing written; and the spent-assertion
// row one tenant writes is invisible to, and unwritable by, another tenant's
// transaction.

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/beevik/etree"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

const (
	samlProtocolNS  = "urn:oasis:names:tc:SAML:2.0:protocol"
	samlAssertionNS = "urn:oasis:names:tc:SAML:2.0:assertion"
	xmlSignatureNS  = "http://www.w3.org/2000/09/xmldsig#"
)

// sessionsOf counts the sessions one person holds in acme.
func sessionsOf(t *testing.T, f *samlFixture, id uuid.UUID) int64 {
	t.Helper()
	var n int64
	err := db.Run(tenancy.WithTenant(t.Context(), acme), f.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Table("sessions").Where("user_id = ?", id).Count(&n).Error
	})
	if err != nil {
		t.Fatalf("counting sessions: %v", err)
	}
	return n
}

// rewrite decodes a SAMLResponse, lets edit change the document, and encodes it again.
func rewrite(t *testing.T, response string, edit func(root *etree.Element)) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(response)
	if err != nil {
		t.Fatalf("the identity provider's response is not base64: %v", err)
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(raw); err != nil {
		t.Fatalf("the identity provider's response is not XML: %v", err)
	}
	edit(doc.Root())
	out, err := doc.WriteToBytes()
	if err != nil {
		t.Fatalf("writing the edited response: %v", err)
	}
	return base64.StdEncoding.EncodeToString(out)
}

// children returns the direct children of el in namespace ns with local name tag.
func children(el *etree.Element, ns, tag string) []*etree.Element {
	var found []*etree.Element
	for _, child := range el.ChildElements() {
		if child.Tag == tag && child.NamespaceURI() == ns {
			found = append(found, child)
		}
	}
	return found
}

// stripResponseSignature removes the envelope's own signature, leaving the
// assertion's: the document the library then has to verify assertion by assertion.
func stripResponseSignature(root *etree.Element) {
	for _, sig := range children(root, xmlSignatureNS, "Signature") {
		root.RemoveChild(sig)
	}
}

func TestATamperedSAMLAssertionIsRefused(t *testing.T) {
	f := samlSignInUp(t)
	bob := person(t, f.conn, "bob@acme.localhost", contracts.RoleMember)
	location, cookie := f.start(t, host)
	acs, response := f.present(t, location)
	before := f.counts(t)

	// Same length, so nothing but the address moves.
	edited := rewrite(t, response, func(root *etree.Element) {
		for _, el := range root.FindElements("//*") {
			if strings.Contains(el.Text(), "ada@acme.localhost") {
				el.SetText(strings.ReplaceAll(el.Text(), "ada@acme.localhost", "bob@acme.localhost"))
			}
		}
	})
	res := post(t, f.router, host, acs, edited, cookie)
	if res.Code != http.StatusForbidden {
		t.Fatalf("a signed assertion whose address was edited = %d %s, want 403", res.Code, res.Body.String())
	}
	if sessionsOf(t, f, bob) != 0 {
		t.Error("an edited assertion opened a session for the address it was edited to")
	}
	assertNothingWritten(t, f, before)
}

func TestAnAssertionTheIdPSignedStillSignsInWithoutTheEnvelopeSignature(t *testing.T) {
	f := samlSignInUp(t)
	location, cookie := f.start(t, host)
	acs, response := f.present(t, location)

	res := post(t, f.router, host, acs, rewrite(t, response, stripResponseSignature), cookie)
	if res.Code != http.StatusSeeOther {
		t.Fatalf("a response whose own assertion is signed, with the envelope signature removed = %d %s, want 303",
			res.Code, res.Body.String())
	}
}

// The forged copy is never the one read: the library verifies each assertion on its
// own and goes on with the one the IdP signed, so the document either signs in the
// person the IdP vouched for or nobody — never the address the copy names.
func TestAnAssertionWrappedBesideTheSignedOneSignsInOnlyWhomTheIdPNamed(t *testing.T) {
	f := samlSignInUp(t)
	var ada uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), acme), f.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := realUsers().ByEmail(ctx, tx, "ada@acme.localhost")
		if err == nil {
			ada = u.ID
		}
		return err
	})
	if err != nil {
		t.Fatalf("reading ada: %v", err)
	}
	bob := person(t, f.conn, "bob@acme.localhost", contracts.RoleMember)
	location, cookie := f.start(t, host)
	acs, response := f.present(t, location)

	// The envelope signature goes, so each assertion is on its own; a copy of the
	// signed assertion, carrying the original's signature element but another
	// address and id, goes in first.
	wrapped := rewrite(t, response, func(root *etree.Element) {
		stripResponseSignature(root)
		signed := children(root, samlAssertionNS, "Assertion")
		if len(signed) != 1 {
			t.Fatalf("the identity provider's response carries %d assertions, want one", len(signed))
		}
		evil := signed[0].Copy()
		evil.CreateAttr("ID", "id-forged-"+uuid.NewString())
		for _, el := range evil.FindElements("//*") {
			if strings.Contains(el.Text(), "ada@acme.localhost") {
				el.SetText(strings.ReplaceAll(el.Text(), "ada@acme.localhost", "bob@acme.localhost"))
			}
		}
		root.InsertChildAt(signed[0].Index(), evil)
	})
	res := post(t, f.router, host, acs, wrapped, cookie)
	if n := sessionsOf(t, f, bob); n != 0 {
		t.Fatalf("a forged assertion beside the signed one opened %d sessions for the address it names (%d)", n, res.Code)
	}
	switch res.Code {
	case http.StatusForbidden:
	case http.StatusSeeOther:
		if n := sessionsOf(t, f, ada); n != 1 {
			t.Errorf("the wrapped document signed somebody in, and ada holds %d sessions, want one", n)
		}
	default:
		t.Errorf("a forged assertion beside the signed one = %d %s, want 403 or ada's 303", res.Code, res.Body.String())
	}
}

func TestASpentSAMLAssertionIsAnotherTenantsToNeitherReadNorWrite(t *testing.T) {
	f := samlSignInUp(t)
	if res := f.signInTo(t, host); res.Code != http.StatusSeeOther {
		t.Fatalf("the sign-in = %d %s, want 303", res.Code, res.Body.String())
	}
	if got := f.counts(t); got.replays != 1 {
		t.Fatalf("acme holds %d spent assertions after one sign-in, want one", got.replays)
	}
	seed(t, f.conn, globex)

	err := db.Run(tenancy.WithTenant(t.Context(), globex), f.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var seen int64
		if err := tx.DB().Table("saml_assertion_replays").Count(&seen).Error; err != nil {
			return err
		}
		if seen != 0 {
			t.Errorf("globex's transaction reads %d of acme's spent assertions, want none", seen)
		}
		deleted := tx.DB().Exec("DELETE FROM saml_assertion_replays")
		if deleted.Error != nil {
			return deleted.Error
		}
		if deleted.RowsAffected != 0 {
			t.Errorf("globex's transaction deleted %d of acme's spent assertions, want none", deleted.RowsAffected)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("globex's transaction: %v", err)
	}
	if got := f.counts(t); got.replays != 1 {
		t.Errorf("acme holds %d spent assertions after globex's delete, want one", got.replays)
	}

	// A row naming acme, written from globex's transaction, is refused by the policy.
	err = db.Run(tenancy.WithTenant(t.Context(), globex), f.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec(`INSERT INTO saml_assertion_replays (tenant_id, assertion_id, expires_at)
			VALUES (?, 'id-planted', now() + interval '1 hour')`, acme.ID).Error
	})
	if err == nil {
		t.Error("globex's transaction planted a spent assertion in acme's namespace")
	}
}

// A browser arriving from the identity provider's auto-submitting form says so:
// Sec-Fetch-Site cross-site, and the IdP's origin. The session cookie is Lax, so it
// is not attached to that POST, and the kernel's cross-site gate has nothing to
// refuse; the assertion consumer service has to sign the person in.
func TestTheIdPsCrossSitePostSignsIn(t *testing.T) {
	f := samlSignInUp(t)
	location, cookie := f.start(t, host)
	acs, response := f.present(t, location)

	form := url.Values{"SAMLResponse": {response}}
	req := httptest.NewRequest(http.MethodPost, acs, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", f.idp.URL)
	req.AddCookie(&http.Cookie{Name: httpx.CookieName("platformkit_saml_request", false), Value: cookie})
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || sessionCookie(w) == "" {
		t.Fatalf("the IdP's cross-site POST = %d %s, want 303 with a session", w.Code, w.Body.String())
	}
}
