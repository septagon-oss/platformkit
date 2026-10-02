package valkey

// An adapter's error reaches a boot log, and the address it was given may carry a
// password (redis://:secret@host is the form go-redis itself documents). Whatever
// kit/config refuses upstream, Connect and New are exported constructors a
// composition may call with a config.Cache it built, so the adapter is where the
// credential is kept out of the sentence.

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
)

const secret = "hunter2-not-for-logs"

// TestARefusedStoreNamesNoPassword: a store that does not answer is named by its
// host, never by the password in its address.
func TestARefusedStoreNamesNoPassword(t *testing.T) {
	_, err := Connect(context.Background(), config.Cache{App: "pkit", URL: "redis://:" + secret + "@127.0.0.1:1"})
	if err == nil {
		t.Fatal("Connect to a port nothing listens on succeeded")
	}
	if !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Fatalf("the refusal does not name the store it could not reach: %v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("the refusal carries the store's password: %v", err)
	}
}

// TestAnUnparsableAddressNamesNoPassword: the same rule for an address the
// client library refuses to parse.
func TestAnUnparsableAddressNamesNoPassword(t *testing.T) {
	_, err := New(config.Cache{App: "pkit", URL: "redis://:" + secret + "@127.0.0.1:6379/not-a-db"})
	if err == nil {
		t.Fatal("New accepted a database number that is not a number")
	}
	if !strings.Contains(err.Error(), "cache.url") {
		t.Fatalf("the refusal does not name the setting to fix: %v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("the refusal carries the store's password: %v", err)
	}
}
