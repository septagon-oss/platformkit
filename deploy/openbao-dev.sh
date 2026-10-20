#!/bin/sh
# Start the dev-mode OpenBao compose.yaml describes, wait for its API to answer, mount
# the two engines kit/secrets' OpenBao adapter speaks through, and then stay up as long
# as the server does.
#
# The two mounts are an operator's act in a real deployment and this script's act here,
# which is the only place a development stack can make them: config.Secrets defaults to
# `secret` for KV v2 and `transit` for the key engine, and a container that came up
# without the second would answer every sealing call with "no mount point", which a
# reader would take for a broken adapter. Dev mode mounts KV v2 by itself, so the mount
# table is read before either enable rather than trusting an exit code that says
# "path is already in use" for the one that is.
#
# Args are the server's own arguments, which compose.yaml passes as `command:` so the
# flags a person reads stay beside the image they run against. Replacing the image's
# entrypoint means naming its binary here: `bao` is what the image would have run.
set -eu

bao "$@" &
server=$!

# `bao status` over the API, not a probe of the port: the listener is up before the
# unsealed server can serve, and a mount issued too early answers nothing at all.
timeout 60 sh -c 'until bao status -format=json >/dev/null 2>&1; do sleep 0.5; done'

for mount in secret transit; do
	if ! bao secrets list -format=json | grep -q "\"$mount/\""; then
		case $mount in
		secret) bao secrets enable -path=secret kv-v2 ;;
		transit) bao secrets enable -path=transit transit ;;
		esac
	fi
done

# The container's lifetime is the server's: whatever stops it should stop this one, and
# a mount this script made that outlived the server would outlive its own memory too.
trap 'kill "$server" 2>/dev/null || true' TERM INT
wait "$server"
