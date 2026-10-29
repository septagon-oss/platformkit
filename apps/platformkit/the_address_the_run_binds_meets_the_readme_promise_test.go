package main

// The README's one command is the promise a stranger acts on, and one of its
// sentences is a claim about the network: that the HTTP server listens only on
// the machine it runs on. A bare ":port" binds every interface the host has, so
// the sentence is true only of an address that names a host. The two are written
// in different files, so the claim is asked of the value the run actually binds
// when nobody names an address: either may move, and they may not disagree.

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const loopbackClaim = "listens only on your machine"

func TestTheAddressTheRunBindsMeetsTheReadMePromise(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), loopbackClaim) {
		return // the README makes no promise about which machine answers
	}

	var bound string
	refused := startApp(nil, startDeps{
		listen: func(_ string, address string) (net.Listener, error) {
			bound = address
			return nil, errors.New("listen tcp: bind: the case refused")
		},
	})
	if refused == nil {
		t.Fatal("start accepted an address the case refused")
	}
	host, _, err := net.SplitHostPort(bound)
	if err != nil {
		t.Fatalf("start binds %s, which names no port to split", bound)
	}
	switch host {
	case "127.0.0.1", "::1", "localhost", "platformkit.localhost":
	default:
		t.Errorf("README.md says the server %q while start binds %q, whose host part %q names every interface it has",
			loopbackClaim, bound, host)
	}
}
