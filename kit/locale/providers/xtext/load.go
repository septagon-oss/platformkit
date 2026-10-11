package xtext

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/septagon-oss/platformkit/kit/locale"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/message/catalog"
)

// Load reads catalogues from files and merges them into one provider
// (ADR 0012). A Source is one owner's copy — a kernel layer's, a module's, a
// product's, a client's — and the argument order is the merge order: a later
// source answers for a key an earlier one also carries, except where an
// earlier source owns that key.
//
// Files are gotext JSON, one per locale, named `<locale>.json` at the root of
// the source's fs.FS, so a catalogue is a directory of files somebody can
// diff, edit and translate without compiling anything:
//
//	"screens.new": {"references": ["ui/screens/screens.go"],
//	                "translation": "New %s",
//	                "plural": {"": "New %s"}}
//
// `translation` is what a reader takes and `plural` is what a plural-aware
// reader takes; a message with no plural variants spells that single form as
// the empty string. Load reads `translation` and requires `plural` to say the
// same thing, because a catalogue whose two spellings disagree answers
// differently to two different readers. `references` and every other field
// belong to the format and are left alone.
//
// fallback names the source language: the language the call sites are written
// in, which is the language `Formatter.Text` answers in from the readable text
// a caller passes when no entry stands behind a key. It is therefore the one
// locale that needs no file — a key with no entry there is untranslated, not
// missing, which is how gettext treats the text in the code. Every other
// locale a source ships must answer for every key that source ships in any of
// them, and must ask for the same arguments its siblings do — the same positions and
// conversions, in whatever order its own sentence reads them, since `%[2]s` exists so a
// translation can put the second argument first. Both conditions are
// refused here, at composition, rather than as a page that quietly answers in
// English. The first is each source's own — a layer may ship one locale and a
// product one key, which is what the merge is for — and the second is the
// merged catalogue's, because a re-wording that lands over a sentence is read
// by every language an earlier source wrote.
//
// The provider reads no process-global catalogue and no environment variable:
// everything it knows came through the fs.FS it was handed.
func Load(fallback string, sources ...Source) Catalog {
	fallbackTag, err := language.Parse(fallback)
	if err != nil || strings.TrimSpace(fallback) == "" {
		panic(fmt.Sprintf("xtext: %q is not a language Load can fall back to", fallback))
	}
	if len(sources) == 0 {
		panic("xtext: localization needs at least one catalog source")
	}

	// tags is the set a request may be answered in: every language a file named,
	// and the source language too, whether or not a source shipped a file for it.
	tags := []language.Tag{fallbackTag}
	byTag := map[language.Tag]map[string]string{}
	// merged and wroteBy hold the same copy keyed by the tag spelled out, and
	// remember which source wrote it. The argument rule below is the composed
	// catalogue's, so it needs the merged view and the names to say the refusal with.
	merged := map[string]map[string]string{}
	wroteBy := map[string]map[string]string{}
	var owned []ownedKey
	for _, source := range sources {
		entries := read(source, fallback)
		// A source owns its prefixes against everyone who comes after it, so the
		// guard is installed once its own files have been read, and the keys read
		// from those files are checked against the earlier sources only.
		for _, prefix := range source.Owns {
			owned = append(owned, ownedKey{prefix: prefix, owner: source.Name})
		}
		for name, messages := range entries {
			tag, err := language.Parse(name)
			if err != nil {
				panic(fmt.Sprintf("xtext: %s/%s.json is not a supported language tag: %v", source.Name, name, err))
			}
			if !slices.Contains(tags, tag) {
				tags = append(tags, tag)
			}
			if byTag[tag] == nil {
				byTag[tag] = map[string]string{}
				merged[tag.String()] = map[string]string{}
				wroteBy[tag.String()] = map[string]string{}
			}
			for key, text := range messages {
				if guard := protected(key, owned[:len(owned)-len(source.Owns)]); guard != "" {
					panic(fmt.Sprintf("xtext: %s/%s.json carries %q, which %s owns and no later catalogue may answer for",
						source.Name, name, key, guard))
				}
				byTag[tag][key] = text
				merged[tag.String()][key] = text
				wroteBy[tag.String()][key] = source.Name
			}
		}
	}
	checkMergedVerbs(fallbackTag.String(), merged, wroteBy)

	messages := catalog.NewBuilder(catalog.Fallback(fallbackTag))
	for tag, keys := range byTag {
		for key, text := range keys {
			if err := messages.SetString(tag, key, text); err != nil {
				panic(fmt.Sprintf("xtext: %s cannot carry %q: %v", tag, key, err))
			}
		}
	}
	return &files{messages: messages, tags: tags, fallback: fallbackTag,
		matcher: language.NewMatcher(tags)}
}

// Source is one owner's catalogues, as files, plus the key prefixes that
// catalogue is authoritative for. A later source may re-word a key it does not
// own; it may not answer for one it does, because the sentence a kernel refusal
// speaks is the kernel's and not the composition's.
// Catalog is what Load composed: a selection over the languages the files named.
// The set is readable because the composition that built a catalogue is the one
// place that knows which languages exist at all — and it is the composition that
// tells a new tenant which of them it can be served in, through modules/tenant's
// Deps, rather than a module guessing from a file it cannot see.
type Catalog interface {
	locale.Messages
	// Languages are the languages a request to this catalogue may be answered in,
	// the source language among them whether or not a file carried it.
	Languages() []string
}

type Source struct {
	// FS holds `<locale>.json` at its root — an embed.FS of a module's messages
	// directory, an os.DirFS, a testfs.Map.
	FS fs.FS
	// Name is what a refusal calls this catalogue by. It is not stored: it only
	// ever appears in the panic that refuses the catalogue, which is the one
	// thing a reader of a boot failure needs to know.
	Name string
	// Owns lists the key prefixes no later source may write.
	Owns []string
}

// ownedKey is one declared prefix and the source that declared it.
type ownedKey struct{ prefix, owner string }

// protected is whose key this is, or "" when any source read so far may answer
// for it.
func protected(key string, owned []ownedKey) string {
	for _, o := range owned {
		if strings.HasPrefix(key, o.prefix) {
			return o.owner
		}
	}
	return ""
}

// entry is one message as gotext JSON spells it. Unknown fields belong to the
// format (extraction comments, obsolete entries) and are left alone.
type entry struct {
	Translation string            `json:"translation"`
	Plurals     map[string]string `json:"plural"`
	References  []string          `json:"references"`
}

// read loads one source's files, refusing the catalogues that would make a
// rendered page lie about what it says.
func read(source Source, fallback string) map[string]map[string]string {
	if source.FS == nil {
		panic(fmt.Sprintf("xtext: %s is a catalog source with no files", source.Name))
	}
	paths, err := fs.Glob(source.FS, "*.json")
	if err != nil {
		panic(fmt.Sprintf("xtext: %s: catalogues cannot be listed: %v", source.Name, err))
	}
	if len(paths) == 0 {
		panic(fmt.Sprintf("xtext: %s carries no <locale>.json catalogues", source.Name))
	}
	sort.Strings(paths)
	entries := map[string]map[string]string{}
	for _, path := range paths {
		name := strings.TrimSuffix(path, ".json")
		if _, err := language.Parse(name); err != nil {
			panic(fmt.Sprintf("xtext: %s/%s is not a supported language tag: %v", source.Name, path, err))
		}
		body, err := fs.ReadFile(source.FS, path)
		if err != nil {
			panic(fmt.Sprintf("xtext: %s/%s cannot be read: %v", source.Name, path, err))
		}
		var file map[string]entry
		if err := json.Unmarshal(body, &file); err != nil {
			panic(fmt.Sprintf("xtext: %s/%s is not gotext JSON: %v", source.Name, path, err))
		}
		messages := make(map[string]string, len(file))
		for key, message := range file {
			if strings.Contains(key, "#") {
				panic(fmt.Sprintf("xtext: %s/%s carries %q: a plural key needs a selector this provider does not compose, and a form that is silently never chosen is worse than a boot that refuses it",
					source.Name, path, key))
			}
			if strings.TrimSpace(message.Translation) == "" {
				panic(fmt.Sprintf("xtext: %s/%s has no copy for %s", source.Name, path, key))
			}
			if form, spelled := message.Plurals[""]; len(message.Plurals) > 0 && (!spelled || strings.TrimSpace(form) != message.Translation) {
				panic(fmt.Sprintf("xtext: %s/%s says two different things about %s: the plural form and the translation must be the one copy a reader gets",
					source.Name, path, key))
			}
			messages[key] = message.Translation
		}
		entries[name] = messages
	}
	checkParity(source.Name, fallback, entries)
	return entries
}

// checkMergedVerbs holds the composed catalogue — not one source's files — to the
// half of the parity rule that only the merge can see: every copy of one key, in
// whichever language and whichever source, interpolates the same arguments in the
// same order.
//
// checkParity cannot see it because it reads one source and the merge spans them.
// A source that ships only a translation has nothing to disagree with, and a later
// source whose only file is the source language is exempt from presence, so each is
// legal alone; merged, they leave the product's English sentence asking for two
// arguments beside the module's Portuguese one asking for one, and the page a person
// is reading prints Go's own mismatch marker in their language. The same fault travels
// the other way — a source-language sentence that interpolates nothing merged over a
// translation that asks for one — and is refused on the same ground. Presence is not
// checked here and that is deliberate: a layer shipping one locale and a product
// re-wording one key in another are what the merge exists for, and requiring every
// source to answer for every key would refuse the brief's own override.
func checkMergedVerbs(fallback string, merged, wroteBy map[string]map[string]string) {
	var locales []string
	for tag := range merged {
		if tag != fallback {
			locales = append(locales, tag)
		}
	}
	sort.Strings(locales)
	var keys []string
	for _, copy := range merged {
		for key := range copy {
			if !slices.Contains(keys, key) {
				keys = append(keys, key)
			}
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		want, from := "", fallback
		// `known`, and not an empty `want`, says a baseline was read. A source-language
		// sentence that interpolates nothing ("Delete") is a baseline of zero arguments,
		// not the absence of one: the call site passes what that English sentence asks
		// for, so a copy merged under it which asks for one is the same fault travelling
		// the other way — the page is short of an argument instead of short of a word.
		known := false
		if source, carried := merged[fallback][key]; carried {
			want, known = verbs(source), true
		}
		for _, tag := range locales {
			text, carried := merged[tag][key]
			if !carried {
				continue
			}
			if !known {
				want, from, known = verbs(text), tag, true
				continue
			}
			if got := verbs(text); got != want {
				panic(fmt.Sprintf("xtext: %s/%s.json and %s/%s.json disagree about the arguments of %q: one copy asks for %q, the other for %q. A copy merged over another changes the words, not the arguments",
					wroteBy[from][key], from, wroteBy[tag][key], tag, key, want, got))
			}
		}
	}
}

// checkParity holds one source to the two conditions that make a catalogue
// answerable in the language it claims: every locale beyond the source language
// answers for the same keys, and no copy of a key asks for different arguments
// than another copy of it.
//
// The source language is exempt from the first condition because its copy is
// the text the call site passes as the fallback — an entry there is only worth
// writing where the shell's own wording differs from the sentence the code
// author wrote, which is why it is checked for arguments and never for
// presence.
func checkParity(name, fallback string, entries map[string]map[string]string) {
	var locales []string
	for locale := range entries {
		if locale != fallback {
			locales = append(locales, locale)
		}
	}
	sort.Strings(locales)
	if len(locales) < 1 {
		return
	}
	// Every key the source wrote a copy for, in any of its files, including the
	// source language's own: a key the shell re-words in English and leaves
	// untranslated is the same hole as one it translates and leaves untranslated
	// in English, because either way one of the two pages is not the copy anyone
	// wrote on purpose.
	var keys []string
	for _, locale := range locales {
		for key := range entries[locale] {
			if !slices.Contains(keys, key) {
				keys = append(keys, key)
			}
		}
	}
	for key := range entries[fallback] {
		if !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		// The copy of record for a key's arguments is the source language's when
		// the source states its own wording there, and the first locale that
		// carries the key otherwise.
		want, from := "", fallback
		if source, ok := entries[fallback][key]; ok {
			want = verbs(source)
		}
		for _, locale := range locales {
			text, carried := entries[locale][key]
			if !carried {
				panic(fmt.Sprintf("xtext: %s/%s.json has no copy for %q, which another of this source's catalogues answers for",
					name, locale, key))
			}
			if want == "" {
				want, from = verbs(text), locale
				continue
			}
			if got := verbs(text); got != want {
				panic(fmt.Sprintf("xtext: %s/%s.json says %q about %q where %s.json says %q: a copy that changes the arguments leaves the sentence short of one",
					name, locale, got, key, from, want))
			}
		}
	}
}

// verbs is the arguments a copy asks fmt for, each spelled with the position it
// fills: `Novo %s para %s` and `New %[1]s for %[2]s` are both `"1:%s 2:%s"`, and
// `Novo %[2]s para %[1]s` is the same pair again, because the two sentences ask for
// the same two arguments and differ only in which one they read first.
//
// Two copies of one key are rendered with the same arguments — the call site is one
// line of Go and every language reads it — so what the checks compare is that pair
// of (position, conversion) lists and not the text of the verbs. Position is part of
// the answer because a copy may name its arguments instead of taking them in order:
// `%[2]s` is Go's only way to say "put the second argument first", and re-ordering
// is the commonest thing a translated sentence has to do to the one it translates.
// Reading the verbs alone would call `New %[1]s for %[2]s` and `New %[2]s for %[1]s`
// the same composition when they print the opposite pairs, and — the other way round
// — would refuse a re-ordering that changed nothing a caller has to supply. The
// sorted list is what makes the comparison a set of positions rather than a spelling.
//
// An unnumbered verb takes the next position, which is fmt's own rule and the only
// reading that keeps the two halves of one copy comparable: `Novo %s para %[2]s` puts
// its plain verb at 1 and its numbered one at 2, as fmt will when it renders it. A
// copy that numbers one argument and leaves another plain past it is fmt's error to
// report at the page, not this guard's to guess about.
//
// A doubled percent is how a gettext copy spells one literal percent sign, and it
// is one character to the reader of the sentence: the pair is stepped over rather
// than scanned, because restarting inside it turns the space and letter that
// follow into a verb — and `%%` is the only advice a translator can be given for
// printing a percent sign, so a scan that charged them for taking it would refuse
// two copies that interpolate the same argument in the same order, and point at
// neither. A lone percent followed by a space and a letter is still a conversion
// (`Save 20% on %d items` really does ask fmt for an octal argument), and still
// refused: this removes escaped pairs, nothing else.
func verbs(copy string) string {
	asked := strings.ReplaceAll(copy, "%%", "")
	var slots []string
	next := 1
	for _, found := range verbPattern.FindAllStringSubmatch(asked, -1) {
		at, verb := strings.Trim(found[1], "[]"), found[2]
		if at == "" {
			at = strconv.Itoa(next)
		}
		if position, err := strconv.Atoi(at); err == nil {
			next = position + 1
		}
		slots = append(slots, at+":"+verb)
	}
	sort.Slice(slots, func(i, j int) bool {
		lat, lverb, _ := strings.Cut(slots[i], ":")
		rat, rverb, _ := strings.Cut(slots[j], ":")
		lpos, _ := strconv.Atoi(lat)
		rpos, _ := strconv.Atoi(rat)
		if lpos != rpos {
			return lpos < rpos
		}
		return lverb < rverb
	})
	return strings.Join(slots, " ")
}

var verbPattern = regexp.MustCompile(`%(\[[0-9]+\])?([-+# 0]*[0-9.]*[a-zA-Z])`)

// files is the provider Load composed: the merged copy, and the languages the
// files named — the set a request may be answered in, which includes the source
// language whether or not a source shipped a file for it.
type files struct {
	messages *catalog.Builder
	tags     []language.Tag
	fallback language.Tag
	matcher  language.Matcher
}

// Languages is the set a request may be answered in, spelled as tags. A copy is
// returned: a caller that sorted or appended to it would be editing the catalogue.
func (f *files) Languages() []string {
	out := make([]string, 0, len(f.tags))
	for _, tag := range f.tags {
		out = append(out, tag.String())
	}
	return out
}

// Select answers from the languages the files declared rather than from the
// languages that happen to hold copy, which is what lets a catalogue ship only
// translations: a preference for the source language selects it, and every key
// then answers from the text its call site passes. An unsupported preference
// gets the source language, which always answers.
func (f *files) Select(preferences ...string) locale.Locale {
	wanted := make([]string, 0, len(preferences))
	for _, p := range preferences {
		if p != "" {
			wanted = append(wanted, p)
		}
	}
	tag, index := language.MatchStrings(f.matcher, wanted...)
	if index < 0 {
		tag, index = f.fallback, slices.Index(f.tags, f.fallback)
	}
	return locale.Locale{Language: f.tags[index].String(),
		Formatter: catalogFormatter{message.NewPrinter(tag, message.Catalog(f.messages))}}
}
