#!/usr/bin/env bash
# Rewrite the secrets a run prints into a marker, so its transcript can be kept.
#
# `start` prints the first administrator's password once and stores nothing, so
# any capture of that first run — a timing receipt, a log, a recorded journey —
# holds a credential until it passes through here. It reads stdin, or the files
# named as arguments, and writes stdout, and it is deliberately blunt: three
# shapes are rewritten whole, and a line that only looks like one of them loses
# its value too. Over-redacting costs a line of a log; under-redacting publishes
# a password. Named files are read whether or not they can be opened, so a
# missing argument is an error rather than an empty redacted transcript.
#
# The shapes: the banner's own `sign in as … with password …` sentence, which is
# the line `start` prints and nothing else writes; a `password:`-keyed line of a
# seed report or a config dump; a JSON `"password": "…"` pair of a request body;
# and the credentials inside a `postgres://user:secret@host` string.
set -euo pipefail

redact=(
	-E
	-e 's#(postgres(ql)?://[^/@:]*:)[^@]*@#\1[redacted]@#g'
	-e 's/(with[[:space:]]+password[[:space:]]+)[^[:space:]]+/\1[redacted]/Ig'
	-e 's/("password"[[:space:]]*:[[:space:]]*)"[^"]*"/\1"[redacted]"/g'
	-e 's/^([[:space:]]*password[^:]*:).*/\1 [redacted]/I'
)

if [ "$#" -gt 0 ]; then
	exec sed "${redact[@]}" "$@"
fi
exec sed "${redact[@]}"
