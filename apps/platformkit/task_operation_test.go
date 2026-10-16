package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestTaskCreationReturnsItsOperationAndSuccessEvidence(t *testing.T) {
	cfg, mods, fixture := deviceComposition(t)
	install(t, fixture.path)
	start(t, cfg, mods, fixture.opts)
	client := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	write := func() (int, string, http.Header) {
		req, err := http.NewRequest("POST", "http://"+cfg.Server.Addr+"/api/v1/task/tasks", strings.NewReader(`{"title":"operation-bound task"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Host = acmeHost
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(httpx.IdempotencyKeyHeader, "offline.task.first-send")
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
	if status != 201 {
		t.Fatalf("create = %d %s", status, body)
	}
	location := headers.Get(httpx.OperationLocationHeader)
	if headers.Get(httpx.OperationIDHeader) != "offline.task.first-send" || location == "" {
		t.Fatalf("headers = %v", headers)
	}
	second, again, replay := write()
	if second != 201 || again != body || replay.Get(httpx.OperationLocationHeader) != location {
		t.Errorf("retry = %d %s headers %v", second, again, replay)
	}
	code, receiptBody := do(t, cfg, client, "GET", acmeHost, location, "")
	if code != 200 {
		t.Fatalf("receipt = %d %s", code, receiptBody)
	}
	var receipt struct {
		Ref      string `json:"ref"`
		Outcome  string `json:"outcome"`
		Revision int64  `json:"revision"`
	}
	if err := json.Unmarshal([]byte(receiptBody), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Outcome != "succeeded" || receipt.Revision != 2 {
		t.Fatalf("receipt = %+v", receipt)
	}
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	err = db.Run(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: acmeTenant(t, cfg)}), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var created, evidence int
		if err := tx.DB().Raw(`SELECT count(*) FILTER (WHERE name='task.task.created'), count(*) FILTER (WHERE name='platformkit.operation_outcome') FROM platformkit_outbox WHERE operation_ref=? AND operation_outcome='succeeded' AND operation_revision=2`, receipt.Ref).Row().Scan(&created, &evidence); err != nil {
			return err
		}
		if created != 1 || evidence != 1 {
			t.Errorf("created=%d evidence=%d", created, evidence)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
