package db_test

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestRemovalExtendsTheEffectiveOptionsWithoutChangingLiteralPlus(t *testing.T) {
	const raw = "postgres://localhost/platformkit?application_name=caller+name&options=-c%20statement_timeout%3D1s&options=-c%20statement_timeout%3D7s"
	dsn, err := withLockWait(raw)
	if err != nil {
		t.Fatal(err)
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if got := config.RuntimeParams["application_name"]; got != "caller+name" {
		t.Errorf("application_name = %q, want literal caller+name", got)
	}
	options := config.RuntimeParams["options"]
	if !strings.HasPrefix(options, "-c statement_timeout=7s ") {
		t.Errorf("effective options = %q, want the last caller value preserved", options)
	}
	if !strings.HasSuffix(options, "-clock_timeout=3000") {
		t.Errorf("effective options = %q, want the removal's lock wait last", options)
	}
}
