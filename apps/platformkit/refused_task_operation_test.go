package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// A task creation the command refuses keeps its operation identity across a retry,
// commits no task and no task event, records only failed evidence, and its receipt
// is invisible to another tenant's administrator and to that tenant's transaction.
func TestARefusedTaskCreationLeavesOnlyFailedEvidence(t *testing.T) {
	cfg, mods, fixture := deviceComposition(t)
	install(t, fixture.path)
	start(t, cfg, mods, fixture.opts)
	client := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	write := func() (int, string, http.Header) {
		req, err := http.NewRequest("POST", "http://"+cfg.Server.Addr+tasksPath, strings.NewReader(`{"title":""}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Host = acmeHost
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(httpx.IdempotencyKeyHeader, "offline.task.refused")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, string(body), res.Header
	}
	status, body, headers := write()
	if status < 400 || status >= 500 {
		t.Fatalf("refused create = %d %s, want a 4xx refusal", status, body)
	}
	location := headers.Get(httpx.OperationLocationHeader)
	if headers.Get(httpx.OperationIDHeader) != "offline.task.refused" || location == "" {
		t.Fatalf("headers = %v", headers)
	}
	again, againBody, replay := write()
	if again != status || againBody != body || replay.Get(httpx.OperationLocationHeader) != location {
		t.Errorf("retry = %d %s headers %v, want %d with the same location", again, againBody, replay, status)
	}

	code, receiptBody := do(t, cfg, client, "GET", acmeHost, location, "")
	if code != http.StatusOK {
		t.Fatalf("receipt = %d %s", code, receiptBody)
	}
	var receipt struct {
		Ref     string `json:"ref"`
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal([]byte(receiptBody), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Outcome != "failed" {
		t.Fatalf("receipt = %+v, want failed", receipt)
	}

	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	acme := acmeTenant(t, cfg)
	err = db.Run(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: acme}), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var domain, failed, other int
		if err := tx.DB().Raw(`SELECT count(*) FILTER (WHERE name LIKE 'task.%'),
			count(*) FILTER (WHERE name='platformkit.operation_outcome' AND operation_outcome='failed'),
			count(*) FILTER (WHERE name='platformkit.operation_outcome' AND operation_outcome IS DISTINCT FROM 'failed')
			FROM platformkit_outbox WHERE operation_ref=?`, receipt.Ref).Row().Scan(&domain, &failed, &other); err != nil {
			return err
		}
		if domain != 0 || failed != 1 || other != 0 {
			t.Errorf("refused write left task events=%d failed evidence=%d other evidence=%d", domain, failed, other)
		}
		var tasks int
		if err := tx.DB().Raw(`SELECT count(*) FROM tasks WHERE title = ''`).Row().Scan(&tasks); err != nil {
			return err
		}
		if tasks != 0 {
			t.Errorf("refused write left %d tasks", tasks)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	code, body = do(t, cfg, client, http.MethodPost, acmeHost, tenantPath, `{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s", tenantPath, code, body)
	}
	globexID := uuid.MustParse(field(t, body, "id"))
	provision(t, cfg, globexID, "root@globex.localhost")
	globex := signIn(t, cfg, globexHost, "root@globex.localhost", adminPass)
	if code, body := do(t, cfg, globex, "GET", globexHost, location, ""); code != http.StatusNotFound {
		t.Errorf("globex reads acme's receipt = %d %s, want 404", code, body)
	}
	err = db.Run(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: globexID}), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var seen int
		if err := tx.DB().Raw(`SELECT count(*) FROM platformkit_operations WHERE id = ?`, receipt.Ref).Row().Scan(&seen); err != nil {
			return err
		}
		if seen != 0 {
			t.Errorf("globex's transaction sees %d of acme's receipts", seen)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Two simultaneous sends of one keyed task creation are one operation: one task,
// one location for both answers, and the loser is a replay or an in-flight refusal.
func TestTwoSimultaneousSendsOfOneOperationCreateOneTask(t *testing.T) {
	cfg, mods, fixture := deviceComposition(t)
	install(t, fixture.path)
	start(t, cfg, mods, fixture.opts)
	client := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	type answer struct {
		status   int
		location string
		err      error
	}
	answers := make(chan answer, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			req, err := http.NewRequest("POST", "http://"+cfg.Server.Addr+tasksPath, strings.NewReader(`{"title":"one operation sent twice"}`))
			if err != nil {
				answers <- answer{err: err}
				return
			}
			req.Host = acmeHost
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(httpx.IdempotencyKeyHeader, "offline.task.twice")
			res, err := client.Do(req)
			if err != nil {
				answers <- answer{err: err}
				return
			}
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
			answers <- answer{status: res.StatusCode, location: res.Header.Get(httpx.OperationLocationHeader)}
		})
	}
	wg.Wait()
	close(answers)
	var got []answer
	for a := range answers {
		if a.err != nil {
			t.Fatal(a.err)
		}
		got = append(got, a)
	}
	created := 0
	for _, a := range got {
		switch a.status {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
		default:
			t.Errorf("send = %d, want 201 or 409", a.status)
		}
		if a.location == "" || a.location != got[0].location {
			t.Errorf("locations = %q and %q, want one", got[0].location, a.location)
		}
	}
	if created == 0 {
		t.Error("neither send was answered 201")
	}
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	err = db.Run(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: acmeTenant(t, cfg)}), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var tasks int
		if err := tx.DB().Raw(`SELECT count(*) FROM tasks WHERE title = 'one operation sent twice'`).Row().Scan(&tasks); err != nil {
			return err
		}
		if tasks != 1 {
			t.Errorf("one operation created %d tasks", tasks)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
