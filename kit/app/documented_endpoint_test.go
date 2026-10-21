package app

// Both documents in this repository print a form of telemetry.otlp_endpoint that
// the boot refuses:
//
//	config.example.yaml:  The collector's gRPC endpoint: "collector.example:4317"
//	kit/config/config.go: https://collector.example:4318, or a host:port for a
//	                      collector on the same network with no TLS
//
// 14dc934 cures it by normalising a bare host:port to its http:// URL in collector().
// A case that hard-codes two endpoint strings proves the two documents
// were true *when it was written* and nothing more: the finding was a documentation
// claim the code did not honour, and the only guard against that recurring is a case
// that reads what the documents print. This file does that, and asks the two
// questions a normalisation can get wrong in the other direction — that it swallows a
// value which is not an endpoint at all, or that it quietly turns an https://
// collector into a plaintext one.

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// endpointForm is what a document can name as an endpoint: a URL with any scheme it
// cares to print, or a bare host:port. The scheme is part of the match, not a prefix
// to strip — a document that prints grpc://collector.example:4317 is promising a
// value the code refuses, and a match that dropped the scheme would read that
// promise as the host:port underneath it and pass.
var endpointForm = regexp.MustCompile(`(?:[A-Za-z][A-Za-z0-9+.-]*://)?[A-Za-z0-9][A-Za-z0-9._-]*:[0-9]{2,5}`)

// TestEveryEndpointFormTheDocumentsPrintIsAccepted reads the telemetry block of the
// shipped example configuration and the doc comment of the field the loader fills,
// and asks collector() about each endpoint form it finds in them.
func TestEveryEndpointFormTheDocumentsPrintIsAccepted(t *testing.T) {
	found := 0
	for _, src := range []struct {
		file  string
		slice func(string) string
	}{
		{"../../config.example.yaml", telemetryBlock},
		{"../config/config.go", otlpComment},
	} {
		body, err := os.ReadFile(src.file)
		if err != nil {
			t.Fatalf("read %s: %v", src.file, err)
		}
		block := src.slice(string(body))
		if block == "" {
			t.Fatalf("%s names no telemetry endpoint at all, so this case has nothing to read: "+
				"the guard has gone quiet, which is how finding 3 arrived", src.file)
		}
		for _, form := range endpointForm.FindAllString(block, -1) {
			found++
			if got, err := collector(form); err != nil {
				t.Errorf("%s prints %q as an accepted telemetry.otlp_endpoint and collector() refuses "+
					"it: %v — the form a document names must not be the form that stops the boot",
					src.file, form, err)
			} else if !strings.HasPrefix(got, "http://") && !strings.HasPrefix(got, "https://") {
				t.Errorf("collector(%q) = %q, which is not a URL the exporter can dial", form, got)
			}
		}
	}
	if found == 0 {
		t.Error("no endpoint form was found in either document, so nothing above was checked")
	}
	t.Logf("checked %d endpoint form(s) the two documents print", found)
}

// TestANormalisedEndpointKeepsItsOwnSecurity asks the other half: the bare host:port
// gains http:// because that is what "a collector on a network that needs no TLS"
// means, but a value that says https must not arrive at a plaintext connection.
func TestANormalisedEndpointKeepsItsOwnSecurity(t *testing.T) {
	secure, err := collector("https://collector.example:4318")
	if err != nil {
		t.Fatalf("an https collector is refused: %v", err)
	}
	if !strings.HasPrefix(secure, "https://") {
		t.Errorf("collector(\"https://collector.example:4318\") = %q: a collector named with TLS "+
			"was normalised to a URL whose scheme means no transport security", secure)
	}
	if plain, err := collector("collector.example:4317"); err != nil || !strings.HasPrefix(plain, "http://") {
		t.Errorf("collector(\"collector.example:4317\") = %q, %v; a bare host:port is documented as "+
			"the no-TLS form, so it normalises to http://", plain, err)
	}
}

// TestAValueThatIsNotAnEndpointIsStillRefused is what the normalisation must not have
// opened: a bare string with no port, credentials, a query, or an empty host are not
// endpoints, and accepting the documented form cannot mean accepting these, or the
// key stops meaning anything and a typo is a silent misroute instead of a refusal.
func TestAValueThatIsNotAnEndpointIsStillRefused(t *testing.T) {
	for _, junk := range []string{
		"",
		"http://",
		"https://user:secret@collector.example:4317",
		"http://collector.example:4317?insecure=true",
		"ftp://collector.example:4317",
		"not a collector at all",
	} {
		if got, err := collector(junk); err == nil {
			t.Errorf("collector(%q) = %q with no error; the key accepts a value that is not an "+
				"endpoint, so a typo in the one place that decides where spans go is not caught at "+
				"the boot", junk, got)
		}
	}
	// Where this boundary sits is the delivery's decision, recorded so the next
	// reader knows it was looked at: a bare host with no port ("http://collector",
	// default port) and a URL with a path are accepted, both of which a collector
	// behind a proxy can legitimately be, and neither is what the example names.
	for _, allowed := range []string{"http://collector", "https://proxy.example/otlp"} {
		if _, err := collector(allowed); err != nil {
			t.Logf("collector(%q) = %v — accepted or refused is the delivery's choice, not this case's", allowed, err)
		}
	}
}

// telemetryBlock is the telemetry stanza of the example configuration, comments and
// all — the comments are the documentation this case reads.
func telemetryBlock(body string) string {
	lines := strings.Split(body, "\n")
	var out []string
	in := false
	for _, l := range lines {
		if strings.HasPrefix(l, "telemetry:") {
			in = true
		} else if in && l != "" && !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "#") {
			break
		}
		if in {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// otlpComment is the doc comment kit/config puts above OTLPEndpoint.
func otlpComment(body string) string {
	i := strings.Index(body, "// OTLPEndpoint is")
	if i < 0 {
		return ""
	}
	rest := body[i:]
	j := strings.Index(rest, "OTLPEndpoint string")
	if j < 0 {
		return rest
	}
	return rest[:j]
}
