package app

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/config"
)

// "app: listening" is the line a person acts on: they open the address, or they
// stop the process they thought had the address. Binding first is what makes it
// true — ListenAndServe binds inside its own goroutine, so the log line used to
// be printed on the way to a bind refusal, with the refusal arriving as the next
// line of the same transcript.
func TestNothingSaysItListensBeforeTheSocketExists(t *testing.T) {
	t.Parallel()
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	var logged bytes.Buffer
	a := &App{
		cfg: config.Config{Server: config.Server{Addr: held.Addr().String()}},
		log: slog.New(slog.NewJSONHandler(&logged, nil)),
	}
	refused := a.serve(context.Background(), http.NotFoundHandler())
	if refused == nil {
		t.Fatal("an occupied address was accepted")
	}
	if !strings.Contains(refused.Error(), "address already in use") {
		t.Errorf("the refusal does not say what the operating system said: %v", refused)
	}
	if strings.Contains(logged.String(), "app: listening") {
		t.Errorf("the run said it listened with no socket behind it:\n%s", logged.String())
	}
}

// The other half: once the socket exists the line is there, with the address the
// configuration named, which is what a caller waiting for the run to be up reads.
func TestTheAddressIsAnnouncedOnceTheSocketExists(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	var logged bytes.Buffer
	a := &App{
		cfg: config.Config{Server: config.Server{Addr: addr}},
		log: slog.New(slog.NewJSONHandler(&logged, nil)),
	}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- a.serve(ctx, http.NotFoundHandler()) }()

	// serve binds in the goroutine this case starts it in, so waiting for the
	// address to answer is what distinguishes "not yet" from "never".
	var conn net.Conn
	for attempt := 0; attempt < 100; attempt++ {
		if conn, err = net.Dial("tcp", addr); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("nothing accepted on %s: %v", addr, err)
	}
	_ = conn.Close()
	cancel()
	if err := <-stopped; err != nil {
		t.Errorf("serve returned %v after the context ended", err)
	}
	if !strings.Contains(logged.String(), "app: listening") || !strings.Contains(logged.String(), addr) {
		t.Errorf("the run never announced the address it is on:\n%s", logged.String())
	}
}
