package main

// The cases behind the way this suite waits for its own application.
//
// Every served case here boots the reference application with start and then asks
// it over HTTP. The wait used to be a dial at the configured address, and the
// address used to be one freeAddr had bound and released — which left a window
// exactly as long as the composition's migration (Run migrates before it listens,
// and the wait for the composition's advisory lock is bounded only by the caller's
// context: kit/db/migrate.go, holdCompositionLock). Inside that window the address
// could be answered by somebody else, and the case could not tell: it read a
// stranger's 404s as a composition that had lost its own routes.
//
// Three claims hold the cure up, and each is asked here rather than argued in a
// comment: that the sentence the wait answers to names *this* address and not any
// address; that a listener which is not this application is not accepted as one; and
// that the address is not one the kernel is handing out to other processes while
// this case holds it open for nobody.

import (
	"bytes"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// ephemeralLow is where the range of addresses the kernel hands out — to every
// bind(":0") and every outbound connection on the host — begins. The development
// host's /proc/sys/net/ipv4/ip_local_port_range says 32768 60999, and the Linux
// default is the same; the case below reads the file rather than trusting either,
// so a deployment that widened its range reddens instead of quietly sharing
// addresses with this suite.
const ephemeralLow = 32768

// TestTheServingSentenceNamesOneAddress is the rule the wait answers to: the
// kernel's sentence about listening, for the address this case named. A match on the
// sentence alone would be closed by any application on the host, which is the same
// mistake as dialling the address and asking whoever picked up.
func TestTheServingSentenceNamesOneAddress(t *testing.T) {
	const mine = "127.0.0.1:25001"
	listening := make(chan struct{}, 1)
	closed := func() bool {
		select {
		case <-listening:
			return true
		default:
			return false
		}
	}
	watch := &servingHandler{addr: mine, inner: slog.DiscardHandler, ready: listening}
	watch.Handle(t.Context(), slog.NewRecord(time.Now(), slog.LevelInfo, "app: listening", 0))
	if closed() {
		t.Error("a sentence naming no address closed the wait")
	}
	other := slog.NewRecord(time.Now(), slog.LevelInfo, "app: listening", 0)
	other.AddAttrs(slog.String("addr", "127.0.0.1:25002"))
	watch.Handle(t.Context(), other)
	if closed() {
		t.Error("a sentence about somebody else's address closed the wait")
	}
	mineRecord := slog.NewRecord(time.Now(), slog.LevelInfo, "app: listening", 0)
	mineRecord.AddAttrs(slog.String("addr", mine))
	watch.Handle(t.Context(), mineRecord)
	if !closed() {
		t.Error("the sentence naming this address did not close the wait")
	}
}

// TestAListeningStrangerIsNotTakenForThisApplication causes the failure the wait
// used to have: a listener that is not this application holds the address and answers
// 404 to everything — the shape another suite's application takes — and the case
// boots anyway.
//
// The scenario has to be run in its own process, because what it owes is a reported
// failure: a subtest that fails fails its parent, so the only honest way to ask
// whether start refuses is to watch a run of this binary refuse and read what it
// said. It must say its own application never served, and it must not have asked the
// stranger for its sign-in page.
func TestAListeningStrangerIsNotTakenForThisApplication(t *testing.T) {
	if os.Getenv(strangerHoldsTheAddress) == "1" {
		path, cfg := configure(t)
		install(t, path)
		c := compose(cfg)
		foreigner, err := net.Listen("tcp", cfg.Server.Addr)
		if err != nil {
			t.Fatalf("hold %s for the stranger: %v", cfg.Server.Addr, err)
		}
		defer foreigner.Close()
		go func() { _ = http.Serve(foreigner, http.NotFoundHandler()) }()

		start(t, cfg, c.modules, app.Options{
			Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans, Authenticate: c.auth.Authenticate,
			Role: app.Web, Transport: memory.New(), Log: quiet(),
		})
		// What a case that believed that wait would then ask — the same question
		// TestPinnedAddresses asks, and the one the stranger answers with a 404.
		if code, body := do(t, cfg, nil, http.MethodGet, acmeHost, pinnedSignIn, ""); code != http.StatusOK {
			t.Errorf("the sign-in page %s = %d %s", pinnedSignIn, code, body)
		}
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestAListeningStrangerIsNotTakenForThisApplication",
		"-test.v", "-test.timeout=5m")
	cmd.Env = append(os.Environ(), strangerHoldsTheAddress+"=1")
	out, err := cmd.CombinedOutput()
	report := string(out)
	if err == nil {
		t.Errorf("a case whose address was held by a stranger came up clean:\n%s", report)
	}
	if !strings.Contains(report, "stopped before it served at") {
		t.Errorf("the case did not say that its own application never served:\n%s", report)
	}
	if strings.Contains(report, "404 page not found") {
		t.Errorf("the case read a stranger's answer as its own:\n%s", report)
	}
}

// strangerHoldsTheAddress is the switch between the two halves of the case above:
// set, this is the run whose address somebody else holds; unset, it is the one that
// watches it and reads what it said.
const strangerHoldsTheAddress = "PLATFORMKIT_TEST_ADDRESS_HELD_BY_A_STRANGER"

// TestTheServingAddressIsNotOneTheKernelCouldGiveAway holds the other half: an
// address this case leaves unbound while its composition migrates is only safe if
// nothing else can be handed it.
func TestTheServingAddressIsNotOneTheKernelCouldGiveAway(t *testing.T) {
	if testPortHigh >= ephemeralLow {
		t.Errorf("the serving band ends at %d and the kernel starts handing addresses out at %d", testPortHigh, ephemeralLow)
	}
	body, err := os.ReadFile("/proc/sys/net/ipv4/ip_local_port_range")
	if err != nil {
		t.Fatalf("read the range this host hands out: %v", err)
	}
	fields := strings.Fields(string(body))
	if len(fields) != 2 {
		t.Fatalf("ip_local_port_range reads %q, want two numbers", strings.TrimSpace(string(body)))
	}
	low, err := strconv.Atoi(fields[0])
	if err != nil {
		t.Fatalf("ip_local_port_range: %v", err)
	}
	if testPortHigh >= low {
		t.Errorf("this host hands out addresses from %d and the serving band ends at %d: a released address could be given away", low, testPortHigh)
	}

	_, portText, err := net.SplitHostPort(freeAddr(t))
	if err != nil {
		t.Fatalf("the address freeAddr picked does not carry a port: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("the port freeAddr picked is not a number: %v", err)
	}
	if port < testPortLow || port > testPortHigh {
		t.Errorf("freeAddr picked port %d, outside the serving band %d-%d", port, testPortLow, testPortHigh)
	}
}

// TestTheKernelStillSaysWhereItIsListening is the tripwire under the wait. The
// return from waitServing depends on one sentence in the kernel's own log and the
// address written beside it; were either to change, every served case would wait out
// the whole boot deadline rather than say what went wrong.
func TestTheKernelStillSaysWhereItIsListening(t *testing.T) {
	src, err := os.ReadFile("../../kit/app/app.go")
	if err != nil {
		t.Fatalf("read kit/app/app.go: %v", err)
	}
	if !bytes.Contains(src, []byte(`"app: listening", "addr"`)) {
		t.Error(`kit/app no longer logs "app: listening" beside an "addr" attribute, and servingHandler matches the pair`)
	}
}
