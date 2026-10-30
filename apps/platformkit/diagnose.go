package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/health"
	"github.com/septagon-oss/platformkit/pkit"
)

type diagnosis struct {
	Environment string        `json:"environment"`
	ObservedAt  string        `json:"observed_at"`
	Probe       bool          `json:"probe"`
	Checks      []observation `json:"checks"`
}

type observation struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Operation string `json:"operation"`
	Target    string `json:"target"`
	Observed  string `json:"observed"`
	Next      string `json:"next"`
}

func diagnose(args []string) error {
	fs := flag.NewFlagSet("diagnose", flag.ContinueOnError)
	path := fs.String("config", "config.yaml", "Path to the configuration file")
	env := fs.String("environment", string(pkit.Production), "development, staging, or production")
	data := fs.String("data", "", "Optional local start data directory to inspect for an embedded PostgreSQL cluster")
	probe := fs.Bool("probe", false, "Opt in to bounded network checks")
	if err := fs.Parse(args); err != nil {
		return err
	}
	deployment := environment(*env)
	if deployment == "" {
		return writeDiagnosis(diagnosis{Environment: *env, ObservedAt: time.Now().UTC().Format(time.RFC3339), Probe: *probe,
			Checks: []observation{{ID: "environment", Status: "unsupported", Operation: "select deployment environment", Target: "--environment", Observed: "environment name is not supported", Next: "Choose development, staging, or production."}}}, true)
	}
	cfg, err := config.Load(*path)
	if err != nil {
		status, target, observed, next := configFailure(err)
		return writeDiagnosis(diagnosis{Environment: *env, ObservedAt: time.Now().UTC().Format(time.RFC3339), Probe: *probe,
			Checks: []observation{{ID: "configuration", Status: status, Operation: "load effective configuration", Target: target, Observed: observed, Next: next}}}, true)
	}
	planned := composeReference(cfg, deployment).plan()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	report := diagnosis{Environment: string(deployment), ObservedAt: time.Now().UTC().Format(time.RFC3339), Probe: *probe}
	report.Checks = append(report.Checks,
		observation{ID: "configuration", Status: "ready", Operation: "load effective configuration", Target: "config file and environment", Observed: "configuration loaded and composition resolved", Next: "No action needed."},
		providerObservation(cfg),
		observation{ID: "runtime-version", Status: "ready", Operation: "read runtime version", Target: "PlatformKit process", Observed: runtime.Version(), Next: "No compatibility matrix is defined by this command."},
	)
	report.Checks = append(report.Checks, portObservation(ctx, cfg.Server.Addr, *probe, startDeps{}))
	report.Checks = append(report.Checks, embeddedClusterObservation(*data))
	if !*probe {
		report.Checks = append(report.Checks,
			observation{ID: "database", Status: "not_checked", Operation: "connect to configured database", Target: "database.url", Observed: "network probe was not requested", Next: "Rerun with --probe to check connectivity."},
			observation{ID: "migrations", Status: "not_checked", Operation: "compare migration files with database history", Target: "database.migrate_url", Observed: "network probe was not requested", Next: "Rerun with --probe to inspect migration history."},
		)
		return writeDiagnosis(report, false)
	}
	dbStatus, dbObserved := databaseObservation(ctx, cfg.Database.URL)
	report.Checks = append(report.Checks, observation{ID: "database", Status: dbStatus, Operation: "connect to configured database", Target: "database.url", Observed: dbObserved, Next: databaseNext(dbStatus)})
	if dbStatus != "ready" {
		report.Checks = append(report.Checks, observation{ID: "migrations", Status: "not_checked", Operation: "compare migration files with database history", Target: "database.migrate_url", Observed: "database connectivity was not confirmed", Next: "Resolve the database connection, then rerun with --probe."})
		return writeDiagnosis(report, false)
	}
	state, err := db.InspectMigrations(ctx, cfg.Database.MigrateURL, app.MigrationSources(planned.Modules())...)
	if err != nil {
		report.Checks = append(report.Checks, observation{ID: "migrations", Status: "unreachable", Operation: "read migration history", Target: "database.migrate_url", Observed: "migration history could not be read", Next: "Check migration-role access and database availability; no schema was changed."})
		return writeDiagnosis(report, false)
	}
	if state.Problem != "" {
		report.Checks = append(report.Checks, observation{ID: "migrations", Status: "incompatible", Operation: "compare migration files with database history", Target: "schema_migrations", Observed: state.Problem, Next: "Preserve the database and use a reviewed migration recovery path."})
	} else if len(state.Pending) > 0 {
		report.Checks = append(report.Checks, observation{ID: "migrations", Status: "incompatible", Operation: "compare migration files with database history", Target: "schema_migrations", Observed: fmt.Sprintf("%d selected migration(s) are pending", len(state.Pending)), Next: "Review the changes, then run platformkit migrate explicitly."})
	} else {
		report.Checks = append(report.Checks, observation{ID: "migrations", Status: "ready", Operation: "compare migration files with database history", Target: "schema_migrations", Observed: "selected migration history matches", Next: "No migration action needed."})
	}
	return writeDiagnosis(report, false)
}

func embeddedClusterObservation(data string) observation {
	if data == "" {
		return observation{ID: "embedded-cluster-version", Status: "not_checked", Operation: "read embedded PostgreSQL version marker", Target: "--data", Observed: "local cluster inspection was not requested", Next: "Supply --data <directory> to inspect start's local PostgreSQL cluster."}
	}
	marker := filepath.Join(data, "postgres", "PG_VERSION")
	content, err := os.ReadFile(marker)
	if errors.Is(err, fs.ErrNotExist) {
		return observation{ID: "embedded-cluster-version", Status: "missing", Operation: "read embedded PostgreSQL version marker", Target: marker, Observed: "no cluster version marker was found", Next: "Confirm --data names the intended local directory; a new installation has no cluster version yet."}
	}
	if err != nil {
		return observation{ID: "embedded-cluster-version", Status: "unreachable", Operation: "read embedded PostgreSQL version marker", Target: marker, Observed: "cluster version marker could not be read", Next: "Check access to the local data directory, preserving its contents."}
	}
	found := strings.TrimSpace(string(content))
	if !databaseVersionMatches(found) {
		observed := "cluster version marker does not match this binary's PostgreSQL " + databaseMajor()
		if isMajorVersionMarker(found) {
			observed = "cluster version " + found + " does not match this binary's PostgreSQL " + databaseMajor()
		}
		return observation{ID: "embedded-cluster-version", Status: "incompatible", Operation: "compare embedded PostgreSQL major version", Target: marker, Observed: observed, Next: "Preserve this cluster and select a compatible supported binary or an explicit backup and migration procedure."}
	}
	return observation{ID: "embedded-cluster-version", Status: "ready", Operation: "compare embedded PostgreSQL major version", Target: marker, Observed: "cluster major version matches this binary", Next: "No version action needed."}
}

func isMajorVersionMarker(value string) bool {
	if len(value) == 0 || len(value) > 2 {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func configFailure(err error) (string, string, string, string) {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "does not exist") {
		return "missing", "configuration file", "configuration file was not found", "Create the named configuration file or choose an existing file with --config."
	}
	for _, key := range config.Keys() {
		if !strings.Contains(message, strings.ToLower(key)) {
			continue
		}
		if strings.Contains(message, "empty") || strings.Contains(message, "required") {
			return "missing", key, "required configuration key is empty", "Set " + key + " in the configuration file or through its documented environment override."
		}
		if key == "database.url" || key == "database.migrate_url" {
			return "unsupported", key, "database URL scheme is not supported", "Use a postgres:// or postgresql:// URL for " + key + "."
		}
		return "unsupported", key, "configuration key has an invalid or unsupported value", "Correct " + key + " using a supported value from config.example.yaml."
	}
	if strings.Contains(message, "database.url") || strings.Contains(message, "database.migrate_url") {
		return "unsupported", "database.url or database.migrate_url", "database configuration was refused", "Use a PostgreSQL URL for the named database key."
	}
	return "unsupported", "configuration file and environment", "effective configuration could not be loaded", "Review the configuration syntax and documented environment overrides; sensitive values are not included here."
}

func providerObservation(cfg config.Config) observation {
	providers := []string{
		"cache=" + defaultName(cfg.Cache.Adapter, "memory"),
		"events=" + defaultName(cfg.NATS.Transport, "role default"),
		"files=" + defaultName(cfg.Files.Store, "local-disk"),
		"secrets=" + defaultName(cfg.Secrets.Adapter, "env"),
	}
	return observation{ID: "providers", Status: "not_checked", Operation: "read configured provider selections", Target: "effective composition", Observed: strings.Join(providers, "; ") + "; external provider reachability is not checked", Next: "Use a provider's own safe health check when available."}
}

func defaultName(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func portObservation(ctx context.Context, address string, probe bool, deps startDeps) observation {
	if !probe {
		return observation{ID: "port", Status: "not_checked", Operation: "bind and close configured server address", Target: "server.addr", Observed: "network probe was not requested", Next: "Rerun with --probe to check whether the address can be bound now."}
	}
	if err := ctx.Err(); err != nil {
		return observation{ID: "port", Status: "unreachable", Operation: "bind configured server address", Target: "server.addr", Observed: "address could not be checked before the probe deadline", Next: "Rerun the diagnostic when the process has capacity."}
	}
	listener, err := deps.withDefaults().listen("tcp", address)
	if err != nil {
		return observation{ID: "port", Status: "unreachable", Operation: "bind configured server address", Target: "server.addr", Observed: "address could not be bound", Next: "Choose an available permitted address and port."}
	}
	_ = listener.Close()
	return observation{ID: "port", Status: "ready", Operation: "bind and close configured server address", Target: "server.addr", Observed: "address was available during this check", Next: "The application may still be unable to bind later."}
}

func databaseObservation(ctx context.Context, url string) (string, string) {
	conn, err := db.Open(ctx, url)
	if err != nil {
		return "unreachable", "database did not answer before the probe deadline"
	}
	defer conn.Close()
	if err := health.DatabaseCheck(conn).Check(ctx); err != nil {
		return "unreachable", "database did not answer before the probe deadline"
	}
	return "ready", "database connectivity was confirmed; application readiness is not implied"
}

func databaseNext(status string) string {
	switch status {
	case "ready":
		return "No connectivity action needed; inspect migrations separately."
	case "unsupported":
		return "Use a PostgreSQL URL in database.url."
	default:
		return "Check database availability and the database.url setting, then retry with --probe."
	}
}

func writeDiagnosis(report diagnosis, failed bool) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("write diagnostic report: %w", err)
	}
	if failed || hasFailure(report) {
		return fmt.Errorf("diagnostics found a non-ready check")
	}
	return nil
}

func hasFailure(report diagnosis) bool {
	for _, check := range report.Checks {
		switch check.Status {
		case "missing", "unsupported", "unreachable", "incompatible":
			return true
		}
	}
	return false
}
