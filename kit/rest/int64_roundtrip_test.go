package rest_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/kit/rest"
)

func (w precisionWorld) request(method, path, body, tag, tenantHost string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://"+tenantHost+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/vnd.platformkit.int64+json")
	r.Header.Set("If-Match", tag)
	r.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	out := httptest.NewRecorder()
	w.router.ServeHTTP(out, r)
	return out
}

func TestInt64KeepsItsDigitsThroughCreateReadListUpdate(t *testing.T) {
	for _, fake := range []bool{false, true} {
		for _, quoted := range []bool{false, true} {
			t.Run(fmt.Sprintf("fake=%t/quoted=%t", fake, quoted), func(t *testing.T) {
				w := precisionFixture(t, fake)
				literal := `9007199254740993`
				if quoted {
					literal = strconv.Quote(literal)
				}
				created := w.request("POST", precisionPath, `{"title":"precision","amountCents":`+literal+`,"currency":"EUR","priority":7,"control":11}`, "", host)
				precisionStatus(t, created, 201)
				rowID := uuid.MustParse(id(t, created.Body.String()))
				assertPrecisionBody(t, created.Body.Bytes(), "9007199254740993")
				assertJavaScriptInteger(t, created.Body.Bytes())
				assertPrecisionReads(t, w, rowID, "9007199254740993")
				for i, amount := range []string{"9007199254740995", "9007199254740993"} {
					literal = amount
					if (i == 0 && !quoted) || (i == 1 && quoted) {
						literal = strconv.Quote(amount)
					}
					get := w.request("GET", precisionPath+"/"+rowID.String(), "", "", host)
					changed := w.request("PATCH", precisionPath+"/"+rowID.String(), `{"amountCents":`+literal+`}`, get.Header().Get("ETag"), host)
					precisionStatus(t, changed, 200)
					assertPrecisionBody(t, changed.Body.Bytes(), amount)
					assertPrecisionReads(t, w, rowID, amount)
				}
				if got := w.events(); !slices.Equal(got, []string{"precision.task.created", "precision.task.updated", "precision.task.updated"}) {
					t.Fatalf("events = %v", got)
				}
				assertPrecisionRefusals(t, w, rowID)
			})
		}
	}
}

func assertPrecisionBody(t *testing.T, data []byte, amount string) {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"amountCents": strconv.Quote(amount), "currency": `"EUR"`, "priority": "7", "control": "11"} {
		if string(body[key]) != want {
			t.Errorf("%s = %s, want %s; body=%s", key, body[key], want, data)
		}
	}
}

func assertPrecisionReads(t *testing.T, w precisionWorld, id uuid.UUID, amount string) {
	t.Helper()
	read := w.request("GET", precisionPath+"/"+id.String(), "", "", host)
	precisionStatus(t, read, 200)
	assertPrecisionBody(t, read.Body.Bytes(), amount)
	listed := w.request("GET", precisionPath, "", "", host)
	precisionStatus(t, listed, 200)
	var page struct {
		Items         []json.RawMessage
		Total         json.RawMessage
		Limit, Offset int
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if string(page.Total) != `"1"` || page.Limit != 50 || page.Offset != 0 || len(page.Items) != 1 {
		t.Fatalf("page = %s", listed.Body.String())
	}
	assertPrecisionBody(t, page.Items[0], amount)
	n, _ := strconv.ParseInt(amount, 10, 64)
	if row := w.read(id); row.AmountCents != n {
		t.Fatalf("stored amount = %d, want %d", row.AmountCents, n)
	}
}

func assertJavaScriptInteger(t *testing.T, data []byte) {
	t.Helper()

	command := exec.CommandContext(t.Context(), "node", "-e", `const fs=require('fs');const v=JSON.parse(fs.readFileSync(0,'utf8'));if(typeof v.amountCents!=='string'||BigInt(v.amountCents)!==9007199254740993n)process.exit(1)`)
	command.Stdin = strings.NewReader(string(data))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("JavaScript response parse: %v %s", err, output)
	}
}

func assertPrecisionRefusals(t *testing.T, w precisionWorld, id uuid.UUID) {
	t.Helper()
	world := porttest.World[precisionWorld]{T: t, Fixture: w, Events: func(w precisionWorld) []string { return w.events() }}
	snapshot := func() string { data, _ := json.Marshal(w.read(id)); return string(data) }
	path := precisionPath + "/" + id.String()
	for _, body := range []string{`{"title":"changed","amountCents":"9223372036854775808"}`, `{"title":"","amountCents":"9007199254740995"}`} {
		world.Refused(func(err error) bool { return err != nil }, porttest.Correctable, snapshot, func() error {
			got := w.request("PATCH", path, body, "", host)
			if got.Code != 422 {
				t.Errorf("refusal = %d %s", got.Code, got.Body.String())
				return nil
			}
			return errors.New("refused")
		})
	}
	for _, method := range []string{"GET", "PATCH"} {
		got := w.request(method, path, `{"amountCents":"9007199254740995"}`, "", "foreign.test")
		precisionStatus(t, got, 404)
	}
	w.actor.denied.Store(true)
	got := w.request("PATCH", path, `{"amountCents":"9007199254740995"}`, "", host)
	w.actor.denied.Store(false)
	precisionStatus(t, got, 403)
}

func TestExactObservedRevisionSettlesConcurrentWrites(t *testing.T) {
	for _, fake := range []bool{false, true} {
		t.Run(fmt.Sprintf("fake=%t", fake), func(t *testing.T) {
			w := precisionFixture(t, fake)
			created := w.request("POST", precisionPath, `{"title":"concurrent","amountCents":9007199254740993,"currency":"EUR","priority":7,"control":11}`, "", host)
			precisionStatus(t, created, 201)
			rowID := uuid.MustParse(id(t, created.Body.String()))
			w.seedRevision(rowID, 9007199254740993)
			start := make(chan struct{})
			results := make(chan int, 2)
			for range 2 {
				go func() {
					<-start
					reply := w.request("PATCH", precisionPath+"/"+rowID.String(), `{"amountCents":"9007199254740995"}`, `"9007199254740993"`, host)
					results <- reply.Code
				}()
			}
			close(start)
			codes := []int{<-results, <-results}
			slices.Sort(codes)
			if !slices.Equal(codes, []int{200, 412}) {
				t.Fatal(codes)
			}
			if row := w.read(rowID); row.Revision != 9007199254740994 || row.AmountCents != 9007199254740995 {
				t.Fatal(row)
			}
			if got := w.events(); !slices.Equal(got, []string{"precision.task.created", "precision.task.updated"}) {
				t.Fatal(got)
			}
		})
	}
}

func TestDefaultedCommandsReceiveExactIntegers(t *testing.T) {
	type input struct {
		Amount int64 `json:"amount" default:"9007199254740993"`
	}
	spec := deckSpec()
	delete(spec.Present.Deck.Directions, "left")
	var received int64
	_, router, _ := mountAs(t, spec, caller{}, func(api *httpx.API) {
		rest.Command(api.Surfaces(spec.Module), spec, "publish", "Publish", "", nil,
			func(_ context.Context, _ db.Tx[db.Tenant], _ uuid.UUID, in input) (*Task, error) {
				received = in.Amount
				return &Task{Title: "published"}, nil
			}, rest.CommandOptions{})
	})
	for body, want := range map[string]int64{
		`{}`: 9007199254740993, `{"amount":"9223372036854775807"}`: 9223372036854775807,
		`{"amount":90071992547409930e-1}`: 9007199254740993, `{"amount":0}`: 0,
	} {
		if code, out := call(t, router, "POST", "/api/v1/tasks/task/"+uuid.NewString()+"/publish", body); code != 200 || received != want {
			t.Fatalf("%s = %d %s; received %d, want %d", body, code, out, received, want)
		}
	}
}

func precisionStatus(t *testing.T, got *httptest.ResponseRecorder, want int) {
	t.Helper()
	if got.Code != want {
		t.Fatalf("status = %d, want %d: %s", got.Code, want, got.Body.String())
	}
}
