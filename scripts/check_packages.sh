#!/usr/bin/env bash
# Counts the first-party packages linked into the reference app and fails when
# that count exceeds "packages" in packages-budget.json. Portable cores and
# selected adapters also enforce their transitive runtime dependency boundaries.
#
# Explicit wiring has no dependency-injection channels to count, so package
# count is the gate on composition complexity: every module, kit package and
# helper that main can reach shows up here exactly once.
#
# Without apps/platformkit, portable boundaries still run; only counting is skipped.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
budget_file="$root/packages-budget.json"
write=0

for arg in "$@"; do
	case "$arg" in
	--write) write=1 ;;
	*)
		echo "usage: $(basename "$0") [--write]" >&2
		exit 2
		;;
	esac
done

# Deps is the complete runtime closure, unlike Imports. Tests are deliberately
# excluded: SQL fixtures and adapter conformance tests may need more than core.
parts=(kit/entity kit/locale kit/flags kit/tenancy modules/task/domain design ui/forms
    kit/events kit/events/transport kit/events/providers/memory kit/events/providers/nats
    kit/tenancy/providers/topaz kit/flags/providers/openfeature
    kit/flags/providers/ofrep kit/locale/providers/xtext)
metadata="$(cd "$root" && go list -deps -f '{{.ImportPath}}|{{.Standard}}|{{join .Deps " "}}|{{if .Module}}{{.Module.Path}}{{end}}' "${parts[@]/#/./}")"
printf '%s\n' "$metadata" | awk -F '|' '
    function contains(set, value) { return index(" " set " ", " " value " ") != 0 }
    function check(owner, allowed, modules, mode,    name, total, list, i, dep, bad) {
        name = p owner
        if (!(name in closure)) {
            print "OUT OF BOUNDS: missing dependency metadata for " name > "/dev/stderr"
            failed = 1
            return
        }
        total = split(closure[name], list, " ")
        for (i = 1; i <= total; i++) {
            dep = list[i]
            if (dep == "") continue
            bad = !(dep in standard)
            if (!bad && standard[dep] != "true") {
                bad = !contains(allowed, dep)
                if (index(dep, p) != 1 && module[dep] != "" && contains(modules, module[dep])) bad = 0
            }
            # UUID exposes sql/driver values; that is not a database runner.
            if (dep == "database/sql" && mode != "sql") bad = 1
            if (dep ~ /^net\/http(\/|$)/ && mode != "provider") bad = 1
            if (bad) {
                print "OUT OF BOUNDS: " name " transitively depends on " dep > "/dev/stderr"
                failed = 1
            }
        }
    }
    { standard[$1] = $2; closure[$1] = $3; module[$1] = $4 }
    END {
        p = "github.com/septagon-oss/platformkit/"
        uuid = "github.com/google/uuid"
        identity = p "kit/tenancy " p "kit/internal/syscap"
        delivery = p "kit/events/transport " p "kit/events/internal/delivery"
        sql = uuid " github.com/jackc/pgpassfile github.com/jackc/pgservicefile github.com/jackc/pgx/v5 github.com/jackc/puddle/v2 github.com/jinzhu/inflection github.com/jinzhu/now golang.org/x/sync golang.org/x/text gorm.io/driver/postgres gorm.io/gorm"
        outbox = identity " " delivery " " p "kit/db " p "kit/events/providers/memory"
        check("kit/entity", uuid)
        check("kit/locale", "")
        check("kit/flags", uuid)
        check("kit/tenancy", uuid " " p "kit/internal/syscap")
        check("modules/task/domain", "")
        check("design", "")
        check("ui/forms", uuid " " p "kit/entity " p "design " p "ui/icon " p "ui/css " p "ui/style " p "ui/components maragu.dev/gomponents maragu.dev/gomponents/html")
        check("kit/events/transport", uuid)
        check("kit/events/providers/memory", uuid " " delivery)
        check("kit/events", outbox, sql, "sql")
        check("kit/events/providers/nats", p "kit/config " delivery,
            uuid " github.com/nats-io/nats.go github.com/nats-io/nkeys github.com/nats-io/nuid github.com/klauspost/compress golang.org/x/crypto golang.org/x/sys gopkg.in/yaml.v3", "provider")
        check("kit/tenancy/providers/topaz", identity,
            uuid " github.com/aserto-dev/go-authorizer github.com/grpc-ecosystem/grpc-gateway/v2 golang.org/x/net golang.org/x/sys golang.org/x/text google.golang.org/genproto/googleapis/api google.golang.org/genproto/googleapis/rpc google.golang.org/grpc google.golang.org/protobuf", "provider")
        check("kit/flags/providers/openfeature", p "kit/flags", uuid " github.com/open-feature/go-sdk", "provider")
        check("kit/flags/providers/ofrep", p "kit/flags " p "kit/flags/providers/openfeature",
            uuid " github.com/open-feature/go-sdk github.com/open-feature/go-sdk-contrib/providers/ofrep", "provider")
        check("kit/locale/providers/xtext", p "kit/locale", "golang.org/x/text")
        exit failed
    }
'

echo "package boundaries: portable cores, design, forms and selected providers passed"

if [ ! -d "$root/apps/platformkit" ]; then
	echo "no app yet"
	exit 0
fi

max="$(sed -n 's/.*"packages"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p' "$budget_file")"
if [ -z "$max" ]; then
	echo "$budget_file: no \"packages\" key" >&2
	exit 2
fi

# go list failing must fail the gate, so it runs on its own line; only grep,
# which exits 1 on no match, is allowed to fail.
deps="$(cd "$root" && go list -deps ./apps/platformkit)"
count="$(printf '%s\n' "$deps" | grep -c '^github.com/septagon-oss/platformkit/' || true)"

echo "packages $count / $max"

if [ "$write" -eq 1 ]; then
	if [ "$count" -lt "$max" ]; then
		printf '{"packages": %d}\n' "$count" >"$budget_file"
		echo "packages-budget.json lowered to $count"
	fi
	exit 0
fi

if [ "$count" -gt "$max" ]; then
	echo "OVER BUDGET: $count first-party packages > $max" >&2
	exit 1
fi
