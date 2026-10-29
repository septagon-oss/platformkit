#!/usr/bin/env bash
# The port a gate run serves on, and the four contracts that hang off it: a port
# somebody is already listening on is refused rather than fought over; the answer to
# "may I serve here" is never taken from a stranger's listener, so who holds a port is
# read from the socket and the machine that reads it has proved it can; the browser run
# at the end of scripts/e2e.sh is told which port the application was actually given;
# and the CI jobs that ask these questions install the tool the answer comes from.
#
# Node is the tool under test here, so it is a prerequisite and not something this
# case installs — which is how scripts/e2e.sh treats it for the same reason.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=scripts/free_port.sh
. "$root/scripts/free_port.sh"

if ! command -v node >/dev/null; then
	echo "free port: node is not installed, so nothing below could run; SKIPPED and unproven on this machine" >&2
	exit 0
fi

failures=0
held_pid=""
cleanup() { # the listener this case started, stopped by the pid it recorded
	if [ -n "$held_pid" ]; then
		kill "$held_pid" 2>/dev/null || true
		wait "$held_pid" 2>/dev/null || true
	fi
}
trap cleanup EXIT

check() { # check <case name> <expected exit> <actual exit>
	local name="$1" want="$2" got="$3"
	if [ "$got" != "$want" ]; then
		echo "FAIL: $name: exit $got, wanted $want"
		failures=$((failures + 1))
	else
		echo "ok   $name"
	fi
}

# 1. An allocation is a port a fresh listener can bind. The proof is to hand the
#    same answer straight back: bind_free_port <p> succeeding is the bind() the
#    application is about to make, and a port printed while taken is the bug that
#    this whole change is about.
allocated="$(bind_free_port)"
got=0
if [[ "$allocated" =~ ^[1-9][0-9]{0,4}$ ]]; then
	bind_free_port "$allocated" >/dev/null 2>&1 || got=$?
else
	got=1
	allocated="'$allocated' is not a port number at all"
fi
check "an allocation ($allocated) is a port a fresh listener can bind" 0 "$got"

# 2. A port somebody is listening on is refused, and refused on the status alone:
#    the caller decides what to say. The listener belongs to this script, was
#    started on a port it just took free of, and is stopped by the pid recorded here.
held_port="$(bind_free_port)"
node -e 'const net = require("node:net");
	const server = net.createServer();
	server.on("error", () => process.exit(1));
	server.listen(Number(process.argv[1]), "127.0.0.1");' "$held_port" &
held_pid=$!
sleep 1
if ! kill -0 "$held_pid" 2>/dev/null; then
	echo "FAIL: this case's own listener died, so nothing held $held_port and nothing was refused"
	failures=$((failures + 1))
else
	got=0
	stdout="$(bind_free_port "$held_port" 2>/dev/null || true)"
	bind_free_port "$held_port" >/dev/null 2>&1 || got=$?
	check "a port somebody is listening on ($held_port) is refused" 1 "$got"
	if [ -n "$stdout" ]; then
		echo "FAIL: a refused port is refused on the status and prints nothing; it printed '$stdout'"
		failures=$((failures + 1))
	else
		echo "ok   a refused port prints nothing, so the caller's message is the only one a person reads"
	fi

	# 3. An allocation never lands on a listener that is already up. Twenty asks is
	#    not a claim about the whole range; it is the question the gate asks once,
	#    asked often enough to catch a scan upward from a literal pretending to be an
	#    allocation — which is exactly what the fixed 8099 was.
	landed=""
	for _ in $(seq 1 20); do
		if [ "$(bind_free_port)" = "$held_port" ]; then landed="$held_port"; break; fi
	done
	if [ -n "$landed" ]; then
		echo "FAIL: an allocation landed on the port this case is holding ($landed)"
		failures=$((failures + 1))
	else
		echo "ok   twenty allocations all avoid the one port that is taken"
	fi

	# 3a. The other question about a port: not whether it may be taken, but who holds
	#     it. scripts/e2e.sh depends on the answer before it trusts an HTTP 200 on
	#     /health, because a 200 is the shape of the probe and not the signature of the
	#     process behind it. The listener here is one this case started, on a port this
	#     script allocated, so the expected answer is this pid and nothing else.
	owners="$(port_listeners "$held_port" 2>/dev/null | tr '\n' ' ')"
	if [ "$owners" != "$held_pid " ]; then
		echo "FAIL: port_listeners said '$held_port' is held by '$owners', and the only listener there is pid $held_pid"
		failures=$((failures + 1))
	else
		echo "ok   who holds a port is answered from the socket, and names the listener that is"
	fi

	# 3b. The same question asked of a port nothing is on. Silence is the answer gate 10
	#     reads as "not mine yet", so it has to be silence and not a failure to ask.
	free_probe="$(bind_free_port)"
	if [ -n "$(port_listeners "$free_probe" 2>/dev/null)" ]; then
		echo "FAIL: port_listeners named a holder for $free_probe, which nothing is listening on"
		failures=$((failures + 1))
	else
		echo "ok   a port nobody listens on is reported as held by nobody"
	fi

	# 3c. And the machine has to have proved it can answer, because the stripped ss and
	#     the container with no /proc return nothing for every question, and silence read
	#     as "not mine" would then refuse every run. This is the check scripts/e2e.sh
	#     makes for itself before it builds anything.
	got=0
	port_attribution_works >/dev/null 2>&1 || got=$?
	check "this machine attributes a listening socket to the process that holds it" 0 "$got"
fi

# 4. scripts/e2e.sh refuses a port its caller named that somebody is holding, and
#    refuses before it touches anything. The two URLs name a Postgres that is not
#    there on purpose: if the script got as far as the database it would fail for a
#    reason that has nothing to do with the port, and the case would prove nothing.
url="postgres://nobody:nobody@127.0.0.1:1/platformkit?sslmode=disable"
run_gate() { # run_gate — gate 10 with the named port held, its output on stdout, its status as the status
	(cd "$root" && PLATFORMKIT_TEST_ADMIN_URL="$url" PLATFORMKIT_TEST_DATABASE_URL="$url" \
		PLATFORMKIT_E2E_PORT="$held_port" ./scripts/e2e.sh) 2>&1
}
out="$(run_gate || true)"
got=0
run_gate >/dev/null 2>&1 || got=$?
check "gate 10 refuses a named port somebody is listening on" 1 "$got"
case "$out" in
*"already listening on $held_port"*) echo "ok   the refusal names the port it refused" ;;
*)
	echo "FAIL: the refusal does not name $held_port; it said:"
	printf '    %s\n' "$out"
	failures=$((failures + 1))
	;;
esac
case "$out" in
*"a database of its own"* | *"e2e: serving"*)
	echo "FAIL: gate 10 got past the port and into the run; it said:"
	printf '    %s\n' "$out"
	failures=$((failures + 1))
	;;
*) echo "ok   the refusal happens before a database is created or a binary is built" ;;
esac

# 4b. The two ways the attribution probe refuses are two, because the faults are two
#     machines' and not one. A socket table that names no process and a toolchain that
#     starts no listener stop the run at the same line of scripts/e2e.sh, and the
#     person fixing it goes to a different place for each, so the refusal has to say
#     which was met. The listener here is modelled as a node that answers nothing and
#     exits — a toolchain that will not hold a socket — and the socket table as the
#     same silence case 6 models for the device journey's job.
broken="$(mktemp -d)"
mute="$(mktemp -d)"
printf '#!/bin/sh\nexit 0\n' >"$broken/node"
printf '#!/bin/sh\nexit 0\n' >"$mute/ss"
printf '#!/bin/sh\nexit 1\n' >"$mute/lsof"
chmod +x "$broken/node" "$mute/ss" "$mute/lsof"
got=0
( PATH="$broken:$PATH" port_attribution_works ) >/dev/null 2>&1 || got=$?
check "a toolchain that starts no listener is refused as the toolchain's fault" 2 "$got"
got=0
( PATH="$mute:$PATH" port_attribution_works ) >/dev/null 2>&1 || got=$?
check "a listener that is up and unattributed is refused as the socket table's" 1 "$got"
out=""
got=0
out="$(cd "$root" && PATH="$broken:$PATH" PLATFORMKIT_TEST_ADMIN_URL="$url" PLATFORMKIT_TEST_DATABASE_URL="$url" ./scripts/e2e.sh 2>&1)" || got=$?
check "gate 10 refuses a toolchain that starts no listener (exit $got)" 1 "$got"
case "$out" in
*"no listener could be started"*)
	echo "ok   that refusal names the listener it could not start"
	;;
*)
	echo "FAIL: gate 10 did not say a listener could not be started; it said:"
	printf '    %s\n' "$out"
	failures=$((failures + 1))
	;;
esac
case "$out" in
*"cannot say which process is listening"*)
	echo "FAIL: gate 10 blamed the socket table for a toolchain that never started a listener; it said:"
	printf '    %s\n' "$out"
	failures=$((failures + 1))
	;;
*) echo "ok   the refusal does not blame ss or lsof for a toolchain that started no listener" ;;
esac
case "$out" in
*"a database of its own"* | *"serving on"* | *"Listing tests:"*)
	echo "FAIL: gate 10 went past the probe it could not answer; it said:"
	printf '    %s\n' "$out"
	failures=$((failures + 1))
	;;
*) echo "ok   that refusal happens before a database is created or a binary is built" ;;
esac
# The refusal a socket table owes is asked for the same three things, because the pair
# was split and only one of the two lines carried the statement of what the run did not
# go on to do. A claim about both refusals is worth nothing until each has been asked on
# its own.
toolchain_refusal="$out"
out=""
got=0
out="$(cd "$root" && PATH="$mute:$PATH" PLATFORMKIT_TEST_ADMIN_URL="$url" PLATFORMKIT_TEST_DATABASE_URL="$url" ./scripts/e2e.sh 2>&1)" || got=$?
check "gate 10 refuses a socket table that names no process (exit $got)" 1 "$got"
case "$out" in
*"cannot say which process is listening"*)
	echo "ok   that refusal names the socket table"
	;;
*)
	echo "FAIL: gate 10 did not say the socket table was the fault; it said:"
	printf '    %s\n' "$out"
	failures=$((failures + 1))
	;;
esac
case "$out" in
*"no listener could be started"*)
	echo "FAIL: gate 10 blamed the toolchain for a socket table that was never able to answer; it said:"
	printf '    %s\n' "$out"
	failures=$((failures + 1))
	;;
*) echo "ok   the refusal does not blame node for a socket table that names no process" ;;
esac
case "$out" in
*"a database of its own"* | *"serving on"* | *"Listing tests:"*)
	echo "FAIL: gate 10 went past the socket table it could not read; it said:"
	printf '    %s\n' "$out"
	failures=$((failures + 1))
	;;
*) echo "ok   that refusal happens before a database is created or a binary is built" ;;
esac
# One statement per refusal, asked of both. A run that stops here has built no binary,
# created no database and dispatched no browser, and the line a person reads is the last
# thing they see before they go and fix the machine, so it has to carry that by itself.
missing=0
for refusal in "$toolchain_refusal" "$out"; do
	case "$refusal" in
	*"Nothing was built, no database was created and no browser was dispatched"*) : ;;
	*)
		missing=$((missing + 1))
		echo "FAIL: a refusal named no part of the run it stopped; it said:"
		printf '    %s\n' "$refusal"
		failures=$((failures + 1))
		;;
	esac
done
if [ "$missing" -eq 0 ]; then
	echo "ok   each refusal says the run built nothing, created no database and dispatched no browser"
fi

# The third machine that cannot answer is neither of those two: a TMPDIR with nowhere
# private for the listener to write the port it was given. It reaches gate 10 through the
# same exit as the broken toolchain, so it has to print its own reason, and print it
# before node is started at all — a sentence about a directory arriving with a toolchain
# nobody asked for sends the person to node. The node here counts its own starts instead
# of failing them, so "never reached" is a count of zero rather than an inference from
# silence. The sentence is judged on the line the script itself printed, because mktemp
# reports the same directory in its own diagnostic and would answer the question for it.
nowhere="$(mktemp -d)"
starts="$nowhere/node-starts"
printf '#!/bin/sh\nprintf 1 >>"${PK_NODE_STARTS:-/dev/null}"\nexit 0\n' >"$nowhere/node"
chmod +x "$nowhere/node"
out=""
got=0
out="$( (TMPDIR="$nowhere/no-such-place" PK_NODE_STARTS="$starts" PATH="$nowhere:$PATH" port_attribution_works) 2>&1)" || got=$?
check "a TMPDIR that cannot be written is refused before the toolchain is asked for a listener (exit $got)" 2 "$got"
if [ -e "$starts" ]; then
	echo "FAIL: the probe started node with nowhere private to write its port; the refusal a person reads is about their directory and the run handed them a toolchain they did not ask for"
	failures=$((failures + 1))
fi
refusal="$(printf '%s\n' "$out" | grep '^free port:' || true)"
case "$refusal" in
*"$nowhere/no-such-place"*) echo "ok   that refusal names the place it could not write" ;;
*)
	echo "FAIL: the refusal did not name the directory it could not write into; it said:"
	printf '    %s\n' "$out"
	failures=$((failures + 1))
	;;
esac
case "$refusal" in
*TMPDIR*) echo "ok   it names the setting that has to change" ;;
*)
	echo "FAIL: the refusal named no step the person can take; it said:"
	printf '    %s\n' "$out"
	failures=$((failures + 1))
	;;
esac
out=""
got=0
out="$(cd "$root" && TMPDIR="$nowhere/no-such-place" PK_NODE_STARTS="$starts" PATH="$nowhere:$PATH" \
	PLATFORMKIT_TEST_ADMIN_URL="$url" PLATFORMKIT_TEST_DATABASE_URL="$url" ./scripts/e2e.sh 2>&1)" || got=$?
check "gate 10 refuses a TMPDIR it cannot write (exit $got)" 1 "$got"
if [ -e "$starts" ]; then
	echo "FAIL: gate 10 started node with no private place to write to; the fault is the directory and the run said toolchain"
	failures=$((failures + 1))
fi
case "$out" in
*"a database of its own"* | *"serving on"* | *"Listing tests:"*)
	echo "FAIL: gate 10 got past the place it could not write; it said:"
	printf '    %s\n' "$out"
	failures=$((failures + 1))
	;;
*) echo "ok   that refusal happens before a database is created or a binary is built" ;;
esac
rm -rf "$broken" "$mute" "$nowhere"

# 5. The browser run is told which port the application actually got. These two
#    greps are the whole of that contract, and it is a contract and not a detail:
#    e2e/session-recovery.spec.ts compares the URL's port against PLATFORMKIT_E2E_PORT
#    and refuses its journeys on any other address, so a run that moved the port
#    without saying so would lose those journeys, and a run that told the app one
#    port and the browser another would have the suite drive somebody else's.
#    The same trust hangs off the answer to 3a: gate 10 must ask who owns the socket
#    before it serves a browser through the port, and it must ask its own process
#    first, because an application that lost the bind answers nothing while a
#    stranger's listener answers everything. These three greps are that order, and
#    they are what an earlier shape of the script failed: it took the first 200 on
#    /health as proof of serving and drove ninety-odd journeys against somebody
#    else's listener (the program's review of 2026-10-04).
got=0
grep -qF 'owners="$(port_listeners "$port")"' "$root/scripts/e2e.sh" || got=$?
check "gate 10 asks the operating system whose socket answers before it names the port to Playwright" 0 "$got"
got=0
live="$(grep -nF 'kill -0 "$app_pid"' "$root/scripts/e2e.sh" | head -1 | cut -d: -f1)"
asked="$(grep -nF 'curl -fsS "http://localhost:$port/health"' "$root/scripts/e2e.sh" | head -1 | cut -d: -f1)"
if [ -n "$live" ] && [ -n "$asked" ] && [ "$live" -lt "$asked" ]; then
	: ; else got=1; fi
check "gate 10 asks its own process before it trusts the answer" 0 "$got"
got=0
grep -qF 'port_attribution_works' "$root/scripts/e2e.sh" || got=$?
check "gate 10 refuses on a machine that cannot say whose listener it is" 0 "$got"

got=0
grep -qF 'PLATFORMKIT_E2E_PORT="$port"' "$root/scripts/e2e.sh" || got=$?
check "gate 10 exports the port it chose, from the same variable as the URL it exports" 0 "$got"
got=0
grep -qF 'PLATFORMKIT_E2E_URL="http://localhost:$port"' "$root/scripts/e2e.sh" || got=$?
check "gate 10 points Playwright at that same port" 0 "$got"
if grep -qE 'PLATFORMKIT_E2E_PORT:-[0-9]' "$root/scripts/e2e.sh"; then
	echo "FAIL: gate 10 still assumes a port of its own; that answer belongs to the kernel"
	failures=$((failures + 1))
else
	echo "ok   gate 10 carries no literal default port"
fi

# 6. And the machine that runs these cases has to have been given the tool. 3a and 3c are asked of ss
#    or lsof, and the CI jobs that ask them run on images that ship neither: ci.yml's is measured in the
#    comment over its install line, and the device journey's job runs scripts/mobile_e2e.sh, whose
#    wait_healthy reads the socket table to decide whether a /health answer came from the application it
#    started. The two jobs differ in what an unanswered question costs. Gate 10 refuses its whole run on
#    a machine that cannot attribute a listener; the device journey carries no such refusal of its own
#    and prints an accusation about somebody else owning its port. So both jobs buy the tool ahead of
#    the first step that needs an answer, and the journey's job is asked the question rather than told
#    the answer: its probe step is run below, as the job runs it, against socket tables that answer in
#    each of the three ways a machine can answer.
for wf in ci mobile; do
	wfpath="$root/.gitea/workflows/$wf.yml"
	installed="$(grep -nE 'apt-get install' "$wfpath" | head -1 | cut -d: -f1 || true)"
	asks="$(grep -nE '^[[:space:]]+run: make (check|check-e2e-guards|e2e|mobile-e2e)$' "$wfpath" | head -1 | cut -d: -f1 || true)"
	if [ -z "$installed" ] || [ -z "$asks" ]; then
		echo "FAIL: $wf.yml must install the probe and run a step that needs it (install line '$installed', first step that asks '$asks')"
		failures=$((failures + 1))
	elif ! sed -n "${installed}p" "$wfpath" | grep -qE '(^|[[:space:]])(iproute2|lsof)([[:space:]]|$)'; then
		echo "FAIL: $wf.yml installs no tool that can say whose socket it is, so a journey it serves refuses; the line reads:"
		printf '    %s\n' "$(sed -n "${installed}p" "$wfpath")"
		failures=$((failures + 1))
	elif [ "$installed" -ge "$asks" ]; then
		echo "FAIL: $wf.yml installs the socket probe at line $installed, at or after the step that needs an answer at $asks"
		failures=$((failures + 1))
	else
		echo "ok   $wf.yml installs a socket probe (line $installed) ahead of the first step that asks whose listener it is ($asks)"
	fi
done

# The device journey's job then gets its own receipt run here: the workflow step that asks the socket
# table about a listener the step started itself is extracted and executed the way the job executes it,
# and against two tables that cannot answer. A stripped `ss` which exits 0 printing nothing is the
# shape a container with no readable /proc takes, and port_listeners answers that successfully with an
# empty list — so the question is never whether the probe succeeded, only whether it named the pid.
# Both silences have to refuse: a journey whose runner cannot attribute a socket is refused by
# scripts/mobile_e2e.sh for somebody else's listener, which is the wrong thing to accuse it of.

# Two questions are asked of that step, and the first is not about its commands. The extraction below
# finds the body wherever the job writes it, so a step put behind an `if:` or given
# `continue-on-error: true` — lines that keep a job from ever running it, or from minding that it
# refused — would leave every verdict here green over a journey that stopped asking the question. So
# the step's own mapping keys are read first: the receipt runs unconditionally, and its refusal ends
# the job.
probe_body() { # probe_body <workflow> — the receipt step's command lines, dedented, as the job runs them
	awk '
		/^      - name: The socket probe/ { keep = 1; next }
		keep && /^      - / { exit }
		keep && /^        run: \|$/ { inrun = 1; next }
		keep && inrun { sub(/^          /, ""); print }
	' "$1"
}
# probe_weakening <workflow> — the step's own keys that can skip it or forgive it, one per line, and
# nothing when it runs on every journey and a refusal there stops the job. A step-level key sits at
# eight spaces, one fewer than the `run: |` block beneath it, so no line of the step's own script can
# answer here. Any `if:` is a condition under which the receipt does not run, so it is refused;
# `continue-on-error: false` is the same promise said in the workflow's own words, so it is allowed
# — a guard that refuses harmless lines is edited out rather than obeyed.
probe_weakening() {
	awk '
		/^      - name: The socket probe/ { keep = 1; next }
		keep && /^      - / { exit }
		keep && /^        if:/ { sub(/^        /, ""); print }
		keep && /^        continue-on-error:/ {
			value = $0
			sub(/^        continue-on-error:[[:space:]]*/, "", value)
			if (value != "false") { sub(/^        /, ""); print }
		}
	' "$1"
}
weakened_copy() { # weakened_copy <file> <field> — this workflow, with one step-level field under the name
	local field="$2"
	awk -v extra='        '"$field" '
		{ print }
		/^      - name: The socket probe/ { print extra }
	' "$root/.gitea/workflows/mobile.yml" >"$1"
}
probe_step="$(probe_body "$root/.gitea/workflows/mobile.yml")"
if [ -z "$probe_step" ]; then
	echo "FAIL: mobile.yml carries no step that asks the socket table about a listener of its own, so the journey's job trusts an answer nobody proved it could give"
	failures=$((failures + 1))
else
	declared="$(probe_weakening "$root/.gitea/workflows/mobile.yml")"
	if [ -n "$declared" ]; then
		echo "FAIL: mobile.yml's journey does not run its own receipt step, or does not stop when it refuses; the step carries:"
		printf '    %s\n' "$declared"
		failures=$((failures + 1))
	else
		echo "ok   mobile.yml runs its receipt step on every journey and ends the job when it refuses"
	fi
	got=0
	(cd "$root" && bash -e -c "$probe_step" >/dev/null 2>&1) || got=$?
	check "the journey's job attributes a listener it started itself" 0 "$got"

	silent="$(mktemp -d)"
	printf '#!/bin/sh\nexit 0\n' >"$silent/ss"
	printf '#!/bin/sh\nexit 1\n' >"$silent/lsof"
	chmod +x "$silent/ss" "$silent/lsof"
	got=0
	(cd "$root" && PATH="$silent:$PATH" bash -e -c "$probe_step" >/dev/null 2>&1) || got=$?
	if [ "$got" = 0 ]; then
		echo "FAIL: the journey's job passed with a socket probe that answers nothing, which is the silence a device run reads as somebody else owning its port"
		failures=$((failures + 1))
	else
		echo "ok   the journey's job refuses a probe that answers nothing (exit $got)"
	fi

	bare="$(mktemp -d)"
	for tool in bash grep sed sort head sleep mktemp python3 rm; do
		here="$(command -v "$tool" 2>/dev/null)" || continue
		ln -s "$here" "$bare/$tool"
	done
	got=0
	(cd "$root" && PATH="$bare" bash -e -c "$probe_step" >/dev/null 2>&1) || got=$?
	if [ "$got" = 0 ]; then
		echo "FAIL: the journey's job passed on a runner with neither ss nor lsof, which is the image the guard exists for"
		failures=$((failures + 1))
	else
		echo "ok   the journey's job refuses a runner with no socket probe at all (exit $got)"
	fi
	# Both ways to unrun the receipt are planted in a copy of the workflow, one field under the step's
	# name, and asked of the guard above. Each copy has to keep the commands that just passed and lose
	# only the declaration, so a refusal is the declaration's and not a step whose body went missing.
	weakened="$(mktemp -d)"
	for field in 'if: ${{ false }}' 'continue-on-error: true'; do
		weakened_copy "$weakened/mobile.yml" "$field"
		weakening="$(probe_weakening "$weakened/mobile.yml")"
		if [ -z "$weakening" ]; then
			echo "FAIL: a receipt step carrying '$field' still answered that the journey runs it and stops when it refuses"
			failures=$((failures + 1))
		elif [ "$(probe_body "$weakened/mobile.yml")" != "$probe_step" ]; then
			echo "FAIL: the copy carrying '$field' lost the step's commands with its declaration, so it tests the extraction and not the policy"
			failures=$((failures + 1))
		else
			echo "ok   a receipt step given '$field' is refused ($weakening)"
		fi
	done
	# And the same promise said out loud has to answer what the file already answered, because without
	# this third copy the two refusals above could be a guard that refuses every line under a step. The
	# comparison is with that answer rather than with silence, so a workflow refused above stays refused
	# here for the reason above and not for a second one invented by the copy.
	weakened_copy "$weakened/mobile.yml" 'continue-on-error: false'
	if [ "$(probe_weakening "$weakened/mobile.yml")" != "$declared" ]; then
		echo "FAIL: a receipt step whose own line says its failure stops the job answers differently from the same step without that line, so the guard above reads no field at all"
		failures=$((failures + 1))
	else
		echo "ok   a receipt step that says its failure stops the job is read as the promise itself"
	fi
	rm -rf "$silent" "$bare" "$weakened"
fi

if [ "$failures" -ne 0 ]; then
	echo "free port: $failures case(s) failed" >&2
	exit 1
fi
echo "free port: an allocation is bindable, a held port is refused before anything is touched, the listener is named by pid before the answer is trusted, the browser is told which port it got, the CI jobs install the tool that answers, and the journey runs its own receipt on every run"
