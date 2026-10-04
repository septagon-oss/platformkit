package app

// The note's tally and the job list this package registers have to agree, in both
// directions, and until now nothing in the tree kept them in step.
//
// CHANGELOG.md publishes `tenant_attributed_boundary_coverage` and names what it
// leaves out: "That tally expressly does not count the five jobs this package
// registers for itself — `outbox-relay`, `outbox-purge`, `limit-purge`,
// `idempotency-purge` and `schema-backfill` —". That sentence is prose, and prose
// rots twice over: review round 17 found one version that left the composition's own
// jobs out of the tally entirely, and this branch's fifth job (`idempotency-purge`,
// which empties `platformkit_idempotency`) made the next version false by one. The
// case below refuses both ways of going wrong, so the note can stay prose and the
// figure behind it stops being something a reader has to trust.
//
// It asks nothing of how the sentence is otherwise written: it reads the one spelled
// figure and the one enumeration of backticked names, and compares them with the list
// `work` puts on the worker. Round 19's ruling for this same published figure is that
// such a case must depend on nothing but the documents it quotes — no base, no build,
// and no job list another task may lengthen. It holds that promise by failing *whoever
// moves one side without the other*: the task that adds a kernel purge owes the
// sentence in the same change, which is the rule the review was written to enforce.

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/jobs"
)

var (
	// The count the note spells for its own jobs, and the enumeration of them that
	// follows. Both are one clause apart in the same sentence, and both are figures.
	ownFigure  = regexp.MustCompile(`(?i)\b(one|two|three|four|five|six|seven|eight|nine|ten)\s+jobs?\s+this\s+package\s+registers\s+for\s+itself`)
	ownListing = regexp.MustCompile(`(?s)registers for itself\s*—\s*(.*?)\s*—`)
	nameInTick = regexp.MustCompile("`([^`]+)`")
	numberWord = map[string]int{"one": 1, "two": 2, "three": 3, "four": 4, "five": 5,
		"six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10}
)

// scheduledByTheComposition is the job list `work` builds for the worker: the
// kernel's jobs and this package's own migration drain, which is what the tally
// excludes and what the sentence above is about.
func scheduledByTheComposition() []jobs.Job {
	return append(kernelJobs(memory.New(), ""), (&App{}).drainMigrations())
}

// TestThePublishedTallyNamesTheJobsTheCompositionRegisters: every job this package
// puts on the worker is named in the tally's exclusion, and nothing the tally names
// is a job that stopped being scheduled. The first half is the finding; the second
// is its mirror — a name left behind is a sentence about a job no process runs.
func TestThePublishedTallyNamesTheJobsTheCompositionRegisters(t *testing.T) {
	note, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatalf("read the release note: %v", err)
	}
	paragraph := ""
	for _, p := range strings.Split(string(note), "\n\n") {
		if strings.Contains(p, "boundaries the reference application registers") {
			paragraph = strings.ReplaceAll(p, "\n", " ")
			break
		}
	}
	if paragraph == "" {
		t.Fatal("the release note no longer states the tenant-attributed boundary tally whose exclusions it names")
	}

	listed := ownListing.FindStringSubmatch(paragraph)
	if listed == nil {
		t.Fatal(`the tally no longer enumerates the jobs it excludes between em dashes after "registers for itself", ` +
			`so nothing here can check it against what the composition registers`)
	}
	stated := map[string]bool{}
	for _, name := range nameInTick.FindAllStringSubmatch(listed[1], -1) {
		stated[name[1]] = true
	}
	registered := map[string]bool{}
	var order []string
	for _, j := range scheduledByTheComposition() {
		registered[j.Name] = true
		order = append(order, j.Name)
	}
	for name := range registered {
		if !stated[name] {
			t.Errorf("the composition schedules %s but the tally in CHANGELOG.md does not name it: the tally counts "+
				"what the composition registers, and its run span carries no tenant for the reason the note gives", name)
		}
	}
	for name := range stated {
		if !registered[name] {
			t.Errorf("the tally in CHANGELOG.md excludes %s, which this package no longer registers (it schedules %s)",
				name, strings.Join(order, ", "))
		}
	}

	figure := ownFigure.FindStringSubmatch(paragraph)
	if figure == nil {
		t.Fatal(`the tally no longer spells how many of its own jobs it excludes, so its sentence cannot check ` +
			`itself against the list this package registers`)
	}
	if want, ok := numberWord[strings.ToLower(figure[1])]; !ok {
		t.Fatalf("the tally spells its own exclusion %q, which is not a number this case understands", figure[1])
	} else if want != len(registered) {
		t.Errorf("the tally says it excludes the %s jobs this package registers, and the composition registers %d (%s)",
			strings.ToLower(figure[1]), len(registered), strings.Join(order, ", "))
	}
}
