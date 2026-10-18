#!/usr/bin/env bash
# The device journey's shell build comes down through the forge's API, on the runner that has a device.
#
# The forge requires sign-in to read anything, and a job's own token is a session on nothing: run
# 62943 asked the browser address of the pinned release asset with `Authorization: token <the job's
# own token>` and was answered 404, where the same address turns an unsigned request toward the
# sign-in page and the API answers an unsigned read 403. The journey job therefore has one step
# before `make mobile-e2e` that resolves the pin the way the forge names it — the release by tag and
# the asset by name, through the API — downloads the address that release document publishes for it
# with PK_MOBILE_TOKEN, a read-only repository secret, falling back to the job token where none is
# set, checks the digest, and exports PK_MOBILE_APK as a file:// URL for the harness.
#
# This runs that step, as the workflow file states it, against a local forge that answers each route
# as the real one was measured to: 403 for an unsigned API read, 200 and the release document for a
# token it knows, 401 for a token it does not, the asset bytes to a token it knows on the browser
# address and 404 to the job's own token. One answer is assumed rather than measured: the API is
# answered for the job token as well, so the refusal below is the one that job really reached — the
# metadata came back and the bytes did not. The cases then ask the step's behaviour rather than its
# text: that it refuses an unreadable release and an unknown token, refuses the job's own token
# while naming the address it tried, succeeds with a token the forge answers, installs nothing whose
# digest differs, takes the asset the pin names rather than the first one the release lists, keeps
# its token on the forge the pin names when an asset is published elsewhere, and refuses an
# unresolvable or unset pin without asking the forge for anything. Two lines around the step still
# hold: the job runs on pkit-ci-android and no other job does, and no job-level PK_MOBILE_APK
# competes with the export.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
workflow="$root/.gitea/workflows/mobile.yml"
work="$(mktemp -d)"
server=''
foreign=''
cleanup() {
	if [ -n "$server" ]; then kill "$server" 2>/dev/null || true; fi
	if [ -n "$foreign" ]; then kill "$foreign" 2>/dev/null || true; fi
	rm -rf "$work"
}
trap cleanup EXIT
failures=0
fail() {
	echo "FAIL: $*"
	failures=$((failures + 1))
}

# The step's own text, and the facts about the job around it, read from the workflow as YAML.
python3 - "$workflow" "$root/.gitea/workflows" "$work" <<'READ'
import glob, os, re, sys, yaml

path, folder, work = sys.argv[1:4]
doc = yaml.safe_load(open(path))
job = doc["jobs"]["journey"]
problems = []
triggers = doc.get(True) or doc.get("on") or {}
if "workflow_dispatch" not in triggers:
    problems.append("mobile.yml declares no workflow_dispatch trigger, so the branch that changed "
                    "the fetch cannot ask the runner with a device for a journey run")
if job.get("runs-on") != "pkit-ci-android":
    problems.append("the journey job runs on %r, not pkit-ci-android" % job.get("runs-on"))
for other in sorted(glob.glob(os.path.join(folder, "*.yml"))):
    for name, body in (yaml.safe_load(open(other)).get("jobs") or {}).items():
        if body.get("runs-on") == "pkit-ci-android" and not (other == path and name == "journey"):
            problems.append("%s job %s also runs on pkit-ci-android" % (os.path.basename(other), name))
if "PK_MOBILE_APK" in (job.get("env") or {}):
    problems.append("a job-level PK_MOBILE_APK competes with the step's file:// export")
steps = job["steps"]
runs = [step.get("run", "") for step in steps]
harness = next((i for i, run in enumerate(runs) if run.strip() == "make mobile-e2e"), None)
fetch = [i for i, run in enumerate(runs) if "GITHUB_ENV" in run and "PK_MOBILE_APK=file://" in run]
if harness is None:
    problems.append("no step runs make mobile-e2e")
elif not fetch or fetch[0] > harness:
    problems.append("no step before make mobile-e2e exports PK_MOBILE_APK as a file:// URL")
else:
    step = steps[fetch[0]]
    token = str((step.get("env") or {}).get("GITHUB_TOKEN", ""))
    if "secrets.GITHUB_TOKEN" not in token:
        problems.append("the fetch step is not given the job's token")
    if not re.search(r"secrets\.(?!GITHUB_TOKEN\b)[A-Z0-9_]+", token):
        problems.append("the fetch step is given no repository secret: the forge requires sign-in to "
                        "read a release asset and a job token is nobody to it, so the download needs "
                        "a read-only token of its own")
    open(os.path.join(work, "fetch.sh"), "w").write(step["run"])
open(os.path.join(work, "problems"), "w").write("".join(p + "\n" for p in problems))
READ
while IFS= read -r problem; do fail "$problem"; done <"$work/problems"
if [ ! -s "$work/fetch.sh" ]; then
	echo "$failures failure(s)"
	exit 1
fi

# The forge double: two builds, three releases and the four credential answers the real one gives.
mkdir -p "$work/srv" "$work/runner-temp"
head -c 65536 /dev/urandom >"$work/srv/shell.apk"
head -c 65536 /dev/urandom >"$work/srv/first-thing.apk"
digest="$(sha256sum "$work/srv/shell.apk" | cut -d' ' -f1)"
other_digest="$(sha256sum "$work/srv/first-thing.apk" | cut -d' ' -f1)"
cat >"$work/forge.py" <<'FORGE'
import http.server, json, os, sys, urllib.parse

log, fixture, off_forge = sys.argv[1:4]

SIGN_IN = b"<html><title>Sign In</title></html>"
NOT_FOUND = b"404 page not found"
NO_SESSION = b'{"message":"Only signed in user is allowed to call APIs."}'
BAD_TOKEN = b'{"message":"invalid username, password or token"}'

# Which asset each release carries, in the order the forge lists it. `pinned` lists an asset the pin
# does not name first, so a step that took the first asset of a release hashes the wrong build;
# `renamed` carries no asset under the name the pin uses; `elsewhere` publishes the pinned asset at
# `off_forge`, an address the pin never named — a second server started beside this one, which
# records every request it receives so the journey's token leaving the forge is a fact.
RELEASES = {"pinned": ["first-thing.apk", "shell.apk"], "renamed": ["first-thing.apk"],
            "elsewhere": ["shell.apk"]}


class Forge(http.server.BaseHTTPRequestHandler):
    def reply(self, status, body=NOT_FOUND, content_type="text/plain", location=None):
        self.send_response(status)
        if location is not None:
            self.send_header("Location", location)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def metadata(self, tag, prefix, host):
        assets = []
        for name in RELEASES[tag]:
            if tag == "elsewhere":
                address = off_forge
            else:
                address = "http://%s/%s/releases/download/%s/%s" % (host, prefix, tag, name)
            assets.append({"id": len(assets) + 1, "name": name, "uuid": "00000000-0000-4000-8000-00000000000%d" % len(assets),
                           "size": os.path.getsize(os.path.join(fixture, name)), "browser_download_url": address})
        return json.dumps({"id": 1, "tag_name": tag, "name": tag, "assets": assets}).encode()

    def do_GET(self):
        asked = urllib.parse.urlsplit(self.path)
        with open(log, "a") as handle:
            handle.write(asked.path + "\n")
        header = self.headers.get("Authorization") or ""
        token = header[6:].strip() if header.startswith("token") else ""
        segs = [s for s in asked.path.split("/") if s]
        host = self.headers.get("Host")
        if segs[:2] == ["api", "v1"]:
            if segs[2:3] != ["repos"] or segs[-2:-1] != ["tags"] or len(segs) < 7:
                return self.reply(404)
            if not token:
                return self.reply(403, NO_SESSION, "application/json")
            if token not in ("read-token", "job-token"):
                return self.reply(401, BAD_TOKEN, "application/json")
            tag = urllib.parse.unquote(segs[-1])
            if tag not in RELEASES:
                return self.reply(404)
            return self.reply(200, self.metadata(tag, "/".join(segs[3:-3]), host), "application/json")
        edge = [i for i, part in enumerate(segs[:-1]) if part == "releases" and segs[i + 1] == "download"]
        if len(edge) == 1 and edge[0] >= 2:
            tag, name = "/".join(segs[edge[0] + 2:-1]), segs[-1]
            if not token:
                return self.reply(303, b"", location="/user/login?redirect_to=" + urllib.parse.quote(asked.path))
            if token == "job-token":
                return self.reply(404)
            if token != "read-token":
                return self.reply(401, BAD_TOKEN, "application/json")
            if tag not in RELEASES or name not in RELEASES[tag]:
                return self.reply(404)
            with open(os.path.join(fixture, name), "rb") as build:
                return self.reply(200, build.read())
        if asked.path == "/user/login":
            return self.reply(200, SIGN_IN, "text/html")
        return self.reply(404)

    def log_message(self, *args):
        pass


server = http.server.HTTPServer(("127.0.0.1", 0), Forge)
open(sys.argv[4], "w").write(str(server.server_port))
server.serve_forever()
FORGE

# The address off the forge: the same bytes, served to any caller, and a record of whoever called.
# Refusing it is the promise; a request in its log is that promise broken.
cat >"$work/foreign.py" <<'FOREIGN'
import http.server, os, sys

class Foreign(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        with open(sys.argv[1], "a") as handle:
            handle.write((self.headers.get("Authorization") or "no credential") + " " + self.path + "\n")
        with open(os.path.join(sys.argv[3], "shell.apk"), "rb") as build:
            body = build.read()
        self.send_response(200)
        self.send_header("Content-Type", "application/octet-stream")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


server = http.server.HTTPServer(("127.0.0.1", 0), Foreign)
open(sys.argv[2], "w").write(str(server.server_port))
server.serve_forever()
FOREIGN
python3 "$work/foreign.py" "$work/foreign-requests" "$work/port2" "$work/srv" </dev/null >"$work/foreign.log" 2>&1 &
foreign=$!
for _ in $(seq 1 50); do [ -s "$work/port2" ] && break; sleep 0.1; done
off_forge="http://127.0.0.1:$(cat "$work/port2")/shell.apk"
: >"$work/foreign-requests"
python3 "$work/forge.py" "$work/requests" "$work/srv" \
	"$off_forge" "$work/port" </dev/null >"$work/forge.log" 2>&1 &
server=$!
for _ in $(seq 1 50); do [ -s "$work/port" ] && break; sleep 0.1; done
base="http://127.0.0.1:$(cat "$work/port")"
pinned="$base/fixture-owner/fixture-repo/releases/download/pinned/shell.apk"
renamed="$base/fixture-owner/fixture-repo/releases/download/renamed/shell.apk"
elsewhere="$base/fixture-owner/fixture-repo/releases/download/elsewhere/shell.apk"
asked_for="$base/shell.apk"

# fetch TOKEN [PIN] [DIGEST] — run the step as the runner would, leaving its GITHUB_ENV in $work/env
# and the forge's record of what the step asked for in $work/requests.
fetch() {
	local token apk sha
	token="$1"
	apk="${2-$pinned}"
	sha="${3-$digest}"
	: >"$work/env"
	: >"$work/requests"
	: >"$work/foreign-requests"
	env -i PATH="$PATH" RUNNER_TEMP="$work/runner-temp" GITHUB_ENV="$work/env" GITHUB_TOKEN="$token" \
		PK_MOBILE_APK="$apk" PK_MOBILE_APK_SHA256="$sha" bash -e "$work/fetch.sh" </dev/null >"$work/out" 2>&1
}

# Without any credential the release itself is unreadable, and the step must say so rather than
# download whatever the address hands out.
if fetch ''; then
	fail "without a credential the step accepted the release: $(cat "$work/out")"
fi
if [ -s "$work/env" ]; then
	fail "a refused download still exported: $(cat "$work/env")"
fi
grep -q "^/fixture-owner/fixture-repo/releases/download/" "$work/requests" &&
	fail "with no credential the step asked for the asset bytes anyway: $(cat "$work/requests")"
grep -q "could not read the release" "$work/out" ||
	fail "the refusal for an unreadable release does not say which read failed: $(cat "$work/out")"

# The job's own token: run 62943's failure. The metadata is the job's to read; the asset is not.
if fetch job-token; then
	fail "with the job's own token the step downloaded the pinned build: $(cat "$work/out")"
fi
if [ -s "$work/env" ]; then
	fail "the 404 the job's token was answered with still exported: $(cat "$work/env")"
fi
grep -q "^/fixture-owner/fixture-repo/releases/download/pinned/shell.apk$" "$work/requests" ||
	fail "with the job's token the step never asked the forge for the asset, so it refused something else: $(cat "$work/requests")"
grep -q "$base" "$work/out" ||
	fail "the refusal does not name the address it tried: $(cat "$work/out")"
grep -q "could not download" "$work/out" ||
	fail "the 404 was hashed and refused as a wrong digest instead of named as a failed download: $(cat "$work/out")"

# A token the forge knows, which is what PK_MOBILE_TOKEN holds: the release is resolved by tag
# through the API, the asset by name, and the bytes come from the address the release document gives.
if ! fetch read-token; then
	fail "with a read-only token the step refused the pinned build: $(cat "$work/out")"
else
	exported="$(sed -n 's/^PK_MOBILE_APK=//p' "$work/env")"
	case "$exported" in
	file://*) ;;
	*) fail "the step exported PK_MOBILE_APK=$exported, not a file:// URL" ;;
	esac
	grep -q "^/api/v1/repos/fixture-owner/fixture-repo/releases/tags/pinned$" "$work/requests" ||
		fail "the step never resolved the pinned tag through the forge's API, it fetched the pin's own address: $(cat "$work/requests")"
	grep -q "^/fixture-owner/fixture-repo/releases/download/pinned/first-thing.apk$" "$work/requests" &&
		fail "the step took the first asset of the release rather than the one the pin names: $(cat "$work/requests")"
	# The harness's own fetch and check, as scripts/mobile_e2e.sh runs them, on what the step exported.
	if ! curl -fsSL "$exported" -o "$work/cached" || ! echo "$digest  $work/cached" | sha256sum --check --status; then
		fail "the harness cannot open $exported as the pinned build"
	fi
fi

# The digest is the last line and the step still holds it: a build that is not the one pinned is
# refused, and refused before the harness ever sees a file.
if fetch read-token "$pinned" "$other_digest"; then
	fail "a build whose digest differs from PK_MOBILE_APK_SHA256 was accepted"
fi
if [ -s "$work/env" ]; then
	fail "a build that failed the digest check still exported: $(cat "$work/env")"
fi
grep -q "PK_MOBILE_APK_SHA256" "$work/out" && grep -qi "nothing is installed" "$work/out" ||
	fail "the digest refusal does not name the pin and say nothing was installed: $(cat "$work/out")"

# A pin whose asset left the release: the forge lists what it has, and the step refuses on the name
# rather than taking a build nobody pinned.
if fetch read-token "$renamed"; then
	fail "a release carrying no asset named by the pin was accepted: $(cat "$work/out")"
fi
if [ -s "$work/env" ]; then
	fail "a pin the release does not answer still exported: $(cat "$work/env")"
fi
grep -q "shell.apk" "$work/out" ||
	fail "the refusal does not name the asset the release is missing: $(cat "$work/out")"

# An asset published away from the forge: the token stays where the pin points, which the second
# server answers by having heard nothing at all.
if fetch read-token "$elsewhere"; then
	fail "an asset published on another host was accepted: $(cat "$work/out")"
fi
grep -q "$off_forge" "$work/out" ||
	fail "the refusal does not name the address the asset is published at: $(cat "$work/out")"
[ -s "$work/foreign-requests" ] &&
	fail "the job sent its token to an address PK_MOBILE_APK does not name: $(cat "$work/foreign-requests")"

# A pin that is no release address is refused as a pin, not tried as a download.
if fetch read-token "$asked_for"; then
	fail "a PK_MOBILE_APK that is no release-asset address was downloaded anyway"
fi
if [ -s "$work/requests" ]; then
	fail "a pin the step cannot resolve still asked the forge for something: $(cat "$work/requests")"
fi

# An unset pin is refused naming both variables, as the harness would.
if fetch read-token "" ""; then
	fail "an unset PK_MOBILE_APK was accepted"
fi
grep -q "PK_MOBILE_APK" "$work/out" && grep -q "PK_MOBILE_APK_SHA256" "$work/out" ||
	fail "the refusal for an unset pin names neither variable: $(cat "$work/out")"

if [ "$failures" -gt 0 ]; then
	echo "$failures failure(s)"
	exit 1
fi
echo "ok: the journey resolves its shell build through the forge's API and refuses a token it cannot read with"
