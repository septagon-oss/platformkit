package main

// FIRST-USE.md is a promise with numbers in it, so the numbers are asked of the
// receipts beside them: a row whose quoted median the receipts do not answer, a
// receipt with no failed attempt in it, a receipt whose machine is not one this
// command supports, a receipt naming bytes this tree cannot find, or a captured
// password is refused here. The check reads the committed file and reaches no
// network: re-taking a receipt is scripts/first_use.sh's job, not a check's.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
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
	Artifact         string `json:"artifact"`
	Worktree         string `json:"worktree"`
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

// machine is what one summary line of the document claims and what one receipt
// belongs to: the same artifact installed on the same machine by the same Go. A
// row measured through the proxy and a row measured from this checkout are not one
// sample, so they are never in one median.
type machine struct {
	artifact, goos, goarch, goVersion string
}

func (m machine) String() string {
	return m.artifact + " on " + m.goos + "/" + m.goarch + ", Go " + m.goVersion
}

func (r firstUseReceipt) machine() machine {
	return machine{r.Artifact, r.GOOS, r.GOARCH, r.Go}
}

// firstUseClaim is one summary line of the document, in the only shape the case
// below reads: the artifact, how many samples it holds, the machine and Go it names,
// and the median, p90 and receipt count it claims.
var firstUseClaim = regexp.MustCompile(`\*\*` +
	`(worktree|published) — (\d+) sample\(s\)\*\* on (\w+)/(\w+), Go (\S+?): median (\d+) ms, p90 (\d+) ms \((\d+) receipt`)

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

// Every receipt names the revision it measured, the machine it measured on, the
// artifact it installed, and the attempts it cost; and nothing in the file is a
// password.
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
		// Which bytes were measured is part of the claim, and the two answers differ
		// in one field the claim depends on: a row that says it installed the public
		// artifact has to name what `@main` resolved to, or it points at a moving
		// branch nobody can check out again.
		switch receipt.Artifact {
		case "published":
			if receipt.PublishedVersion == "" {
				t.Errorf("a receipt on %s/%s says it measured the published artifact and names no version that @main resolved to",
					receipt.GOOS, receipt.GOARCH)
			}
		case "worktree":
			if receipt.Worktree != "clean" && receipt.Worktree != "dirty" {
				t.Errorf("a receipt on %s/%s names worktree state %q, want clean or dirty",
					receipt.GOOS, receipt.GOARCH, receipt.Worktree)
			}
		default:
			t.Errorf("a receipt on %s/%s names artifact %q, which is neither this checkout nor the published module",
				receipt.GOOS, receipt.GOARCH, receipt.Artifact)
		}
	}
	if quoted := regexp.MustCompile(`"password"`); quoted.MatchString(body) {
		t.Error("FIRST-USE.md carries a password field: a captured transcript got past the redactor")
	}
}

// The revision a receipt names is a claim about which bytes were measured, so the
// tree that quotes the receipt has to be able to find them: a receipt taken in some
// other checkout measures code this document does not describe and no reader can
// rebuild. A checkout with nothing to answer from — a shallow clone, a tarball — is
// said so rather than refused, because the receipt did not cause that.
func TestEveryReceiptNamesARevisionTheTreeThatQuotesItHolds(t *testing.T) {
	t.Parallel()
	_, receipts := readFirstUseReceipts(t)
	if len(receipts) == 0 {
		t.Fatal("FIRST-USE.md quotes no receipt, so it makes an unmeasured claim")
	}
	for _, receipt := range receipts {
		refused := git("merge-base", "--is-ancestor", receipt.Revision+"^{commit}", "HEAD")
		switch {
		case refused == nil:
		case errors.Is(refused, errGitNoHistory):
			t.Skipf("this checkout answers no reachability question over %s", receipt.Revision)
		default:
			t.Errorf("the receipt for %s/%s names revision %s, which this tree cannot reach as an ancestor of HEAD: %v",
				receipt.GOOS, receipt.GOARCH, receipt.Revision[:8], refused)
		}
	}
}

// The quoted median and p90 are re-derived from the receipts of the same artifact on
// the same machine, so a summary sentence cannot go stale beside the lines it
// summarises and a new kind of row cannot arrive without a sentence of its own.
func TestTheFirstUseQuotedFiguresAreTheReceiptsOwn(t *testing.T) {
	t.Parallel()
	body, receipts := readFirstUseReceipts(t)

	type sample struct {
		total int64
		parts int
	}
	byMachine := map[machine][]sample{}
	for _, receipt := range receipts {
		total := receipt.FetchMS + receipt.ColdMS + receipt.FirstActionMS
		if receipt.ColdPlusFirstMS != total {
			t.Errorf("receipt %s: its whole does not equal its parts (%d against %d+%d+%d)",
				receipt.Revision[:8], receipt.ColdPlusFirstMS, receipt.FetchMS, receipt.ColdMS, receipt.FirstActionMS)
		}
		m := receipt.machine()
		byMachine[m] = append(byMachine[m], sample{total: receipt.ColdPlusFirstMS, parts: len(receipt.Attempts)})
	}

	claims := map[machine]int{}
	for _, found := range firstUseClaim.FindAllStringSubmatch(body, -1) {
		m := machine{found[1], found[3], found[4], found[5]}
		claims[m]++
		quota, err := strconv.Atoi(found[2])
		if err != nil {
			t.Fatalf("a summary line counts %s samples in a way no number answers", found[2])
		}
		group := byMachine[m]
		if len(group) == 0 {
			t.Errorf("FIRST-USE.md summarises %s, which no receipt on this machine is", m)
			continue
		}
		if quota != len(group) {
			t.Errorf("FIRST-USE.md says %s was sampled %d time(s) where the file holds %d receipt(s)", m, quota, len(group))
		}
		total := make([]int64, 0, len(group))
		for _, s := range group {
			if s.parts == 0 {
				t.Errorf("%s: a receipt carries no attempt", m)
			}
			total = append(total, s.total)
		}
		sort.Slice(total, func(i, j int) bool { return total[i] < total[j] })
		median := total[len(total)/2]
		if len(total)%2 == 0 {
			median = (total[len(total)/2-1] + total[len(total)/2]) / 2
		}
		for _, pair := range [][2]string{
			{found[2], itoa(int64(len(group)))},   // samples named
			{found[6], itoa(median)},              // median
			{found[7], itoa(total[len(total)-1])}, // p90 of these samples is their maximum
			{found[8], itoa(int64(len(group)))},   // receipts named
		} {
			if pair[0] != pair[1] {
				t.Errorf("FIRST-USE.md quotes %s for %s where its own receipts answer %s", pair[0], m, pair[1])
			}
		}
	}
	if len(claims) == 0 {
		t.Fatalf("FIRST-USE.md quotes no `**artifact — N sample(s)** … median N ms, p90 N ms (N receipts)` figure to check")
	}
	for m, n := range claims {
		if n > 1 {
			t.Errorf("%s is summarised %d times, so the document holds two figures for one set of receipts", m, n)
		}
	}
	for m := range byMachine {
		if claims[m] == 0 {
			t.Errorf("%s has receipts and no summary line, so its figures are unread", m)
		}
	}
}

// errGitNoHistory is git's own answer when it has no object to ask about, as
// opposed to having the object and saying it is not an ancestor.
var errGitNoHistory = errors.New("no history to answer from")

// git runs one git command in this package's repository and reports its answer:
// nil for yes, errGitNoHistory for “nothing to ask”, the stderr for no.
func git(args ...string) error {
	command := exec.Command("git", args...)
	var out, failures bytes.Buffer
	command.Stdout, command.Stderr = &out, &failures
	if err := command.Run(); err != nil {
		if command.ProcessState.ExitCode() == 128 {
			return errGitNoHistory
		}
		return errors.New(strings.TrimSpace(failures.String()))
	}
	return nil
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
