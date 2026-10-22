package main

import (
	"fmt"
	"regexp"
	"strings"
)

type budgetState struct {
	values  budget
	present bool
}
type history struct {
	repo       repository
	head, base string
	initial    budgetState
	budgets    map[string]budgetState
}

func (r repository) commit(ref string) (string, error) {
	out, err := r.git(nil, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	return strings.TrimSpace(string(out)), err
}

func uniqueBase(r repository, parents []string) (string, error) {
	args := append([]string{"merge-base", "--all", "--octopus"}, parents...)
	out, err := r.git(nil, args...)
	if err != nil {
		return "", err
	}
	bases := strings.Fields(string(out))
	if len(bases) != 1 {
		return "", fmt.Errorf("expected unique merge base for %v, got %v", parents, bases)
	}
	return bases[0], nil
}

func startHistory(r repository, exact, mergeRef string) (*history, error) {
	head, err := r.commit("HEAD")
	if err != nil {
		return nil, err
	}
	base := exact
	if mergeRef != "" {
		ref, err := r.commit(mergeRef)
		if err != nil {
			return nil, err
		}
		base, err = uniqueBase(r, []string{head, ref})
		if err != nil {
			return nil, err
		}
	} else {
		if !regexp.MustCompile(`^[0-9a-fA-F]{40}$|^[0-9a-fA-F]{64}$`).MatchString(exact) {
			return nil, fmt.Errorf("--base requires a full commit SHA")
		}
		base, err = r.commit(exact)
		if err != nil {
			return nil, err
		}
		if _, err := r.git(nil, "merge-base", "--is-ancestor", base, head); err != nil {
			return nil, fmt.Errorf("base %s is not an available ancestor of %s: %w", base, head, err)
		}
	}
	h := &history{repo: r, head: head, base: base, budgets: map[string]budgetState{}}
	h.initial, err = h.budgetAt(base)
	if err != nil {
		return nil, err
	}
	if !h.initial.present {
		files, err := r.sources(base)
		if err != nil {
			return nil, err
		}
		m, err := measure(files)
		if err != nil {
			return nil, fmt.Errorf("baseline %s: %w", base, err)
		}
		h.initial.values = m.counts
	}
	return h, nil
}

func (h *history) budgetAt(sha string) (budgetState, error) {
	if b, ok := h.budgets[sha]; ok {
		return b, nil
	}
	out, err := h.repo.git(nil, "ls-tree", "-z", sha, "--", budgetFile)
	if err != nil {
		return budgetState{}, err
	}
	b := budgetState{present: len(out) != 0}
	if b.present {
		if !strings.HasPrefix(string(out), "100644 blob ") && !strings.HasPrefix(string(out), "100755 blob ") {
			return b, fmt.Errorf("%s:%s: expected regular budget file", sha, budgetFile)
		}
		raw, err := h.repo.git(nil, "cat-file", "blob", sha+":"+budgetFile)
		if err != nil {
			return b, err
		}
		b.values, err = decodeBudget(raw)
		if err != nil {
			return b, fmt.Errorf("%s: %w", sha, err)
		}
	}
	h.budgets[sha] = b
	return b, nil
}

func (h *history) values(b budgetState) budget {
	if !b.present {
		return h.initial.values
	}
	return b.values
}

func (h *history) stable() error {
	current, err := h.repo.commit("HEAD")
	if err != nil {
		return err
	}
	if current != h.head {
		return fmt.Errorf("HEAD changed: captured %s, current %s", h.head, current)
	}
	return nil
}

func (h *history) check(working budget) ([]string, error) {
	out, err := h.repo.git(nil, "rev-list", "--reverse", "--topo-order", h.base+".."+h.head)
	if err != nil {
		return nil, err
	}
	var problems []string
	for sha := range strings.FieldsSeq(string(out)) {
		issues, err := h.transition(sha)
		if err != nil {
			return nil, err
		}
		problems = append(problems, issues...)
	}
	head, err := h.budgetAt(h.head)
	if err != nil {
		return nil, err
	}
	problems = append(problems, rises("working budget against HEAD "+h.head, h.values(head), working, "uncommitted increase; commit approval cannot be borrowed")...)
	return problems, nil
}

func rises(label string, before, after budget, reason string) []string {
	var problems []string
	for _, key := range measures {
		if after[key] > before[key] {
			problems = append(problems, fmt.Sprintf("%s: %s %d -> %d: %s", label, key, before[key], after[key], reason))
		}
	}
	return problems
}
