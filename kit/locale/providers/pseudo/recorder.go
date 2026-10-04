package pseudo

import (
	"sort"
	"sync"
)

// Ask is one call to [Formatter.Text] the recorder saw.
type Ask struct {
	// Key is what the call site asked for, and Unanswered names the worklist a
	// translation task works through.
	Key string
	// Page is the document the ask happened while, named the way the gate names
	// a page ("GET /app/task/tasks/{id}"). It is empty for an ask outside any
	// page — a boot-time string, a worker's log line, a unit test.
	Page string
	// Language is the delegate's own selection, so an ask recorded while the
	// delegate was answering pt-PT says pt-PT and not the pseudo tag.
	Language string
	// Answered says whether an entry stood behind the key in that language. A
	// key answered from the text its call site passed is untranslated copy,
	// which is the only kind of key this gate can hand to a translator.
	Answered bool
}

// Recorder keeps what a render asked its formatter for. [kit/locale] promises
// concurrent selection without global state, and this holds to it: asks are safe
// from any number of goroutines, and the one thing that cannot be concurrent is
// *which page* an ask belongs to, so [Recorder.Begin] refuses a second open page
// rather than attributing asks to whichever page last asked. A gate that renders
// pages in parallel loses nothing; it renders them one at a time and serves them
// concurrently with itself.
type Recorder struct {
	mu   sync.Mutex
	page string
	open bool
	asks []Ask
}

// NewRecorder returns a recorder with no page open.
func NewRecorder() *Recorder { return new(Recorder) }

// Begin names the document the next renders belong to. Calling it while another
// page is open panics: that is a composition that would attribute a sentence to
// the wrong page, and a wrong attribution is a worse report than a red test.
func (r *Recorder) Begin(page string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.open {
		panic("pseudo: recorder is already rendering " + r.page + ", so " + page +
			" cannot open — attribute one page at a time")
	}
	r.page, r.open = page, true
}

// End closes the page. Ending one that was never opened is a caller that lost
// track of its own stack, so it panics too.
func (r *Recorder) End() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.open {
		panic("pseudo: recorder has no page open to end")
	}
	r.page, r.open = "", false
}

// ask records one call. carrier is the delegate, which can usually say whether it
// holds an entry; the delegate already answered, so the only work here is the
// decision of what to say about the answer.
func (r *Recorder) ask(carrier any, key, language, answer, fallback string, args []any) {
	ask := Ask{Key: key, Language: language,
		Answered: answered(carrier, language, key, answer, fallback, args)}
	r.mu.Lock()
	defer r.mu.Unlock()
	ask.Page = r.page
	r.asks = append(r.asks, ask)
}

// Asks returns everything recorded, in the order it was asked.
func (r *Recorder) Asks() []Ask {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Ask, len(r.asks))
	copy(out, r.asks)
	return out
}

// Unanswered returns the distinct keys a render asked for that no catalogue
// answered, in the order each was first asked. It is the worklist; the artefact
// stores only how long it is.
func (r *Recorder) Unanswered() []Ask {
	var out []Ask
	seen := map[string]bool{}
	for _, ask := range r.Asks() {
		if ask.Answered || seen[ask.Key] {
			continue
		}
		seen[ask.Key] = true
		out = append(out, ask)
	}
	return out
}

// UnansweredKeys is [Recorder.Unanswered] as the list of keys alone, sorted,
// which is what a report prints one per line.
func (r *Recorder) UnansweredKeys() []string {
	var keys []string
	for _, ask := range r.Unanswered() {
		keys = append(keys, ask.Key)
	}
	sort.Strings(keys)
	return keys
}
