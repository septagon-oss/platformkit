package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiagnoseWithoutProbeLeavesNetworkChecksUnobserved(t *testing.T) {
	output, err := captureDiagnosis(t, []string{"--config", "../../config.example.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	var report diagnosis
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("diagnose output is not JSON: %v\n%s", err, output)
	}
	for _, id := range []string{"port", "database", "migrations"} {
		if statusFor(report, id) != "not_checked" {
			t.Errorf("%s status = %q, want not_checked", id, statusFor(report, id))
		}
	}
	if strings.Contains(strings.ToLower(output), "production-ready") {
		t.Errorf("diagnostic implies application readiness: %s", output)
	}
}

func TestDiagnoseMissingConfigProducesSafeAction(t *testing.T) {
	const sentinel = "password-that-must-not-appear"
	output, err := captureDiagnosis(t, []string{"--config", "missing-" + sentinel + ".yaml"})
	if err == nil {
		t.Fatal("missing configuration returned success")
	}
	if strings.Contains(output, sentinel) || strings.Contains(err.Error(), sentinel) {
		t.Fatalf("secret sentinel appeared in diagnostic output: %q, %v", output, err)
	}
	var report diagnosis
	if jsonErr := json.Unmarshal([]byte(output), &report); jsonErr != nil {
		t.Fatalf("diagnose output is not JSON: %v\n%s", jsonErr, output)
	}
	if statusFor(report, "configuration") != "missing" {
		t.Fatalf("configuration status = %q, want missing", statusFor(report, "configuration"))
	}
	if !strings.Contains(report.Checks[0].Next, "Create") {
		t.Fatalf("missing configuration has no safe next action: %+v", report.Checks[0])
	}
}

func TestDatabaseProbeHidesConnectionDetails(t *testing.T) {
	const secret = "query-secret-sentinel"
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	status, observed := databaseObservation(ctx, "postgres://operator:private@127.0.0.1:1/platformkit?token="+secret)
	if status != "unreachable" {
		t.Fatalf("database status=%q, want unreachable", status)
	}
	if strings.Contains(observed, secret) || strings.Contains(observed, "private") {
		t.Fatalf("database observation exposed connection details: %q", observed)
	}
}

func TestPortProbeClosesItsListener(t *testing.T) {
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reserved.Addr().String()
	if err := reserved.Close(); err != nil {
		t.Fatal(err)
	}
	got := portObservation(t.Context(), address, true)
	if got.Status != "ready" {
		t.Fatalf("port observation=%+v, want ready", got)
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("diagnostic left %s held: %v", address, err)
	}
	_ = listener.Close()
}

func TestPortProbeReportsOccupiedAddress(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	got := portObservation(t.Context(), listener.Addr().String(), true)
	if got.Status != "unreachable" || !strings.Contains(got.Next, "available") {
		t.Fatalf("occupied port observation=%+v", got)
	}
}

func TestDiagnoseReportsEmbeddedClusterVersionMismatch(t *testing.T) {
	data := t.TempDir()
	cluster := filepath.Join(data, "postgres")
	if err := os.Mkdir(cluster, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(cluster, "PG_VERSION")
	if err := os.WriteFile(marker, []byte("15\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := embeddedClusterObservation(data)
	if got.Status != "incompatible" || !strings.Contains(got.Observed, "15") || !strings.Contains(got.Next, "Preserve") {
		t.Fatalf("cluster version observation=%+v", got)
	}
	content, err := os.ReadFile(marker)
	if err != nil || string(content) != "15\n" {
		t.Fatalf("diagnostic changed the version marker: %q, %v", content, err)
	}
}

func captureDiagnosis(t *testing.T, args []string) (string, error) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	err = diagnose(args)
	_ = writer.Close()
	os.Stdout = previous
	output, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(output), err
}

func statusFor(report diagnosis, id string) string {
	for _, check := range report.Checks {
		if check.ID == id {
			return check.Status
		}
	}
	return ""
}
