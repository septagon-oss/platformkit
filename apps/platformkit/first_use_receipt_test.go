package main

// FIRST-USE.md is a promise with numbers in it, so the numbers are asked of the
// receipts beside them: a row whose quoted median the receipts do not answer, a
// receipt with no failed attempt in it, a receipt whose machine is not one this
// command supports, or a captured password is refused here. The check reads the
// committed file and reaches no network: re-taking a receipt is the harness's job,
// not a check's.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type firstUseReceipt struct {
	Revision         string `json:"revision"`
	Go               string `json:"go"`
	GOOS             string `json:"goos"`
	GOARCH           string `json:"goarch"`
	FetchMS          int64  `json:"fetch_ms"`
	ColdMS           int64  `json:"cold_ms"`
	FirstActionMS    int64  `json:"first_action_ms"`
	ColdPlusFirstMS  int64  `json:"cold_plus_first_ms"`
	WarmMS           int64  `json:"warm_ms"`
	TaskPersisted    bool   `json:"task_persisted"`
	RerunReused      bool   `json:"rerun_reused"`
	PublishedVersion string `json:"published_version"`
	Attempts         []struct {
		Kind string `json:"kind"`
		MS   int64  `json:"ms"`
	} `json:"attempts"`
}

func readFirstUseReceipts(t *testing.T) (string, []firstUseReceipt) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("FIRST-USE.md"))
	if err != nil {
		t.Fatal(err)
	}
	var receipts []firstUseReceipt
	for _, line := range strings.Split(string(body), "\n") {
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var receipt firstUseReceipt
		if err := json.Unmarshal([]byte(line), &receipt); err != nil {
			t.Fatalf("a receipt is not one JSON object: %v\n%s", err, line)
		}
		receipts = append(receipts, receipt)
	}
	return string(body), receipts
}

// Every receipt names the revision it measured, the machine it measured on, and
// the attempts it cost; and nothing in the file is a password.
func TestEveryFirstUseReceiptNamesWhatItMeasured(t *testing.T) {
	t.Parallel()
	body, receipts := readFirstUseReceipts(t)
	if len(receipts) == 0 {
		t.Fatal("FIRST-USE.md quotes no receipt, so it makes an unmeasured claim")
	}
	for _, receipt := range receipts {
		if len(receipt.Revision) != 40 {
			t.Errorf("a receipt names revision %q, want the full commit it measured", receipt.Revision)
		}
		if receipt.Go == "" {
			t.Error("a receipt names no Go version, so its fetch time cannot be repeated")
		}
		if !supportedTarget(receipt.GOOS, receipt.GOARCH) {
			t.Errorf("a receipt was taken on %s/%s, which this command refuses", receipt.GOOS, receipt.GOARCH)
		}
		if !receipt.TaskPersisted || !receipt.RerunReused {
			t.Errorf("a receipt on %s/%s does not say a task was persisted and a rerun reused it: %+v",
				receipt.GOOS, receipt.GOARCH, receipt)
		}
		if len(receipt.Attempts) == 0 {
			t.Error("a receipt carries no failed attempt, so its durations describe a run nobody made twice")
		}
	}
	if quoted := regexp.MustCompile(`"password"`); quoted.MatchString(body) {
		t.Error("FIRST-USE.md carries a password field: a captured transcript got past the redactor")
	}
}

// The quoted median and p90 are re-derived from the receipts, so the sentence
// cannot go stale beside the lines it summarises.
func TestTheFirstUseQuotedFiguresAreTheReceiptsOwn(t *testing.T) {
	t.Parallel()
	body, receipts := readFirstUseReceipts(t)
	claim := regexp.MustCompile(`median (\d+) ms, p90 (\d+) ms \((\d+) receipt`)
	found := claim.FindStringSubmatch(body)
	if found == nil {
		t.Fatalf("FIRST-USE.md quotes no `median N ms, p90 N ms (N receipts)` figure to check")
	}
	total := make([]int64, 0, len(receipts))
	for _, receipt := range receipts {
		if receipt.ColdPlusFirstMS != receipt.FetchMS+receipt.ColdMS+receipt.FirstActionMS {
			t.Errorf("receipt %s: its whole does not equal its parts (%d against %d+%d+%d)",
				receipt.Revision[:8], receipt.ColdPlusFirstMS, receipt.FetchMS, receipt.ColdMS, receipt.FirstActionMS)
		}
		total = append(total, receipt.ColdPlusFirstMS)
	}
	if len(total) != len(receipts) {
		t.Fatalf("no receipt carries cold_plus_first_ms")
	}
	sort.Slice(total, func(i, j int) bool { return total[i] < total[j] })
	median := total[len(total)/2]
	if len(total)%2 == 0 {
		median = (total[len(total)/2-1] + total[len(total)/2]) / 2
	}
	p90 := total[len(total)-1]
	for _, pair := range [][2]string{{found[1], itoa(median)}, {found[2], itoa(p90)}, {found[3], itoa(int64(len(total)))}} {
		if pair[0] != pair[1] {
			t.Errorf("FIRST-USE.md quotes %s ms where the receipts answer %s ms", pair[0], pair[1])
		}
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
