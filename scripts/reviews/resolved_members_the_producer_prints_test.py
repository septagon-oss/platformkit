#!/usr/bin/env python3
"""A resolved member the producer can print as null is nullable or omitted in the document.

The kernel prints a resolved presentation policy from the pointer fields of the types in
kit/entity/present, and publishes the shape of that body as an OpenAPI document from which
clients are generated strictly. The two artefacts have to agree member by member, so this
reads both: every pointer field of every type in that package, and the component the
document publishes for the type that declares it.

For each field, judged at the component its own type publishes — never at a same-named
member of a different component:
  * one with `omitempty` prints nothing when unset, so the component must publish the
    member as a declared property and must not demand it;
  * one without it always prints, null included, so that component must admit null there.

A field that reaches neither spelling is the defect this refuses. Every resolved body this
repository recorded as an answer a composition gives is then validated against the served
document, and each member is asked in a recorded body as a reader meets it: absent, which is
what an entry that declares none prints, and null, which nothing prints for a member that
omits itself. When the `jsonschema` package is installed those body checks run against the
published components; without it they are skipped by name rather than passed silently.

Run: python3 -m pytest -q scripts/reviews/resolved_members_the_producer_prints_test.py
"""
import json
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
RESOLVED_DIR = os.path.join(ROOT, "kit", "entity", "present")
DOCUMENT = os.path.join(ROOT, "apps", "platformkit", "testdata", "openapi.json")
BODIES = [
    os.path.join(ROOT, "ui", "screens", "testdata", "catalog.json"),
    os.path.join(ROOT, "ui", "screens", "testdata", "catalog-deck.json"),
    os.path.join(ROOT, "ui", "screens", "testdata", "catalog-choices.json"),
    os.path.join(ROOT, "apps", "platformkit", "testdata", "catalog-task-en.json"),
    os.path.join(ROOT, "apps", "platformkit", "testdata", "catalog-content-en.json"),
]

STRUCT = re.compile(r"^type\s+([A-Z]\w*)\s+struct\s*\{")
FIELD = re.compile(r"^\s+[A-Za-z_]\w*\s+\*[\w\[\]\.]+\s+`json:\"(\w+)(?:,omitempty)?\"")


def pointer_fields():
    """Every pointer field in the package, by the type that declares it.

    Answers {member: [(declaring type, omitempty when empty), ...]} read from every Go file
    of the package: a member declared in one file of the package is printed as often as any
    other, so the set is the package's, not one file's.
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


def components(document):
    return document["components"]["schemas"]


def type_names(published):
    stated = published.get("type")
    if isinstance(stated, str):
        return [stated]
    if isinstance(stated, list):
        return [str(name) for name in stated]
    return []


def published_at(document, component, member):
    """How `component` publishes `member`: its schema and whether it is required there."""
    schema = components(document).get(component)
    if schema is None:
        return None
    properties = schema.get("properties", {})
    if member not in properties:
        return None
    return properties[member], member in (schema.get("required") or [])


def resolved_bodies():
    """Every resolved policy this repository recorded as an answer the composition gives."""
    out = []
    for path in BODIES:
        with open(path, encoding="utf-8") as handle:
            recorded = json.load(handle)
        for resource in recorded.get("resources", []) or []:
            if isinstance(resource, dict) and isinstance(resource.get("resolved"), dict):
                out.append(resource["resolved"])
        if isinstance(recorded.get("resolved"), dict):
            out.append(recorded["resolved"])
    return out


def every_object(policy):
    """Every object of a recorded resolved policy, the policy itself included."""
    seen, stack = [], [policy]
    while stack:
        value = stack.pop()
        if isinstance(value, dict):
            seen.append(value)
            stack.extend(value.values())
        elif isinstance(value, list):
            stack.extend(value)
    return seen


def instances(body, document, component):
    """The objects of `body` that answer `component`: its required members, no others.

    Matching on the published members is what tells one component's object from another's,
    and it is what keeps a member named `group` on a choice option from being asked about as
    the member named `group` on an entry's identity. Every resolved component closes its
    members, so an object that holds exactly those members is one of them.
    """
    schema = components(document).get(component)
    if schema is None or schema.get("additionalProperties") is not False:
        return []
    required, properties = set(schema.get("required") or []), set(schema.get("properties") or {})
    return [holder for holder in every_object(body)
            if required <= set(holder) and set(holder) <= properties]


def validator(document):
    jsonschema = __import__("jsonschema", fromlist=["Draft202012Validator"])
    return jsonschema.Draft202012Validator(
        {"$ref": "#/components/schemas/Resolved", "components": document["components"]},
        format_checker=jsonschema.FormatChecker())


def test_every_pointer_member_is_nullable_or_omitted_where_its_type_publishes_it():
    """Each pointer member is judged at the component its own type publishes."""
    fields = pointer_fields()
    assert fields, "no pointer field found in the resolved types: this case would say nothing"
    document = json.load(open(DOCUMENT, encoding="utf-8"))
    for member, declared in sorted(fields.items()):
        for declaring_type, omitted_when_empty in declared:
            where = published_at(document, declaring_type, member)
            assert where, (
                f"{declaring_type} prints {member!r}, which that component does not publish: "
                "a reader generated from the document has nothing to read it into")
            published, required = where
            if omitted_when_empty:
                assert not required, (
                    f"{declaring_type} still requires {member!r}, which is printed only when declared")
                assert published.get("$ref", "").startswith("#/components/schemas/"), \
                    f"{declaring_type}.{member} is published as {published}, not as the component a reader resolves"
            else:
                assert "null" in type_names(published), (
                    f"{declaring_type}.{member} is printed for every {declaring_type} and publishes "
                    f"{type_names(published)}: a reader of this document cannot be sent the null the producer prints")


def test_every_recorded_resolved_body_answers_the_served_document():
    """Every resolved policy recorded in this repository is admitted as it stands."""
    validator_of = validator(json.load(open(DOCUMENT, encoding="utf-8")))
    bodies = resolved_bodies()
    assert bodies, "no recorded resolved policy: this case would say nothing"
    problems = []
    for index, policy in enumerate(bodies):
        for error in validator_of.iter_errors(policy):
            problems.append(f"body #{index} {'/'.join(str(p) for p in error.absolute_path)}: {error.message}")
    assert not problems, "the served document refuses the body the producer printed:\n  " + "\n  ".join(problems)


def test_a_member_may_be_left_out_where_it_is_omitted_and_never_elsewhere():
    """Absent is admitted where the producer may leave the member out; null never is."""
    document = json.load(open(DOCUMENT, encoding="utf-8"))
    check = validator(document)
    bodies = resolved_bodies()
    for member, declared in sorted(pointer_fields().items()):
        for declaring_type, omitted_when_empty in declared:
            holding = [body for body in bodies if instances(body, document, declaring_type)]
            assert holding, (
                f"no recorded body holds a {declaring_type}, so {declaring_type}.{member} could not be asked "
                "in a body — its spelling in the document is still asked above")
            for shape, admits in shapes_of(omitted_when_empty):
                for body in holding:
                    asked = json.loads(json.dumps(body))
                    for same in instances(asked, document, declaring_type):
                        if shape == "remove":
                            same.pop(member, None)
                        else:
                            same[member] = None
                    admitted = not list(check.iter_errors(asked))
                    assert admitted == admits, (
                        f"{declaring_type}.{member} "
                        f"{'left out' if shape == 'remove' else 'printed as null'}: "
                        f"the served document answered {admitted}, want {admits}")


def shapes_of(omitted_when_empty):
    """The two shapes a reader meets one member in, and what the document must answer each.

    `remove` asks the body without the member at all, `null` asks it printed as null.
    """
    if omitted_when_empty:
        return [("remove", True), ("null", False)]
    return [("remove", False), ("null", True)]


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
            except ImportError as missing:
                print(f"skip {name}: {missing}")
    sys.exit(1 if failures else 0)
