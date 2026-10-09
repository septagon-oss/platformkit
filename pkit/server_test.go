package pkit_test

// A server is a process, and a process makes three claims: which environment it
// runs, which half of the application it is, and which tenants it is reached for.
// All three are recorded rather than validated as they are written (decision 0074
// rule 2), and Build is where they are answered — including the one arrangement
// this build refuses outright, two applications over one database.

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/cart"
)

// process is a server with a configuration and an environment and nothing
// hosted: the shape every case below starts from.
func process(t *testing.T) *pkit.Server {
	t.Helper()
	return pkit.NewServer().
		Config(onOneDatabase(t)).
		Deploy(pkit.Deployment{Environment: pkit.Development}).
		Transports(app.Transports{Memory: memory.New}).
		Role(app.All)
}

func TestServerRefusesTheSecondAppOnOneDatabase(t *testing.T) {
	s := process(t).
		Host(pkit.NewApp("collect").Use(doors, desk, cart.Module)).
		Host(pkit.NewApp("wishlist").Use(doors, desk))
	_, err := s.Build(t.Context())
	says(t, err, "T-0231")
	if !strings.Contains(err.Error(), "collect") || !strings.Contains(err.Error(), "wishlist") {
		t.Errorf("the refusal does not name the two apps: %v", err)
	}
}

func TestServerExplainRefusesTheSecondAppItWouldNotBuild(t *testing.T) {
	// The composition file is one application's, and a reader commits it. Two
	// hosted applications are refused by Build, so an Explain that answered anyway
	// would be a second, friendlier answer to the same question — and the file it
	// wrote would describe one of the two as though it were the process.
	s := process(t).
		Host(pkit.NewApp("collect").Use(doors, desk, cart.Module)).
		Host(pkit.NewApp("wishlist").Use(doors, desk))
	_, err := s.Explain()
	says(t, err, "collect")
	says(t, err, "wishlist")
	if _, err2 := s.Build(t.Context()); err2 == nil {
		t.Fatal("Build accepted the same process Explain refused")
	}
}

func TestServerRefusesOneHostClaimedByTwoTenants(t *testing.T) {
	s := process(t).Host(pkit.NewApp("collect").Use(doors, desk),
		pkit.Tenant("Acme", "acme.test"), pkit.Tenant("Globex", "acme.test"))
	_, err := s.Build(t.Context())
	says(t, err, "acme.test")
	says(t, err, "Acme")
	says(t, err, "Globex")
}

func TestServerRefusesATenantWithNoName(t *testing.T) {
	s := process(t).Host(pkit.NewApp("collect").Use(doors, desk), pkit.Tenant("", "acme.test"))
	_, err := s.Build(t.Context())
	says(t, err, "a tenant with no name")
}

func TestServerRefusesNothingHosted(t *testing.T) {
	_, err := process(t).Build(t.Context())
	says(t, err, "hosts nothing: Host(app) first")
}

func TestAHostNoRowServesIsANoteNotARefusal(t *testing.T) {
	// The claim is not a row. Were Build to ask the database which hosts it has,
	// a first boot — an empty database, migrated by this very call — would refuse
	// the application it was about to create.
	s := process(t).Host(pkit.NewApp("collect").Use(doors, desk), pkit.Tenant("Acme", "no-such-row.test"))
	text, err := s.Explain()
	if err != nil {
		t.Fatalf("a claim no row answers is honest: %v", err)
	}
	if !strings.Contains(text, "Acme — no-such-row.test (claimed; the tenant rows decide who is served)") {
		t.Errorf("Explain does not say who claims the host:\n%s", text)
	}
}

func TestServerExplainNamesWhoAnswersEachQuestion(t *testing.T) {
	s := process(t).Host(pkit.NewApp("collect").Use(doors, desk), pkit.Tenant("Acme", "acme.test"))
	text, err := s.Explain()
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	for _, want := range []string{
		"# COMPOSITION — collect · development",
		"## Composition",
		"pkit: collect in development builds 2 modules.",
		"- which host is which tenant: doors.Module",
		"- who is calling: doors.Module",
		"- what they may do: doors.Module",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the composition file never says %q:\n%s", want, text)
		}
	}
	// The composition section is Explain's own bytes, so a kernel sentence that
	// changes lands in the committed file or fails here — never silently.
	app := pkit.NewApp("collect").Use(doors, desk)
	said, err := app.Explain(pkit.Deployment{Environment: pkit.Development})
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	section := strings.SplitN(strings.SplitAfter(text, "## Composition\n")[1], "\n## ", 2)[0]
	if strings.TrimSpace(section) != strings.TrimSpace(said) {
		t.Errorf("the composition section is not what Explain says:\n%s\n----\n%s", section, said)
	}
}

func TestExplainSaysWhenAProvidedContractIsUnneeded(t *testing.T) {
	// The doors module answers the kernel's questions; no *module* needs them.
	// Rule 4 refuses silence about that, so the line names both sides.
	text, err := pkit.NewApp("collect").Use(doors, desk).
		Explain(pkit.Deployment{Environment: pkit.Development})
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	saysLine(t, text, "pkit: doors.Module provides httpx.TenantLoader; no module in collect needs it.")
}

// saysLine fails unless the text carries this exact line.
func saysLine(t *testing.T, text, line string) {
	t.Helper()
	for _, l := range strings.Split(text, "\n") {
		if l == line {
			return
		}
	}
	t.Errorf("no line %q in:\n%s", line, text)
}

// freeAddr picks a port the process has just confirmed is free, so a Run that
// serves can be reached without the case and the listener guessing at one.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

// answersWithin is the wait for a process to be reachable: how long a boot may
// take before the case calls it a failure rather than a slow machine.
const answersWithin = 60 * time.Second

// TestServerRunsTheApplicationItHosts is the one claim Build cannot answer by
// existing: a process told to run its application must not refuse that
// application for having been built. kit/app's Run is its own Start and the
// halves the role names, so a Run that went through Build first would meet its
// own "already built" refusal having migrated the database and served nothing.
func TestServerRunsTheApplicationItHosts(t *testing.T) {
	cfg := onOneDatabase(t)
	cfg.Server.Addr = freeAddr(t)
	s := pkit.NewServer().Config(cfg).
		Deploy(pkit.Deployment{Environment: pkit.Development, Transports: app.Transports{Memory: memory.New}}).
		Role(app.All).
		Host(pkit.NewApp("collect").Use(doors, desk), pkit.Tenant("Acme", "acme.test"))
	// The context stays live while the process boots, because booting is what is
	// under test: cancelled before the call, Run refuses at its first connect and
	// the case would measure the cancellation. The process is stopped by the effect
	// it exists to produce — a served request — and never by a shorter bound than
	// the one that fails the case when that effect never lands.
	ctx, cancel := context.WithCancel(t.Context())
	ran := make(chan error, 1)
	go func() { ran <- s.Run(ctx) }()

	var err error
	deadline := time.Now().Add(answersWithin)
	for !answered(t, cfg.Server.Addr) {
		select {
		case early := <-ran:
			t.Fatalf("Run returned %v without serving anything", early)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("Run served nothing within %s", answersWithin)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err = <-ran:
	case <-time.After(answersWithin):
		t.Fatal("Run did not return when its context was cancelled")
	}
	if err != nil {
		if strings.Contains(err.Error(), "already built") {
			t.Fatalf("Run refused the app it had just started itself: %v", err)
		}
		t.Errorf("Run returned after its context was cancelled: %v", err)
	}
	if got := tablesIn(t, cfg.Database.MigrateURL); got == 0 {
		t.Errorf("Run migrated nothing: %d tables", got)
	}
}

// answered is whether this address has a server that answers the probes — the
// surface a process is up the moment it answers, at the pod address, where no
// tenant has to resolve and nobody has to be signed in.
func answered(t *testing.T, addr string) bool {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/health", nil)
	if err != nil {
		t.Fatalf("probe request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false // nothing is listening yet, which is the only reason this is asked in a loop
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
