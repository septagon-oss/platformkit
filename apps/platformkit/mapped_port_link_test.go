package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/modules/notification"
)

// An installation published on a port that is not the one it listens on — a
// container run as `-p 38591:8080` — mails the port its people use. The socket
// cannot know that port, so the installation declares it: in its host of record
// or in server.public_host, the two places the kernel names for it. Both are
// declared here, and the reset link a person asks for through the published port
// has to lead back to it, never to the port behind the mapping.
func TestAMappedInstallationMailsThePortItIsPublishedAt(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)

	published, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = published.Close() })
	_, port, err := net.SplitHostPort(published.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	public := acmeHost + ":" + port
	cfg.Server.PublicHost = public

	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	// The host record's half of the declaration; a kernel that only reads the
	// configured address may refuse it, and that is not this case's question.
	_ = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		all, err := c.tenants.List(ctx, tx)
		if err != nil {
			return err
		}
		for _, one := range all {
			if one.Slug == "acme" {
				_, err = c.tenants.AddHost(ctx, tx, one.ID, public, true)
			}
		}
		return err
	})

	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)
	go forward(published, cfg.Server.Addr)

	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the reference application mails through %T", c.mail)
	}
	req, err := http.NewRequest(http.MethodPost, "http://"+published.Addr().String()+"/api/v1/public/auth/password/forgot",
		strings.NewReader(`{"email":"`+adminEmail+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = public
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("asking for a link at %s = %d, want 200", public, res.StatusCode)
	}

	var mail string
	eventually(t, "the reset link to be mailed", func() bool {
		for _, sent := range box.Sent() {
			if sent.To == adminEmail {
				mail = sent.Body
				return true
			}
		}
		return false
	})
	if want := "http://" + public + "/app/auth/reset?token="; !strings.Contains(mail, want) {
		t.Errorf("a person who reached %s was mailed a link elsewhere:\n%s", public, mail)
	}
}

// forward is the mapping: every connection to the published port is carried to
// the port the application listens on.
func forward(published net.Listener, to string) {
	for {
		in, err := published.Accept()
		if err != nil {
			return
		}
		go func() {
			out, err := net.Dial("tcp", to)
			if err != nil {
				_ = in.Close()
				return
			}
			go func() { _, _ = io.Copy(out, in); _ = out.Close() }()
			_, _ = io.Copy(in, out)
			_ = in.Close()
		}()
	}
}
