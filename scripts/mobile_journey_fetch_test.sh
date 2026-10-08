#!/usr/bin/env bash
# The device journey's shell build comes down with the job's token, on the runner that has a device.
#
# The forge answers an anonymous release-asset download with its sign-in page and a 200, so a plain
# curl saves HTML and the harness refuses on the digest. The journey job therefore has a step before
# `make mobile-e2e` that downloads the pin with `Authorization: token $GITHUB_TOKEN`, checks it, and
# exports PK_MOBILE_APK as a file:// URL for the harness. This runs that step, as the workflow file
# states it, against a local server that behaves like the forge: the login page without the token,
# the build with it. It also holds the two lines the step depends on: the job runs on
# pkit-ci-android (and no other job does), and no job-level PK_MOBILE_APK competes with the export.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
workflow="$root/.gitea/workflows/mobile.yml"
work="$(mktemp -d)"
server=''
cleanup() {
	if [ -n "$server" ]; then kill "$server" 2>/dev/null || true; fi
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
import glob, os, sys, yaml

path, folder, work = sys.argv[1:4]
job = yaml.safe_load(open(path))["jobs"]["journey"]
problems = []
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
    if "secrets.GITHUB_TOKEN" not in str((step.get("env") or {}).get("GITHUB_TOKEN", "")):
        problems.append("the fetch step is not given the job's token")
    open(os.path.join(work, "fetch.sh"), "w").write(step["run"])
open(os.path.join(work, "problems"), "w").write("".join(p + "\n" for p in problems))
READ
while IFS= read -r problem; do fail "$problem"; done <"$work/problems"
if [ ! -s "$work/fetch.sh" ]; then
	echo "$failures failure(s)"
	exit 1
fi

# The forge: a 200 sign-in page without the token, the build with it.
mkdir -p "$work/srv" "$work/runner-temp"
head -c 65536 /dev/urandom >"$work/srv/shell.apk"
digest="$(sha256sum "$work/srv/shell.apk" | cut -d' ' -f1)"
cat >"$work/forge.py" <<'FORGE'
import http.server, os, sys

class Forge(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.headers.get("Authorization") == "token job-token":
            body = open(os.path.join(sys.argv[2], "shell.apk"), "rb").read()
        else:
            body = b"<html><title>Sign In</title></html>"
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass

server = http.server.HTTPServer(("127.0.0.1", 0), Forge)
open(sys.argv[1], "w").write(str(server.server_port))
server.serve_forever()
FORGE
python3 "$work/forge.py" "$work/port" "$work/srv" </dev/null >"$work/forge.log" 2>&1 &
server=$!
for _ in $(seq 1 50); do [ -s "$work/port" ] && break; sleep 0.1; done
url="http://127.0.0.1:$(cat "$work/port")/shell.apk"

# fetch TOKEN — run the step as the runner would, leaving its GITHUB_ENV in $work/env.
fetch() {
	: >"$work/env"
	env -i PATH="$PATH" RUNNER_TEMP="$work/runner-temp" GITHUB_ENV="$work/env" GITHUB_TOKEN="$1" \
		PK_MOBILE_APK="$url" PK_MOBILE_APK_SHA256="$digest" bash -e "$work/fetch.sh" </dev/null >"$work/out" 2>&1
}

if fetch ''; then
	fail "without the token the step accepted the sign-in page: $(cat "$work/out")"
fi
if [ -s "$work/env" ]; then
	fail "a refused download still exported: $(cat "$work/env")"
fi

if ! fetch job-token; then
	fail "with the token the step refused the pinned build: $(cat "$work/out")"
else
	exported="$(sed -n 's/^PK_MOBILE_APK=//p' "$work/env")"
	case "$exported" in
	file://*) ;;
	*) fail "the step exported PK_MOBILE_APK=$exported, not a file:// URL" ;;
	esac
	# The harness's own fetch and check, as scripts/mobile_e2e.sh runs them, on what the step exported.
	if ! curl -fsSL "$exported" -o "$work/cached" || ! echo "$digest  $work/cached" | sha256sum --check --status; then
		fail "the harness cannot open $exported as the pinned build"
	fi
fi

if [ "$failures" -gt 0 ]; then
	echo "$failures failure(s)"
	exit 1
fi
echo "ok: the journey fetches its shell build with the job's token on pkit-ci-android"
