package dbtest_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// initdbArgs is the environment variable the Postgres image hands to initdb, before
// a server exists. It is the settings carrier a workflow's service container has —
// such a container is created for its job from an empty data directory, so initdb
// always runs there — and the carrier a developer's Compose volume must not rely on,
// because that volume is created once and outlives the container.
const initdbArgs = "POSTGRES_INITDB_ARGS"

// namedSetting is one server setting and the value this repository starts it with.
type namedSetting struct{ setting, value string }

// serverSettings are the settings a development or test cluster of this repository
// starts with, each with the value it has to carry, and they are the whole of what
// those files may name: a service that asks for a setting outside this table is
// refused, so the levers a shared cluster pulls are decided in one reviewed place
// rather than by whoever edits one service block.
//
// The first three trade a durability no suite here can observe for the device writes
// that durability costs — nothing in this cluster is ever recovered, and the host IO
// pressure those writes cause is what parks the slowest packages of a suite in the
// middle of their own time bound. PostgreSQL names these settings for exactly this
// case in "Non-Durable Settings" (non-durability.html in its tuning chapter) and
// names the one risk left behind: an abrupt stop of the operating system, answered
// here by starting a fresh cluster rather than by recovering an old one.
// max_connections is in the table for a different reason and travels in the same
// five blocks: the suite's pool bounds are priced against one server limit, so a
// service that leaves it out, or names another number, runs the suite against a
// server its own pool math was not written for.
var serverSettings = []namedSetting{
	{"fsync", "off"},
	{"synchronous_commit", "off"},
	{"full_page_writes", "off"},
	{"max_connections", "256"},
}

// TestEveryPostgresServiceStartsWithoutDurabilityItNeverNeeds asks one relation of
// every file that starts a Postgres for this repository: the settings the stack
// starts with are the settings every continuous-integration job starts with, each
// named in the carrier that runtime actually reads.
//
// It exists because the point of these settings is that no case can observe their
// absence: a suite that lost one answers with the same green it answered with, a few
// minutes slower. A merge, a rebase or a hand edit that drops one setting out of one
// of these files therefore stays invisible until somebody reads the write counters
// again. The questions it asks, each with the failure it is there for:
//
//   - every service names each setting of serverSettings with the value that table
//     carries: the setting fell out of one file, which is what a merge of two
//     branches that each rewrote one service block does quietly;
//   - no two services name different values for one setting — the same question asked
//     of a setting this file knows nothing about, so any setting a service adds is
//     compared with every other service the moment it appears;
//   - each service carries them in the carrier its runtime reads, which differs by
//     kind and is refused per kind;
//   - every setting any service names is one of serverSettings.
func TestEveryPostgresServiceStartsWithoutDurabilityItNeverNeeds(t *testing.T) {
	services := postgresServices(t)
	// The stack alone would answer the first three questions of its own file, and the
	// fourth is the relation between the stack and the jobs. A reader that matched no
	// service would otherwise pass every assertion below by finding nothing.
	if len(services) < 2 {
		t.Fatalf("read %d Postgres services out of compose.yaml and the workflows; the stack and at least one job have to be there for this to be a relation between them", len(services))
	}

	// setting -> the value each service started with -> the services that said so.
	// The parity question is answered across services, so its answers are collected
	// as each service is read and asked once over the whole set afterwards.
	owners := map[string]map[string][]string{}
	for _, service := range services {
		t.Run(service.where, func(t *testing.T) {
			service.refuseCarrier(t)
			for _, want := range serverSettings {
				got, ok := service.carries(want.setting)
				if !ok {
					t.Errorf("starts Postgres naming no %s: this cluster then writes to a device nothing is ever recovered from, which is the IO the slowest packages of a suite wait behind", want.setting)
					continue
				}
				if got != want.value {
					t.Errorf("starts Postgres with %s=%s while this file's table says %s: the cluster this job tests on is not the cluster `make up` starts", want.setting, got, want.value)
				}
			}
			for _, named := range sortedKeys(service.carried()) {
				if !listed(serverSettings, named) {
					t.Errorf("names %s, which is not one of the settings this file carries: a server setting nobody listed is a decision about every cluster of this repository, and it belongs in that table rather than in one service block", named)
					continue
				}
				value, _ := service.carries(named)
				if owners[named] == nil {
					owners[named] = map[string][]string{}
				}
				owners[named][value] = append(owners[named][value], service.where)
			}
		})
	}

	for _, setting := range sortedKeys(owners) {
		if len(owners[setting]) < 2 {
			continue // every service that named it named the same value
		}
		t.Run("every service that names "+setting+" names the same value", func(t *testing.T) {
			t.Errorf("%s is started as %s: one cluster of this repository is tuned one way and another the other, and no suite can tell which one it ran against",
				setting, described(owners[setting]))
		})
	}
}

// postgresService is one Postgres of this repository, read out of the file that
// starts it. The two carriers keep their settings apart because which one a cluster
// answers to is the question this file has to be able to ask.
type postgresService struct {
	where     string
	inCompose bool

	command map[string]string // named on the server's own command line
	initdb  map[string]string // named in POSTGRES_INITDB_ARGS
}

func (s postgresService) carried() map[string]string {
	out := map[string]string{}
	for name, value := range s.command {
		out[name] = value
	}
	for name, value := range s.initdb {
		out[name] = value
	}
	return out
}

func (s postgresService) carries(setting string) (string, bool) {
	value, ok := s.command[setting]
	if !ok {
		value, ok = s.initdb[setting]
	}
	return value, ok
}

// refuseCarrier asks what no value can answer: whether the settings sit where this
// kind of runtime looks. An initializer argument reaches the configuration file only
// when a data directory is created, so on a cluster that already exists the image
// logs that it is skipping initialisation and the argument is ignored — which is why
// the service a developer keeps has to name its settings on the server's own command
// line, and why a job's service container names them the other way round.
func (s postgresService) refuseCarrier(t *testing.T) {
	t.Helper()
	if !s.inCompose {
		if len(s.initdb) == 0 {
			t.Errorf("gives its Postgres service no settings in %s: a service container starts from an empty data directory for its job, which is the one case the initializer arguments reach", initdbArgs)
		}
		return
	}
	for _, want := range serverSettings {
		if _, ok := s.initdb[want.setting]; ok {
			t.Errorf("compose.yaml's Postgres carries %s in %s: initdb runs once, on an empty data directory, so a container recreated onto the volume a developer already has would stop applying it without a word", want.setting, initdbArgs)
		}
	}
	if len(s.command) == 0 {
		t.Errorf("compose.yaml's Postgres names no setting on the server's own command line: that cluster outlives its container, so only a carrier that applies on every start reaches it")
	}
}

// The two file kinds spell the same two ideas differently, so they read through
// their own shape: Compose calls a service's environment `environment` and gives it
// a `command` to start the server with, and a workflow calls the same environment
// `env` and gives a service container no command at all. Services are found by
// structure rather than by line, so a re-indented file or a renamed job cannot hide
// one from this reader.
type composeService struct {
	Image       string    `yaml:"image"`
	Command     yaml.Node `yaml:"command"`
	Environment yaml.Node `yaml:"environment"`
}

type jobService struct {
	Image string    `yaml:"image"`
	Env   yaml.Node `yaml:"env"`
}

func postgresServices(t *testing.T) []postgresService {
	t.Helper()
	var out []postgresService

	var stack struct {
		Services map[string]composeService `yaml:"services"`
	}
	decode(t, "../../../compose.yaml", &stack)
	for _, name := range sortedKeys(stack.Services) {
		service := stack.Services[name]
		if !startsPostgres(service.Image) {
			continue
		}
		where := "compose.yaml's " + name + " service"
		command, err := settingsOf(words(t, &service.Command, where+"'s command"))
		if err != nil {
			t.Errorf("%s: %v", where, err)
		}
		out = append(out, postgresService{where: where, inCompose: true,
			command: command, initdb: initializerArguments(t, &service.Environment, where)})
	}

	for _, pattern := range []string{"../../../.github/workflows/*.y*ml", "../../../.gitea/workflows/*.y*ml"} {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("glob %s: %v", pattern, err)
		}
		sort.Strings(paths)
		for _, path := range paths {
			var workflow struct {
				Jobs map[string]struct {
					Services map[string]jobService `yaml:"services"`
				} `yaml:"jobs"`
			}
			decode(t, path, &workflow)
			for _, job := range sortedKeys(workflow.Jobs) {
				services := workflow.Jobs[job].Services
				for _, name := range sortedKeys(services) {
					service := services[name]
					if !startsPostgres(service.Image) {
						continue
					}
					where := strings.TrimPrefix(strings.TrimPrefix(path, "../../../"), "./") + " job " + job + " service " + name
					out = append(out, postgresService{where: where, initdb: initializerArguments(t, &service.Env, where)})
				}
			}
		}
	}
	return out
}

// initializerArguments reads POSTGRES_INITDB_ARGS out of a service's environment.
// It is one string of words, because an environment variable is what carries it and
// an environment variable is one string.
func initializerArguments(t *testing.T, env *yaml.Node, where string) map[string]string {
	t.Helper()
	args, err := settingsOf(strings.Fields(environment(t, env, where)[initdbArgs]))
	if err != nil {
		t.Errorf("%s: %v", where, err)
	}
	return args
}

// settingsOf reads the settings a server command or an initializer argument list
// names. The binary and every other word stay unread: this answers which settings
// are named, not how else the server was started. A word that asks for a setting
// without carrying one is an error rather than a word skipped, because the whole
// point of reading these files is that a setting which fell out of one is refused
// here and not discovered as a slower suite.
func settingsOf(words []string) (map[string]string, error) {
	out := map[string]string{}
	for index, word := range words {
		pair, named := settingArgument(word)
		if !named {
			continue
		}
		if pair == "" {
			if index+1 >= len(words) {
				return nil, fmt.Errorf("%s names a setting with no name=value after it", word)
			}
			pair = words[index+1]
		}
		setting, value, ok := strings.Cut(pair, "=")
		if !ok || setting == "" {
			return nil, fmt.Errorf("%s names a setting this file cannot read: it carries no name=value", pair)
		}
		out[setting] = value
	}
	return out, nil
}

// settingArgument splits one word into the name=value a setting option carries and
// whether that word named a setting at all. The server and initdb take the option
// four ways — `-c name=value`, `-cname=value`, `--set name=value` and
// `--set=name=value` — and an empty pair says the value sits in the next word.
// Everything in this repository spells it the first way; the other three are read
// because a service block re-spelled by hand, or copied out of the server's own
// documentation, is the case a silent skip would hide.
func settingArgument(word string) (pair string, named bool) {
	switch {
	case word == "-c" || word == "--set":
		return "", true
	case strings.HasPrefix(word, "-c") && len(word) > 2:
		return word[2:], true
	case strings.HasPrefix(word, "--set="):
		return word[len("--set="):], true
	default:
		return "", false
	}
}

// TestTheSettingsReaderRefusesAWordThatNamesNoSetting is the parser's own contract,
// asked without a server: the four spellings the server accepts read to the same
// setting, and the words that ask for one and carry none fail rather than read as
// the empty set. The case above this one reaches it through the five service blocks
// as they are written today, so on its own it would answer nothing about the three
// spellings none of them uses.
func TestTheSettingsReaderRefusesAWordThatNamesNoSetting(t *testing.T) {
	for _, words := range [][]string{
		{"postgres", "-c", "fsync=off"},
		{"postgres", "-cfsync=off"},
		{"initdb", "--set", "fsync=off"},
		{"initdb", "--set=fsync=off"},
	} {
		t.Run(strings.Join(words, " "), func(t *testing.T) {
			got, err := settingsOf(words)
			if err != nil {
				t.Fatalf("%v", err)
			}
			if got["fsync"] != "off" || len(got) != 1 {
				t.Errorf("read %v of %v", got, words)
			}
		})
	}
	for _, words := range [][]string{
		{"postgres", "-c"},
		{"initdb", "--set"},
		{"postgres", "-c", "fsync"},
		{"initdb", "--set", "fsync"},
	} {
		t.Run(strings.Join(words, " "), func(t *testing.T) {
			if got, err := settingsOf(words); err == nil {
				t.Errorf("read %v of %v, which names a setting with no name=value", got, words)
			}
		})
	}
}

// words reads the two shapes a command or an argument list takes in these files — a
// sequence of words, or one quoted string of them — so re-formatting a service block
// cannot hide a setting from this reader.
func words(t *testing.T, node *yaml.Node, where string) []string {
	t.Helper()
	switch node.Kind {
	case yaml.SequenceNode:
		var list []string
		if err := node.Decode(&list); err != nil {
			t.Fatalf("%s: %v", where, err)
		}
		return list
	case yaml.ScalarNode:
		var one string
		if err := node.Decode(&one); err != nil {
			t.Fatalf("%s: %v", where, err)
		}
		return strings.Fields(one)
	case 0:
		return nil
	default:
		t.Fatalf("%s is neither a list of words nor one string of them, which is no shape either Compose or a workflow takes", where)
		return nil
	}
}

// environment reads a service's environment in the mapping voice and in the
// `KEY=value` list voice, because both file kinds allow either.
func environment(t *testing.T, node *yaml.Node, where string) map[string]string {
	t.Helper()
	out := map[string]string{}
	switch node.Kind {
	case 0:
		return out
	case yaml.MappingNode:
		for pair := 0; pair+1 < len(node.Content); pair += 2 {
			out[node.Content[pair].Value] = node.Content[pair+1].Value
		}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			setting, value, ok := strings.Cut(item.Value, "=")
			if !ok {
				t.Errorf("%s: %s names no value", where, item.Value)
				continue
			}
			out[setting] = value
		}
	default:
		t.Fatalf("%s names an environment that is neither a mapping nor a list of KEY=value pairs", where)
	}
	return out
}

func decode(t *testing.T, path string, into any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := yaml.Unmarshal(raw, into); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}

func startsPostgres(image string) bool { return strings.HasPrefix(image, "postgres") }

func listed(items []namedSetting, name string) bool {
	for _, item := range items {
		if item.setting == name {
			return true
		}
	}
	return false
}

// described prints who started a setting which way, so a refusal names the file a
// person has to open rather than a value they have to go and look for.
func described(byValue map[string][]string) string {
	parts := make([]string, 0, len(byValue))
	for value, holders := range byValue {
		sort.Strings(holders)
		parts = append(parts, fmt.Sprintf("%s in %s", value, strings.Join(holders, " and ")))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", while ")
}

func sortedKeys[V any](byName map[string]V) []string {
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
