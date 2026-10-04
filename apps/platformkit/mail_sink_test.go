package main

import (
	"os"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
)

// keepMailInTheProcess writes the same configuration with `mail.sink: mailbox`
// added, and returns that file and what the loader makes of it.
//
// A case asks for this by name rather than getting it by default, and the reason
// is the defect one review measured: a composition wired with an in-memory sink
// because nothing else was configured answers a sign-up with 202 and writes an
// account whose confirmation link no person will ever read. So `mailer` wires a
// mailbox only where the configuration says `mailbox`, refuses-by-nothing other
// wise (mail nil, which is what makes modules/auth answer a reasoned 503 and write
// no row), and every case that reads a confirmation link, an invitation link or a
// notice out of this process's memory says so in its own setup. Every assertion in
// them is the one it was written with: the sink they opt into hands back the same
// *notification.Mailbox they have always read.
func keepMailInTheProcess(t *testing.T, path string, cfg config.Config) (string, config.Config) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the configuration to add its mail sink: %v", err)
	}
	if strings.Contains(string(body), "\nmail:") {
		t.Fatal("this configuration already names a mail sink; mailbox refuses beside one")
	}
	withSink := path + "-mailbox"
	if err := os.WriteFile(withSink, append(body, []byte("mail:\n  sink: \"mailbox\"\n")...), 0o600); err != nil {
		t.Fatalf("write the configuration with its mail sink: %v", err)
	}
	loaded, err := config.Load(withSink)
	if err != nil {
		t.Fatalf("load the configuration with its mail sink: %v", err)
	}
	return withSink, loaded
}

// TestTheMailboxSinkIsSomethingToAskFor pins the three states of one configuration
// key from the side the loader reads: no server and no sink means nothing sends and
// the command that would promise a message refuses; `sink: mailbox` means this
// process keeps them; a host means an SMTP session. A fourth state — a sink beside
// a host — is refused, because the two answer the same question differently.
func TestTheMailboxSinkIsSomethingToAskFor(t *testing.T) {
	path, nothing := configure(t)
	if nothing.Mail.Enabled() || nothing.Mail.Mailbox() {
		t.Errorf("the configuration with no mail block is wired as %+v; it names nothing", nothing.Mail)
	}

	written, mailbox := keepMailInTheProcess(t, path, nothing)
	if !mailbox.Mail.Mailbox() || mailbox.Mail.Enabled() {
		t.Fatalf("%s does not carry the sink it was written with: %+v", written, mailbox.Mail)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, ask := range []struct{ name, block, want string }{
		{"a sink beside a server",
			"mail:\n  sink: \"mailbox\"\n  host: \"smtp.localhost\"\n  from: \"noreply@acme.localhost\"\n",
			"say one"},
		{"a sink that is not one",
			"mail:\n  sink: \"inbox\"\n",
			"mailbox"},
	} {
		file := t.TempDir() + "/config.yaml"
		if err := os.WriteFile(file, append(append([]byte{}, body...), []byte(ask.block)...), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.Load(file); err == nil || !strings.Contains(err.Error(), ask.want) {
			t.Errorf("%s loaded as %v; want the refusal to say %q", ask.name, err, ask.want)
		}
	}
}
