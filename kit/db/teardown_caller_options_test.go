package db_test

import (
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// The fixture rewrites a connection URL it did not write: the removal adds its lock
// wait to the admin URL's query, and the fixture points both test URLs at the
// database it created. Rewriting through url.Values.Encode re-encodes every pair the
// caller wrote, and Encode writes a space as `+` while pgx reads a connection URL the
// way libpq does — percent-decoding each pair and leaving `+` as a literal character
// — so a caller whose `options` names a GUC with a space in it gets
// `FATAL: unrecognized configuration parameter "+statement_timeout"` for the whole
// package. The reported refusal is the first case below, and it is run before the
// cure is written, so it fails first.

// callerOption is the GUC a caller might well set on the admin URL, spelled with the
// space between `-c` and its name that libpq asks a URI to percent-encode.
const callerOption = "-c%20statement_timeout%3D7s"

// withCallerOptions is base carrying an `options` pair of the caller's own.
func withCallerOptions(t *testing.T, base, value string) string {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse %q: %v", base, err)
	}
	if u.RawQuery != "" {
		u.RawQuery += "&"
	}
	u.RawQuery += "options=" + value
	return u.String()
}

// askedValues reads the settings one session opened with dsn answers for.
func askedValues(t *testing.T, dsn string, names ...string) []string {
	t.Helper()
	pool, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open %q: %v", dsn, err)
	}
	defer func() { _ = pool.Close() }()
	answers := make([]string, 0, len(names))
	for _, name := range names {
		var got string
		if err := pool.QueryRowContext(t.Context(), "SHOW "+name).Scan(&got); err != nil {
			t.Fatalf("a session opened with %q could not be asked about %s: %v", dsn, name, err)
		}
		answers = append(answers, got)
	}
	return answers
}

// TestTheRemovalKeepsTheCallersOwnOptions holds the reported refusal: the admin URL
// arrives with an option of its own, and the session the removal opens must answer
// both settings — the caller's, which the fixture has no business changing, and the
// lock wait the removal exists to carry.
func TestTheRemovalKeepsTheCallersOwnOptions(t *testing.T) {
	adminURL := os.Getenv("PLATFORMKIT_TEST_ADMIN_URL")
	if adminURL == "" {
		t.Fatalf("PLATFORMKIT_TEST_ADMIN_URL is unset; start the stack with `make up` and export the test URLs")
	}
	for _, caller := range []struct {
		name     string
		options  string
		lockWait string
	}{
		{
			name:     "a caller naming one GUC keeps it, and gets the removal's wait",
			options:  callerOption,
			lockWait: dropLockWait.String(),
		},
		{
			// The server reads the options in order, so the wait the removal appends
			// is the one that answers a setting the caller named too — and the
			// caller's own setting still answers everywhere the removal named none.
			name:     "a caller naming the wait the removal sets is refused by the removal, not by the driver",
			options:  callerOption + "%20-c%20lock_timeout%3D1s",
			lockWait: dropLockWait.String(),
		},
	} {
		t.Run(caller.name, func(t *testing.T) {
			dsn, err := withLockWait(withCallerOptions(t, adminURL, caller.options))
			if err != nil {
				t.Fatalf("withLockWait: %v", err)
			}
			if got := askedValues(t, dsn, "statement_timeout", "lock_timeout"); got[0] != "7s" {
				t.Errorf("statement_timeout = %q on the removal's session, want 7s: the caller's own "+
					"option was rewritten on the way through (%s)", got[0], dsn)
			} else if got[1] != caller.lockWait {
				t.Errorf("lock_timeout = %q on the removal's session, want %s (dropLockWait)", got[1], caller.lockWait)
			}
		})
	}
}

// TestTheFixtureLeavesTheURLItIsGivenAlone is the same rule on the other rewriting
// door in this fixture: pointing a URL at the database the package created removes
// the pair that names a database and keeps every pair that does not, as written.
//
// It asks the string rather than the server on purpose: the pair this fixture removes
// is the pair a caller's own URL carries, and a bare space is in the table because it
// is the spelling that shows a pair is copied rather than re-encoded — the driver's
// answer to it is libpq's rule that a URI percent-encodes a space, which this fixture
// neither enforces nor hides.
func TestTheFixtureLeavesTheURLItIsGivenAlone(t *testing.T) {
	const database = "platformkit_dbtest_pin_options"
	for _, raw := range []string{
		"postgres://postgres:platformkit@localhost:5432/platformkit?sslmode=disable",
		"postgres://postgres:platformkit@localhost:5432/platformkit?sslmode=disable&dbname=other",
		"postgres://app:secret@localhost:5432/platformkit?sslmode=disable&options=-c%20statement_timeout%3D7s&application_name=kit%2Fdb",
		"postgres://app:secret@localhost:5432/platformkit?application_name=my%20app&options=-c search_path=a,b",
	} {
		got, err := intoDatabase(raw, database)
		if err != nil {
			t.Fatalf("intoDatabase(%q): %v", raw, err)
		}
		u, err := url.Parse(got)
		if err != nil {
			t.Fatalf("parse %q: %v", got, err)
		}
		if u.Path != "/"+database {
			t.Errorf("intoDatabase(%q) = %q, which reaches %q", raw, got, u.Path)
		}
		for _, dropped := range []string{"dbname", "database"} {
			if u.Query().Has(dropped) {
				t.Errorf("intoDatabase(%q) = %q, which still names %s", raw, got, dropped)
			}
		}
		// Every pair the caller wrote, other than the two above, reaches pgx as the
		// caller wrote it: the separator between pairs, the encoding inside a value,
		// and the order. Read off the raw query, because that is the string pgx parses.
		want := strings.Split(raw[strings.Index(raw, "?")+1:], "&")
		for _, pair := range want {
			name, _, _ := strings.Cut(pair, "=")
			if name == "dbname" || name == "database" {
				continue
			}
			if !strings.Contains("&"+u.RawQuery+"&", "&"+pair+"&") {
				t.Errorf("intoDatabase(%q) = %q, which does not carry the pair %s it was given",
					raw, got, pair)
			}
		}
	}
}
