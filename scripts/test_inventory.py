#!/usr/bin/env python3
"""The test inventory: derived from the tree, checked against the committed table (decision 0088).

    tests/inventory.json  one row per test — the machine-readable table CI re-checks
    tests/INVENTORY.md    the same figures as a table a person ratifies, quoting the JSON

Three modes, one implementation of each decision behind a figure:

  --check          refuse a test with no row, a row with no test, a verdict outside the four,
                   a `merge` naming a file that is not in the tree, a `delete` beside an
                   unmeasured coverage column, a round-named file count above its ceiling, a
                   summary that disagrees with the rows it summarises, and a figure the markdown
                   quotes that the JSON no longer answers. Exit 1, with every refusal named.
  --write          refresh the mechanical columns and propose rows for tests that have none.
                   A verdict is a human column: --write never rewrites one, so re-running it on
                   an unchanged tree writes nothing. `--write --baseline <rev>` records what the
                   measured columns were measured against.
  --tier NAME      print the Go packages the named tier runs, for the packages a diff reaches
                   (--base <ref>) or every package (--base '' is every package). The push tier
                   refuses a selection that opens a stack: its promise is "no database", and no
                   flag opens that door.

The exit status is part of the answer, not a courtesy: 0 for "the tree answers the table", 1 for a
refused --check, 2 for a --ceiling that would rise, 3 for a push selection that opens a stack, 4 for
a tool the selector could not ask (a failed `git diff` or `go list`, which is not the same empty
answer as "the diff reaches nothing"). scripts/check_test_inventory.sh execs it and
scripts/check_push_tier.sh substitutes it, so both inherit these numbers verbatim.

Columns split by who may fill them:

  mechanical  file, pkg, name, kind, area, layer, tier, needs_db, round_named — recomputed from
             the tree on every --write, because the tree answers them.
  measured    unique_reach, unique_reach_kind, unique_lines, and the summary's job/packagewide
             CI figures — a measurement of a named revision, carried forward until somebody
             measures again. A row with no measurement is a keep; nothing here deletes on a
             guess, so `unique_lines: null` is refused beside a `delete` rather than read as zero.
  human       verdict, merge_into, rename_to, rule, rule_present, evidence — root ratifies these;
             --write fills them only for a row that did not exist.
"""
from __future__ import annotations

import argparse
import collections
import json
import os
import re
import subprocess
import sys

MODULE = "github.com/septagon-oss/platformkit"
SCHEMA = "platformkit.test-inventory.v1"
VERDICTS = ("keep", "keep-rewrite", "merge", "delete")
LAYERS = ("contract", "behaviour", "composition", "journey")
TIERS = ("push", "merge", "nightly")

# A file is named for a round when its own basename says so: 0088 rule 2 acts on the files a
# review round left behind, and on nothing else.
ROUND_PREFIX = re.compile(r"^(review|round\d*|probe)")
GO_FUNC = re.compile(r"^func\s+(Test|Benchmark|Fuzz|Example)([A-Za-z0-9_]*)\s*\(", re.M)
GO_IMPORT = re.compile(r'^\s*(?:(\w+)\s+)?"' + re.escape(MODULE) + r'/([\w./-]+)"', re.M)
GO_SELECT = re.compile(r"\b([a-z]\w*)\.([A-Z]\w*)")
GO_CALL = re.compile(r"\b([A-Z][A-Za-z0-9_]+)\s*[({]")
GO_DECL = re.compile(r"^\s*(?:func|type|var|const)\s+([A-Z][A-Za-z0-9_]*)", re.M)
GO_METHOD = re.compile(r"^\s*func\s+\([^)]*\)\s+([A-Z][A-Za-z0-9_]*)\s*\(", re.M)
ROUTE = re.compile(r'"(/[a-z0-9][\w/{}.-]*)["?]')
HTTP_SHAPE = re.compile(r"\bdo\(t,|httptest\.|http\.Method(Get|Post|Put|Delete|Patch)\b")
# A file judges something when it calls a Go assertion, an assert/require helper, the conformance
# suite, or carries an Example `// Output:` block the go command itself checks. Counting only
# t.Errorf/t.Fatalf calls a TestMain harness, a benchmark, a fixture and a golden all "assert-free".
JUDGES = re.compile(r"\bt\.(?:Errorf?|Fatalf?|FailNow|Fail|Skipf?|Fatal)\b|\b(?:assert|require|qt)\.|\bexpect\(|"
                    r"//\s*Output:|\bconformance\.|\bRun[A-Za-z]*\(t,|\b[a-z]\w*\(t,")
HARNESS = re.compile(r"\bfunc TestMain\b|^func Benchmark|^func Fuzz|^func Example", re.M)
CONTRACT_KEY = ("openapi", "asyncapi", "apidiff", "public-api", "golden", "wire_compatibility", "mobile_flows", "openapi.json")

root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
root_dir = root


def git(*args):
    return subprocess.run(("git", *args), cwd=root, capture_output=True, text=True).stdout.splitlines()


def read(rel):
    try:
        with open(os.path.join(root, rel), encoding="utf-8", errors="replace") as fh:
            return fh.read()
    except OSError:
        return ""


def tracked(*patterns):
    return sorted(p for p in git("ls-files", "--", *patterns) if p)


def round_named(path):
    return bool(ROUND_PREFIX.match(path.rsplit("/", 1)[-1]))


def layer_of(path):
    low = path.lower()
    if any(k in low for k in CONTRACT_KEY) or "testdata" in low:
        return "contract"
    if path.startswith("e2e/"):
        return "journey"
    if path.startswith("apps/"):
        return "composition"
    return "behaviour"


def tier_of(path, needs_stack):
    """The push tier's promise is that it opens no stack, so stack need — not the layer's name —
    decides the tier. Naming the tier by layer would schedule 1 427 database-opening cases into a
    tier that must finish on a laptop."""
    if path.startswith("e2e/"):
        return "nightly"
    if "scripts/" in path:
        return "push"
    if any(k in path.lower() for k in CONTRACT_KEY):
        return "push"
    return "merge" if needs_stack else "push"


def stack_packages():
    """Packages whose test binary opens a store. One binary per package, so one file that opens
    Postgres puts every case beside it behind a database: need is a package fact, not a file fact."""
    db = subprocess.run(("git", "grep", "-l", "-e", "dbtest", "-e", "PLATFORMKIT_TEST_DATABASE_URL",
                         "--", "*_test.go"), cwd=root, capture_output=True, text=True)
    stack = subprocess.run(("git", "grep", "-l", "-e", "PLATFORMKIT_TEST_NATS", "-e", "PLATFORMKIT_TEST_VALKEY",
                            "-e", "PLATFORMKIT_TEST_OBJECT", "-e", "mailpit", "--", "*_test.go"),
                           cwd=root, capture_output=True, text=True)
    dirs = {os.path.dirname(p) for p in db.stdout.split()} | {os.path.dirname(p) for p in stack.stdout.split()}
    return {d.lstrip("./") for d in dirs if d}


def symbol_index():
    """dir -> exported names that package declares. A name is only evidence when the package that
    declares it is a first-party one; `New` in a hundred places proves nothing about which."""
    idx = collections.defaultdict(set)
    for p in tracked("*.go"):
        if p.endswith("_test.go"):
            continue
        text = read(p)
        d = os.path.dirname(p)
        for pattern in (GO_DECL, GO_METHOD):
            for m in pattern.finditer(text):
                idx[d].add(m.group(1))
    return {k: v for k, v in idx.items() if len(k) > 3}


def surface_of(path, text, sym):
    """Production call sites this file reaches: `<pkg>.<Symbol>` where the import resolves to a
    first-party package that declares Symbol, plus same-package `Symbol(` calls it declares."""
    d = os.path.dirname(path)
    alias = {}
    for m in GO_IMPORT.finditer(text):
        alias[m.group(1) or m.group(2).rsplit("/", 1)[-1]] = m.group(2)
    out = set()
    for m in GO_SELECT.finditer(text):
        pkgdir = alias.get(m.group(1))
        if pkgdir and m.group(2) in sym.get(pkgdir, ()):
            out.add(pkgdir + "." + m.group(2))
    for m in GO_CALL.finditer(text):
        if m.group(1) in sym.get(d, ()):
            out.add(os.path.basename(d) + "." + m.group(1))
    return out


def routes_of(text):
    return {m.group(1) for m in ROUTE.finditer(text) if len(m.group(1)) > 2 and not m.group(1).startswith("//")}


def proposal(path, text, rows_of_package, sym, needs_stack):
    """Every column a machine may fill, for a file the tree holds and the table does not.

    The verdict rule is 0088's, declared here so a reader can disagree with the rule and not the
    mood: a file named for a round that reaches something no kept sibling reaches is the only proof
    of that reach (keep-rewrite, renamed for its rule per 0072); one that reaches nothing new is a
    merge whose target the kept sibling names; anything not named for a round is a keep.
    """
    rnd = round_named(path)
    is_go = path.endswith("_test.go")
    http = bool(HTTP_SHAPE.search(text))
    surface, routes = set(), set()
    if is_go:
        surface, routes = surface_of(path, text, sym), routes_of(text)
    unique = []
    if rnd and is_go:
        siblings = set()
        for other in rows_of_package:
            if other["file"] != path and not other["round_named"]:
                siblings |= set(other.get("_surface") or ()) | (set(other.get("_routes") or ()) if http else set())
        mine = surface if not http else routes
        unique = sorted(mine - siblings)
    funcs = [m.group(1) + m.group(2) for m in GO_FUNC.finditer(text)]
    asserts = len(JUDGES.findall(text))
    if not is_go:
        verdict, target, evidence = ("keep-rewrite", "", "named for a round") if rnd else ("keep", "", "not-round-named")
    elif not rnd:
        verdict, target, evidence = "keep", "", "not-round-named"
    elif unique:
        verdict, target, evidence = "keep-rewrite", "", "only file in its package reaching " + ", ".join(unique[:3])
    elif asserts == 0 and not (JUDGES.search(text) or HARNESS.search(text)):
        verdict, target, evidence = "delete", "", "no test function, or nothing judges anything in it"
    else:
        verdict, target, evidence = "merge", "", "round-named and reaches nothing a kept sibling does not reach"
    row = {
        "area": os.path.dirname(path).split("/")[0],
        "evidence": evidence,
        "file": path,
        "id": path + "#" + (funcs[0] if funcs else os.path.basename(path)),
        "kind": "go" if is_go else kind_of(path),
        "layer": layer_of(path),
        "name": funcs[0] if funcs else os.path.basename(path),
        "needs_db": os.path.dirname(path) in needs_stack,
        "pkg": os.path.dirname(path),
        "round_named": rnd,
        "rule": "",
        "rule_present": False,
        "tier": tier_of(path, os.path.dirname(path) in needs_stack),
        "unique_lines": None,
        "unique_reach": unique[:6],
        "unique_reach_kind": ("routes" if http else "symbols") if is_go else "",
        "verdict": verdict,
        "merge_into": target,
        "_surface": sorted(surface),
        "_routes": sorted(routes),
    }
    if rnd:
        row.pop("rule_present", None)   # a round-named row's `rule` column is the sentence; the flag is a keep's
        row["assertions_in_file"] = asserts
        if verdict == "keep-rewrite":
            row["rename_to"] = rename_for(row["name"])
    else:
        row.pop("merge_into", None)      # a row with nothing to merge into says nothing about one
    return row


def kind_of(path):
    if path.endswith(".ts"):
        return "playwright"
    if path.endswith(".yaml"):
        return "maestro"
    return "shell_pin"


def walk():
    """Every test in the tree, in the order a reader would list them."""
    needs_stack = stack_packages()
    rows = []
    for p in tracked("*_test.go"):
        text = read(p)
        d = os.path.dirname(p)
        funcs = [m.group(1) + m.group(2) for m in GO_FUNC.finditer(text)]
        base = proposal(p, text, [], {}, needs_stack)
        base["funcs"] = funcs
        rows.append(base)
    sym = symbol_index()
    by_pkg = collections.defaultdict(list)
    for r in rows:
        by_pkg[r["pkg"]].append(r)
    for r in rows:  # second pass: a sibling's reach is only known once every sibling is walked
        fresh = proposal(r["file"], read(r["file"]), by_pkg[r["pkg"]], sym, needs_stack)
        r.update(fresh)
    for p in tracked("e2e/*.spec.ts", "e2e/**/*.spec.ts"):
        title = re.search(r"^(?:test|test\.describe)\(\s*['\"`]([^'\"`]+)", read(p), re.M)
        rows.append(proposal_named(p, "e2e", "journey", "nightly", title.group(1) if title else p, True))
    for p in tracked("e2e/maestro/*.yaml"):
        rows.append(proposal_named(p, "e2e/maestro", "journey", "nightly", os.path.basename(p), True))
    for p in tracked("scripts/*_test.sh", "scripts/*_test.py"):
        rows.append(proposal_named(p, "scripts", "composition", "push", os.path.basename(p), False))
    out = []
    for r in rows:
        names = r.pop("funcs", None)
        if names is None:            # a journey or a pin script: the file is the test
            out.append(r)
            continue
        for name in names:            # a Go file is one row per Test/Benchmark/Fuzz/Example
            row = dict(r)
            row["name"] = name
            row["id"] = r["file"] + "#" + name
            out.append(row)
    return out


def proposal_named(path, pkg, layer, tier, name, needs_stack):
    """One row for one file: a Playwright spec, a Maestro flow and a `scripts/` pin script are each
    the whole test, so the file's own path is its id, as `file#TestName` is a Go function's."""
    rnd = round_named(path)
    return {
        "area": path.split("/")[0], "evidence": "named for a round" if rnd else "not-round-named",
        "file": path, "id": path, "kind": kind_of(path), "layer": layer, "name": name,
        "needs_db": needs_stack, "pkg": pkg, "round_named": rnd, "rule": "", "rule_present": False,
        "tier": tier, "unique_lines": None, "unique_reach": [], "unique_reach_kind": "",
        "verdict": "keep-rewrite" if rnd else "keep", "merge_into": "",
    }


# Recomputed from the tree on every --write. Everything else on a row is a measurement or a
# verdict, and --write carries it: a tool may not silently re-ratify what a person decided.
MECHANICAL = ("area", "file", "kind", "layer", "name", "needs_db", "pkg", "round_named", "tier")


def rename_for(name):
    """The 0072 name: the test function's own sentence, in snake_case, as a file name."""
    s = re.sub(r"^(Test|Benchmark|Fuzz|Example)", "", name)
    s = re.sub(r"([^A-Z0-9])([A-Z]+)", lambda m: m.group(1) + " " + m.group(2).lower(), " " + s)
    s = re.sub(r"([A-Z]+)([A-Z][a-z])", lambda m: m.group(1).lower() + " " + m.group(2), s)
    s = re.sub(r"[^a-z0-9]+", "_", s.lower()).strip("_")
    return (s[:78] + "_test.go") if s else ""


def summarise(rows, carried_summary, carried_packages, ceiling):
    counts = collections.Counter(r["verdict"] for r in rows)
    files = {r["file"] for r in rows}
    verd_files = collections.Counter()
    seen = set()
    for r in rows:
        if r["file"] not in seen:
            seen.add(r["file"])
            verd_files[r["verdict"]] += 1
    summary = {
        "check_job_seconds_median": carried_summary.get("check_job_seconds_median"),
        "files": len(files),
        "forge_runs": carried_summary.get("forge_runs", []),
        "layers": {k: sum(1 for r in rows if r["layer"] == k) for k in LAYERS},
        "package_coverage": carried_summary.get("package_coverage", {}),
        "round_named_ceiling": ceiling,
        "round_named_files": len({r["file"] for r in rows if r["round_named"]}),
        "rows": len(rows),
        "tiers": {k: sum(1 for r in rows if r["tier"] == k) for k in TIERS},
        "unique_lines_measured_files": len({r["file"] for r in rows if r["unique_lines"] is not None}),
        "unique_lines_zero_files": len({r["file"] for r in rows if r["unique_lines"] == 0}),
        "verdict_files": {k: verd_files.get(k, 0) for k in VERDICTS},
        "verdict_rows": {k: counts.get(k, 0) for k in VERDICTS},
    }
    pkg_round = collections.Counter(r["pkg"] for r in rows if r["round_named"])
    packages = {}
    for pkg in sorted({r["pkg"] for r in rows}):
        old = carried_packages.get(pkg, {})
        packages[pkg] = {
            "ci_fails": old.get("ci_fails", 0), "ci_max_s": old.get("ci_max_s"),
            "ci_med_s": old.get("ci_med_s"), "ci_runs": old.get("ci_runs", 0),
            "coverage": summary["package_coverage"].get(pkg, ""), "round_named": pkg_round.get(pkg, 0),
        }
    return summary, packages


def load(path):
    if not os.path.exists(path):
        return {"packages": {}, "revision_measured": "", "rows": [], "rules": RULES, "schema": SCHEMA, "summary": {}}
    with open(path, encoding="utf-8") as fh:
        return json.load(fh)


RULES = {
    "delete": "no test function or no assertion",
    "keep": "not named for a round",
    "keep-rewrite": "round-named and unique reach; rename_to carries the proposed name",
    "merge": "round-named and no unique reach; merge_into names the sibling",
}


def emit(doc, out_json):
    with open(out_json, "w", encoding="utf-8") as fh:
        json.dump(doc, fh, indent=1, sort_keys=True)
        fh.write("\n")


# --------------------------------------------------------------------------- check

def quoted_figures(md_path):
    """Every figure `tests/INVENTORY.md` quotes in a table cell, keyed by its section and row label.
    A quote the tree no longer answers is the same drift the README's figures are: the tree is the
    authority, so the quote moves, or the check refuses the quote."""
    if not os.path.exists(md_path):
        return {}
    section, out = None, {}
    for line in open(md_path, encoding="utf-8"):
        head = re.match(r"^##+\s+(.*)$", line)
        if head:
            section = head.group(1).strip()
            continue
        if section not in ("Scale", "Layers and tiers", "Verdicts") or not line.strip().startswith("|"):
            continue
        cells = [c.strip() for c in line.strip().strip("|").split("|")]
        if len(cells) < 2:
            continue
        numbers = [int(n) for n in re.findall(r"\d+", cells[1])]
        if numbers and not re.fullmatch(r"\d+(?:\s*/\s*\d+)*", cells[1]):
            continue        # a cell that mixes a figure with prose is not a quoted figure
        if len(cells) > 2 and re.fullmatch(r"\d+", cells[2]):
            numbers.append(int(cells[2]))       # the Verdicts table quotes files and rows in one row
        if numbers:
            out[(section, cells[0].replace("`", "").replace("*", ""))] = numbers
    return out


def expected_figures(rows, summary):
    kinds = collections.Counter(r["kind"] for r in rows)
    want = {
        ("Scale", "test rows"): [summary["rows"]],
        ("Scale", "files"): [summary["files"]],
        ("Scale", "Go test functions"): [kinds["go"]],
        ("Scale", "files named for a round (review, round, probe)"): [summary["round_named_files"]],
        ("Scale", "Playwright specs / Maestro flows / scripts/ pins"): [kinds["playwright"], kinds["maestro"], kinds["shell_pin"]],

        ("Scale", "rows inside them"): [sum(1 for r in rows if r["round_named"])],
        ("Scale", "Playwright specs / Maestro flows / scripts/ pins"): [kinds["playwright"], kinds["maestro"], kinds["shell_pin"]],
        ("Layers and tiers", "contract"): [summary["layers"]["contract"]],
        ("Layers and tiers", "behaviour"): [summary["layers"]["behaviour"]],
        ("Layers and tiers", "composition"): [summary["layers"]["composition"]],
        ("Layers and tiers", "journey"): [summary["layers"]["journey"]],
    }
    for v in VERDICTS:
        want[("Verdicts", v)] = [summary["verdict_files"][v], summary["verdict_rows"][v]]
    return want


def check(doc, md_path, rows):
    """Every refusal 0088 needs of CI, answered from the tree and the file, never a database."""
    failures = []
    if doc.get("schema") != SCHEMA:
        failures.append("tests/inventory.json is absent or is not schema %s: run scripts/test_inventory.py --write and commit the table" % SCHEMA)
        return failures, len(rows), summarise(rows, {}, {}, None)[0]
    existing = {r["id"]: r for r in doc["rows"]}
    tree = {r["id"]: r for r in rows}
    if len(existing) != len(doc["rows"]):
        for rid, n in collections.Counter(r["id"] for r in doc["rows"]).items():
            if n > 1:
                failures.append("duplicate row id (%d): %s" % (n, rid))
    for rid in sorted(set(tree) - set(existing)):
        failures.append("no row for the test in the tree: " + rid)
    for rid in sorted(set(existing) - set(tree)):
        failures.append("row with no test in the tree: " + rid)

    ceiling = doc["summary"].get("round_named_ceiling")
    round_files = sorted({r["file"] for r in rows if r["round_named"]})
    if ceiling is None:
        failures.append("summary.round_named_ceiling is absent, so the round-named count is unbounded: "
                        "run the generator with --ceiling once and commit it")
    elif len(round_files) > ceiling:
        failures.append("round-named test files grew to %d above the ceiling %d — prune or rename one, or lower the ceiling in its own build(budget): commit (%s)"
                        % (len(round_files), ceiling, ", ".join(round_files[-3:])))

    summary = summarise(rows, doc["summary"], doc["packages"], ceiling)[0]
    for key in ("rows", "files", "round_named_files"):
        if doc["summary"].get(key) != summary[key]:
            failures.append("summary.%s says %s; the rows in the tree answer %d" % (key, doc["summary"].get(key), summary[key]))

    have, want = quoted_figures(md_path), expected_figures(rows, summary)
    matched = 0
    for key, values in want.items():
        if not md_path:
            break
        if key not in have:
            failures.append("tests/INVENTORY.md quotes no figure for %s / %s" % key)
        elif have[key] != values:
            failures.append("tests/INVENTORY.md quotes %s for %s / %s; the tree answers %s" % (have[key], key[0], key[1], values))
        else:
            matched += 1
    if md_path and matched < 8:
        failures.append("tests/INVENTORY.md re-derives only %d of the %d figures it should quote"
                        % (matched, len(want)))

    for row in doc["rows"]:
        if row["verdict"] not in VERDICTS:
            failures.append("verdict %r is not one of %s: %s" % (row["verdict"], "/".join(VERDICTS), row["id"]))
        if row["layer"] not in LAYERS or row["tier"] not in TIERS:
            failures.append("layer/tier %s/%s is not a defined pair: %s" % (row["layer"], row["tier"], row["id"]))
        target = row.get("merge_into") or ""
        if target and not os.path.exists(os.path.join(root, target)):
            failures.append("merge_into names a file that is not in the tree: %s -> %s" % (row["id"], target))
        if row["verdict"] == "delete" and row["kind"] == "go" and row.get("unique_lines") is None:
            failures.append("a delete beside an unmeasured coverage column is refused: " + row["id"])
    return failures, len(rows), summary


def report(failures, total, summary, md=True):
    for f in failures:
        print("INVENTORY: " + f, file=sys.stderr)
    if failures:
        print("INVENTORY: %d refusal(s) against %d test(s) in the tree" % (len(failures), total), file=sys.stderr)
        return 1
    print("INVENTORY: %d rows agree with the tree; %d round-named file(s) of %s ceiling; %d test(s) of %d in the push tier%s"
          % (summary["rows"], summary["round_named_files"], summary["round_named_ceiling"], summary["tiers"]["push"], summary["rows"],
             "" if md else " (no prose table in this tree)"))
    return 0


# --------------------------------------------------------------------------- tier

def asked(cmd, where):
    """Run a command the selection is read from, and say so when it cannot answer. A selector that
    ignores its own tools treats "nothing reached" and "nothing asked" as the same empty answer, and
    the wrapper prints the first and exits 0 on either."""
    run = subprocess.run(cmd, cwd=where, capture_output=True, text=True)
    if run.returncode:
        why = run.stderr.strip().splitlines() or ["no reason given"]
        print("TIER: %s failed, so the packages the diff reaches cannot be answered (exit 4): %s"
              % (" ".join(cmd), why[0]), file=sys.stderr)
        return None, 4
    return run.stdout, 0


def tier_selection(tier, base):
    """The packages the named tier runs, for the directories a diff reaches. Selection is a pure
    function of the diff: `go list` gives the dependency graph, its inverse gives the consumers —
    the packages that import a changed one and the packages whose test binary compiles it — and a
    package with no test row is not scheduled. Push refuses a selection that opens a stack."""
    doc = load(os.path.join(root, "tests", "inventory.json"))
    pkgs = {}
    for r in doc["rows"]:
        if r["kind"] == "go":
            pkgs.setdefault(r["pkg"], r["tier"])
    if base:
        changed_text, code = asked(("git", "diff", "--name-only", base + "...HEAD"), root)
        if code:
            return None, code
        changed = changed_text.splitlines()
        changed += git("ls-files", "--others", "--exclude-standard")
    else:
        changed = tracked("*")
    dirs = {os.path.dirname(p) for p in changed if os.path.dirname(p)}

    def choose(reached):
        """The tier's own packages among the directories the selection reaches — a package with no
        test row is not scheduled, and a package whose rows sit in another tier is not this one's."""
        prefix = MODULE + "/"
        selected = {p[len(prefix):] for p in reached if p.startswith(prefix)} | {d for d in dirs if d in pkgs}
        return sorted(p for p in selected if p in pkgs and pkgs[p] == tier)

    def opens_stack(selected):
        if tier != "push":
            return 0
        opens = [p for p in selected for r in doc["rows"] if r["pkg"] == p and r["needs_db"]]
        if opens:
            print("PUSH TIER: the selection reaches packages that open a stack, which the push tier never runs: %s"
                  % " ".join(sorted(set(opens))), file=sys.stderr)
            return 3
        return 0

    # The push tier's promise is asked twice: of the directories the diff touched, before the
    # dependency graph is consulted, and of the whole selection after. The first asking is what a
    # caller that touched a package opening a stack has already earned, and it is answered whatever
    # `go list` does next — including on a tree `go list` cannot read, where the refusal a caller
    # earned must not be replaced by the selector's own complaint about its tool.
    code = opens_stack(choose(set()))
    if code:
        return None, code

    # Three columns out of one `go list`: the production dependency graph, and the two columns of
    # imports that exist only inside a package's test binary. `.Deps` is transitive, so one pass
    # over it names every production consumer; `TestImports` (the `package foo` test files) and
    # `XTestImports` (the `package foo_test` files beside them) are direct, and are read after that
    # pass, against the set the pass completed.
    graph, code = asked(("go", "list", "-f",
                         "{{.ImportPath}}|{{join .Deps \" \"}}|{{join .TestImports \" \"}}|{{join .XTestImports \" \"}}",
                         "./..."), root)
    if code:
        return None, code
    listing, code = asked(("go", "list", "-f", "{{.ImportPath}}|{{.Dir}}", "./..."), root)
    if code:
        return None, code
    import_dir = {}
    for r in listing.splitlines():
        path, _, d = r.partition("|")
        # go list answers with an absolute directory; the diff answers with a path relative to the
        # checkout. One of the two has to move, and a selector that keyed the absolute one matched
        # nothing: the selection was only the directories the diff touched, so no package ever
        # reached its consumer on a push (review 1, finding 1).
        import_dir[os.path.relpath(d, root)] = path
    reached = {import_dir[d] for d in dirs if d in import_dir}
    tested = []
    for line in graph.splitlines():
        name, _, rest = line.partition("|")
        deps, _, test_deps = rest.partition("|")
        internal, _, external = test_deps.partition("|")
        tested.append((name, (internal + " " + external).split()))
        if any(dep in reached for dep in deps.split()):
            reached.add(name)
    # A package that compiles the change into its test binary consumes it, whether the test file
    # sits in the package or in a `package foo_test` beside it — review 1 finding 1 reached only
    # production importers, so a package with no test of its own could change, break the test that
    # imports it, and leave the selector answering an empty list a caller read as "nothing to run".
    # Nothing travels onward through these edges: a consumer of such a package compiles the package,
    # not its test files, which is why `reached` is the set the production pass completed and the
    # test columns are asked against it once, not folded into it.
    out = choose(reached | {name for name, imports in tested if any(dep in reached for dep in imports)})
    code = opens_stack(out)
    if code:
        return None, code
    return out, 0


# --------------------------------------------------------------------------- main

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", default=root_dir, help="checkout to read (the fixture cases point it at a fake tree)")
    ap.add_argument("--check", action="store_true", help="compare the committed table with the tree")
    ap.add_argument("--write", action="store_true", help="refresh mechanical columns and add missing rows")
    ap.add_argument("--tier", choices=TIERS, help="print the packages this tier runs")
    ap.add_argument("--base", default="", help="ref the diff is taken against ('' is every package)")
    ap.add_argument("--ceiling", type=int, default=None, help="lower the round-named ceiling (never raises)")
    ap.add_argument("--markdown", default="", help="prose table to re-check ('-' for a tree that has none)")
    ap.add_argument("--baseline", default=None, help="record what the measured columns were measured against")
    a = ap.parse_args()
    global root
    root = os.path.abspath(a.root)
    doc_path = os.path.join(root, "tests", "inventory.json")
    md_path = os.path.join(root, "tests", "INVENTORY.md") if a.markdown == "" else ("" if a.markdown == "-" else a.markdown)
    if a.markdown == "-" and not os.path.isdir(os.path.join(root, "tests")):
        pass        # a fixture tree says for itself that it has no ratified prose table
    elif a.markdown == "" and not os.path.exists(md_path):
        print("INVENTORY: " + md_path + " is absent, so nothing re-checks the figures a person ratifies", file=sys.stderr)
        return 1

    if a.tier:
        selected, code = tier_selection(a.tier, a.base)
        if code:
            return code
        print(" ".join("./" + p for p in selected))
        return 0

    rows = walk()
    old = load(doc_path)
    by_id = {r["id"]: r for r in old["rows"]}
    ceiling = a.ceiling if a.ceiling is not None else old["summary"].get("round_named_ceiling")
    merged, added = [], 0
    for r in rows:
        r.pop("_surface", None)
        r.pop("_routes", None)
        prev = by_id.get(r["id"])
        if prev is None:
            added += 1
            merged.append(r)
            continue
        # Everything the tree does not answer today — the verdict a person ratified, the reach and
        # the coverage delta a measurement produced — carries across unchanged, so re-running the
        # generator on a tree nobody edited writes the bytes it read.
        for key, value in prev.items():
            if key not in MECHANICAL:
                r[key] = value
        merged.append(r)
    summary, packages = summarise(merged, old["summary"], old["packages"], ceiling)
    if a.ceiling is not None and old["summary"].get("round_named_ceiling") is not None:
        if a.ceiling > old["summary"]["round_named_ceiling"]:
            print("CEILING: --ceiling only lowers %d; a rise belongs in a build(budget): commit" % old["summary"]["round_named_ceiling"], file=sys.stderr)
            return 2
    doc = {"packages": packages,
           "revision_measured": a.baseline or old["revision_measured"],
           "rows": merged,
           "rules": RULES, "schema": SCHEMA, "summary": summary}
    if a.write:
        emit(doc, doc_path)
        print("rows %d (new %d) files %d round-named %d/%s ceiling; verdicts %s"
              % (summary["rows"], added, summary["files"], summary["round_named_files"], summary["round_named_ceiling"],
                 " ".join("%s=%d" % (k, v) for k, v in summary["verdict_files"].items())))
        if added:
            print("%d new row(s): tests/INVENTORY.md still quotes the old figures — update its Scale, Layers and tiers and Verdicts cells." % added, file=sys.stderr)
        return 0
    failures, total, fresh = check(old, md_path, rows)
    return report(failures, total, fresh, bool(md_path))


# The refusal is the answer: --check's exit status is the verdict CI acts on, and check_push_tier.sh
# reads the selection's status before it reads the selection. A bare call here threw the value away and
# every path exited 0, so a refused table and a refused selection both looked green to the gate that
# asked (review 1, finding 2).
sys.exit(main())
