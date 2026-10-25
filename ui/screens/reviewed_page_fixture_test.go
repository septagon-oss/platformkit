package screens_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reviewedPageCase is one page expectation beside a reviewed parity case: the same
// input plus the presentation states it is asked about, and the Page JSON each state
// answers. Its provenance is its own — the reviewed case's author and date stay with
// the reviewed file — and its input, minus `state`, is the reviewed input, which the
// case below checks by hash rather than by copying a digest.
type reviewedPageCase struct {
	Name       string `json:"name"`
	Provenance struct {
		Author       string `json:"author"`
		Date         string `json:"date"`
		InputHash    string `json:"inputHash"`
		ExpectedHash string `json:"expectedHash"`
	} `json:"provenance"`
	Input    json.RawMessage `json:"input"`
	Expected json.RawMessage `json:"expected"`
}

// pageExpectations is the "expected" member of a page case: one Page JSON per named
// presentation state, so Go's map order never enters a hash.
type pageExpectations struct {
	Page map[string]json.RawMessage `json:"page"`
}

// TestEveryReviewedParityCaseHasAPageExpectation is the fixture half of the Page
// contract: `Build` is only checked against a decision somebody reviewed once each
// reviewed parity case has its page expectation beside it. The reviewed files keep
// every byte — this case reads them and writes nothing.
func TestEveryReviewedParityCaseHasAPageExpectation(t *testing.T) {
	t.Parallel()
	cases, err := filepath.Glob(filepath.Join("testdata", "parity", "*.json"))
	if err != nil || len(cases) == 0 {
		t.Fatalf("no reviewed parity cases to mirror: %v", err)
	}
	for _, file := range cases {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var parity parityFixture
		if err := json.Unmarshal(raw, &parity); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if err := verifyProvenance(parity, file); err != nil {
			t.Errorf("%s: the reviewed case does not carry its own provenance: %v", file, err)
		}

		page := filepath.Join("testdata", "page", filepath.Base(file))
		pageRaw, err := os.ReadFile(page)
		if err != nil {
			t.Errorf("%s has no page expectation: %v", strings.TrimSuffix(filepath.Base(file), ".json"), err)
			continue
		}
		var want reviewedPageCase
		if err := json.Unmarshal(pageRaw, &want); err != nil {
			t.Errorf("%s: %v", page, err)
			continue
		}
		if want.Name != strings.TrimSuffix(filepath.Base(file), ".json") {
			t.Errorf("%s names its case %q: a page case is named for the parity case it is about", page, want.Name)
		}
		if want.Provenance.Author == "" || want.Provenance.Date == "" {
			t.Errorf("%s: provenance names no author or date", page)
		}
		for _, c := range []struct {
			raw  json.RawMessage
			want string
			what string
		}{{want.Input, want.Provenance.InputHash, "input"}, {want.Expected, want.Provenance.ExpectedHash, "expected"}} {
			got, err := canonicalHash(c.raw)
			if err != nil {
				t.Errorf("%s: %s: %v", page, c.what, err)
				continue
			}
			if got != c.want {
				t.Errorf("%s: %s changed without its provenance: hash is %s, provenance says %s", page, c.what, got, c.want)
			}
		}

		// The case's input is the reviewed input plus its own "state" list: the same
		// member, trimmed of it, must be the reviewed input under the canonical rule.
		var members map[string]json.RawMessage
		if err := json.Unmarshal(want.Input, &members); err != nil {
			t.Errorf("%s: input is not an object: %v", page, err)
			continue
		}
		stateRaw, hasState := members["state"]
		delete(members, "state")
		trimmed, err := json.Marshal(members)
		if err != nil {
			t.Errorf("%s: %v", page, err)
			continue
		}
		hash, err := canonicalHash(trimmed)
		if err != nil {
			t.Errorf("%s: %v", page, err)
			continue
		}
		if hash != parity.Provenance.InputHash {
			t.Errorf("%s is not built on the reviewed input: its input without \"state\" hashes %s, the reviewed case hashes %s", page, hash, parity.Provenance.InputHash)
		}

		if !hasState {
			t.Errorf("%s carries no \"state\" list, so it asks about no page", page)
			continue
		}
		var states []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(stateRaw, &states); err != nil {
			t.Errorf("%s: its \"state\" member is not a list of states: %v", page, err)
			continue
		}
		if len(states) == 0 {
			t.Errorf("%s states no presentation state, so it expects no page", page)
		}
		var expectations pageExpectations
		if err := json.Unmarshal(want.Expected, &expectations); err != nil {
			t.Errorf("%s: expected is not a page expectation: %v", page, err)
			continue
		}
		for _, s := range states {
			if s.Name == "" {
				t.Errorf("%s: a state carries no name to key its expectation by", page)
				continue
			}
			if _, ok := expectations.Page[s.Name]; !ok {
				t.Errorf("%s: the state %q has no expectation in expected.page", page, s.Name)
			}
		}
	}
}

// withoutState is a page case's input with its "state" member removed: what must equal
// the reviewed parity case's own input.
func withoutState(pageRaw []byte) any {
	var in map[string]any
	if err := json.Unmarshal(pageRaw, &in); err != nil {
		return nil
	}
	input, _ := in["input"].(map[string]any)
	delete(input, "state")
	return input
}

// stateMember is the page case's "state" list, or a null the caller refuses.
func stateMember(pageRaw []byte) json.RawMessage {
	var in struct {
		State json.RawMessage `json:"input"`
	}
	_ = json.Unmarshal(pageRaw, &in)
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(in.State, &wrapper); err != nil {
		return json.RawMessage("null")
	}
	if state, ok := wrapper["state"]; ok {
		return state
	}
	return json.RawMessage("null")
}
