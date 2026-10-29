#!/usr/bin/env python3
"""Both published documents decide the same members required, at the same spelling.

The composition publishes two OpenAPI documents from one set of Go types: the reference
document, which answers a catalog call in both int64 spellings at once, and the exact-int64
document a client that cannot hold a 64-bit integer is generated from. A phone may be
generated from either, so a resolved member that a reader may find absent has to be absent
in both — a relaxation landed in one document only, or a producer tag left printed in one
spelling, is the same defect the pair already refuses, arriving through the second door.

This asks two things of the two checked-in documents:

  * for every component both publish, the `required` list is the same decision, and the
    resolved family publishes the same member names;
  * every pointer member of kit/entity/present, judged at the component its own declaring
    type publishes, holds its spelling in each document: one with `omitempty` stays a
    declared property naming a component and is demanded by neither document, one without
    it admits null in both.

A body the producer prints is asked elsewhere, of the served document; this case reads the
documents themselves, so a golden that moved on one side and not the other is refused here.

Run: python3 -m pytest -q scripts/reviews/resolved_members_both_published_documents_decide_alike_test.py
"""
import json
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
RESOLVED_DIR = os.path.join(ROOT, "kit", "entity", "present")
TESTDATA = os.path.join(ROOT, "apps", "platformkit", "testdata")
DOCUMENTS = ["openapi.json", "openapi.int64.json"]

STRUCT = re.compile(r"^type\s+([A-Z]\w*)\s+struct\s*\{")
FIELD = re.compile(r"^\s+[A-Za-z_]\w*\s+\*[\w\[\]\.]+\s+`json:\"(\w+)(?:,omitempty)?\"")


def pointer_fields():
    """Every pointer field of the resolved package, by the type that declares it.

    Answers {member: [(declaring type, omitted_when_empty), ...]} read from every Go file of
    the package: the printed set is the package's, not the file a member happens to sit in.
    """
    out = {}
    for name in sorted(os.listdir(RESOLVED_DIR)):
        if not name.endswith(".go") or name.endswith("_test.go"):
            continue
        current = None
        with open(os.path.join(RESOLVED_DIR, name), encoding="utf-8") as handle:
            for line in handle:
                stated = STRUCT.match(line)
                if stated:
                    current = stated.group(1)
                    continue
                if current and line.rstrip().startswith("}"):
                    current = None
                    continue
                if not current:
                    continue
                declared = FIELD.match(line)
                if declared:
                    out.setdefault(declared.group(1), []).append(
                        (current, ",omitempty" in line))
    return out


def schemas(document):
    return document["components"]["schemas"]


def load():
    return {name: json.load(open(os.path.join(TESTDATA, name), encoding="utf-8"))
            for name in DOCUMENTS}


def type_names(published):
    stated = published.get("type")
    if isinstance(stated, str):
        return [stated]
    if isinstance(stated, list):
        return [str(name) for name in stated]
    return []


def required_of(schema):
    return sorted(schema.get("required") or [])


def test_the_two_published_documents_decide_the_same_members_required():
    """One Go type, two documents: the same `required`, and the same members, per component."""
    documents = load()
    assert len(documents) == len(DOCUMENTS), "a published document is missing: this case would say nothing"
    reference, other = (schemas(documents[name]) for name in DOCUMENTS)
    shared = sorted(set(reference) & set(other))
    assert shared, "the two documents share no component: this case would say nothing"
    for component in shared:
        assert required_of(reference[component]) == required_of(other[component]), (
            f"{component} is required differently by the two documents: a client generated "
            f"from one is told to demand what a client generated from the other may be sent without "
            f"({required_of(reference[component])} against {required_of(other[component])})")
        if component.startswith(("Resolved", "Choice")):
            assert sorted(reference[component].get("properties") or {}) == \
                sorted(other[component].get("properties") or {}), (
                f"{component} publishes different members in the two documents, so a reader of "
                "one cannot read what a reader of the other is sent")


def test_every_omitted_resolved_member_is_optional_in_both_documents():
    """Each pointer member keeps its spelling at the component its own type publishes, twice."""
    documents = load()
    fields = pointer_fields()
    assert fields, "no pointer field found in the resolved types: this case would say nothing"
    for member, declared in sorted(fields.items()):
        for declaring_type, omitted_when_empty in declared:
            for name in DOCUMENTS:
                component = schemas(documents[name]).get(declaring_type)
                assert component is not None, f"{name} publishes no {declaring_type}"
                published = (component.get("properties") or {}).get(member)
                assert published is not None, (
                    f"{name}: {declaring_type} prints {member!r}, which that document does not "
                    "publish: a reader generated from it has nothing to read it into")
                demanded = member in (component.get("required") or [])
                if omitted_when_empty:
                    assert not demanded, (
                        f"{name}: {declaring_type} still requires {member!r}, which the producer "
                        "prints only when the author declared one")
                    assert published.get("$ref", "").startswith("#/components/schemas/"), (
                        f"{name}: {declaring_type}.{member} is published as {published}, not as the "
                        "component a reader resolves")
                else:
                    assert "null" in type_names(published), (
                        f"{name}: {declaring_type}.{member} is printed for every {declaring_type} "
                        f"and publishes {type_names(published)}: the null the producer prints is "
                        "not admitted here")


if __name__ == "__main__":
    failures = 0
    for name, case in sorted(globals().items()):
        if name.startswith("test_") and callable(case):
            try:
                case()
                print(f"ok   {name}")
            except AssertionError as problem:
                failures += 1
                print(f"FAIL {name}: {problem}")
    sys.exit(1 if failures else 0)
