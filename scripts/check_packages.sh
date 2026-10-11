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
#
# ui/document and ui/resource are the database-free cores of the page and
# screen layers: a document is values, a screen is a schema plus rows, and
# neither reaches kit/db, net/http or a module. ui/page and ui/screens are their
# adapters and are gated at the adapter closure: through kit/httpx they reach
# kit/db, database/sql and net/http (the request context and its transaction)
# and, through kit/module's manifest types, kit/events and kit/jobs. Recording
# both boundaries is what refuses growth — ui/export, ui/source, a module's
# internals — in either; the "web" mode is the one that admits both
# database/sql and net/http.
#
# A package whose own boundary check() asserts below belongs in this list even
# when nothing here needs it for a closure: go list reports metadata for what it
# was asked about, and check() refuses an assertion it cannot measure as
# "missing dependency metadata". Measuring a core through whatever reaches it
# would leave the assertion resting on a caller that may stop calling tomorrow.
parts=(kit/wire kit/entity kit/entity/display kit/locale kit/fault kit/flags kit/tenancy kit/trace kit/appname kit/request modules/task/domain design ui/forms
    ui/document ui/resource ui/page ui/screens
    kit/cache kit/cache/providers/valkey kit/app kit/events kit/events/transport kit/events/providers/memory kit/events/providers/nats
    kit/tenancy/providers/topaz kit/flags/providers/openfeature
    kit/flags/providers/ofrep kit/locale/providers/xtext pkit)
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
            # "trace" is kit/events: the W3C carrier is an interface over
            # net/http.Header, so propagating a trace context reaches net/http while
            # this package still opens no request and no connection.
            if (dep == "database/sql" && mode != "sql" && mode != "web" && mode != "trace") bad = 1
            if (dep ~ /^net\/http(\/|$)/ && mode != "provider" && mode != "web" && mode != "trace") bad = 1
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
        # kit/appname is a value package beside kit/trace: its closure is the
        # standard library and the UUID type a tenant id already is. It is in the
        # delivery group because that is where an address is formed — subject,
        # filter, durable — and every other name two apps could share (cookie,
        # job lock, limit bucket, stored path) is formed by the same grammar. A
        # package that reaches it reaches only values; nothing reaches back.
        appname = p "kit/appname"
        delivery = p "kit/events/transport " p "kit/events/internal/delivery " appname
        sql = uuid " github.com/jackc/pgpassfile github.com/jackc/pgservicefile github.com/jackc/pgx/v5 github.com/jackc/puddle/v2 github.com/jinzhu/inflection github.com/jinzhu/now golang.org/x/sync golang.org/x/text gorm.io/driver/postgres gorm.io/gorm"
        # kit/trace is in the outbox bound and the kernel one because the trace
        # context of a request is stored with the event the request caused, and
        # the relay hands it to the envelope: an event that could not name the
        # call behind it would be a log line no event joins to.
        # kit/request is beside kit/trace for the same reason: the call that caused
        # an event is stored with the event, because by relay time the request is
        # gone. It is a value package — the standard library and kit/trace — and
        # it parses no request, which is why net/http stays out of this closure and
        # why the HTTP middleware is the only writer of these fields.
        outbox = identity " " delivery " " p "kit/db " p "kit/trace " p "kit/request"
        # The recorded closure of the page composition layer (see the comment above parts).
        # kit/fault sits beside kit/crud because the adapter names the three refusals
        # through it: whatever reaches the adapter reaches the values it re-exports, and
        # the bound that matters is the other direction, refused by check("kit/fault", "").
        # A typed rich-text field reaches its parser and sanitizer through
        # kit/httpx. Keep this list explicit so a new renderer dependency is
        # visible at the page, screen and runner boundaries.
        richtextDeps = p "kit/richtext github.com/aymerick/douceur/css github.com/aymerick/douceur/parser github.com/gorilla/css/scanner golang.org/x/net/html golang.org/x/net/html/atom"
        richtext = p "kit/richtext github.com/yuin/goldmark github.com/yuin/goldmark/ast github.com/yuin/goldmark/extension github.com/yuin/goldmark/extension/ast github.com/yuin/goldmark/parser github.com/yuin/goldmark/renderer github.com/yuin/goldmark/renderer/html github.com/yuin/goldmark/text github.com/yuin/goldmark/util github.com/microcosm-cc/bluemonday github.com/microcosm-cc/bluemonday/css github.com/aymerick/douceur/css github.com/aymerick/douceur/parser github.com/gorilla/css/scanner golang.org/x/net/html golang.org/x/net/html/atom"
        # The OpenTelemetry API, and nothing above it: the API is what a package
        # makes a span or records a number with, and it drags no exporter, no
        # provider and no transport with it. The exporters, the SDK and gRPC belong
        # to kit/app alone — the one package the runtime brief names as the home
        # of a TracerProvider and a MeterProvider — which is why `providers` is the
        # only list that admits them.
        otel = "go.opentelemetry.io/otel go.opentelemetry.io/otel/metric go.opentelemetry.io/otel/trace go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp go.opentelemetry.io/auto/sdk github.com/go-logr/logr github.com/go-logr/stdr github.com/cespare/xxhash/v2 github.com/felixge/httpsnoop"
        # kit/trace and kit/telemetry both sit in the kernel list: the first carries
        # the W3C trace context a caller sent as a value, the second names the
        # vocabulary of a span and a number. Neither owns an exporter or a provider.
        kernel = p "kit/config " p "kit/cache " identity " " p "kit/trace " p "kit/db " p "kit/entity " p "kit/crud " p "kit/fault " p "kit/problem " p "kit/httpx " p "kit/locale " p "kit/locale/providers/xtext " outbox " " p "kit/events " p "kit/jobs " p "kit/module " p "kit/telemetry " richtextDeps
        presentation = p "design " p "ui/css " p "ui/icon " p "ui/style " p "ui/components " p "ui/components/examples " p "ui " p "ui/document"
        markup = "maragu.dev/gomponents maragu.dev/gomponents/html"
        web = sql " github.com/danielgtaylor/huma/v2 github.com/go-chi/chi/v5 gopkg.in/yaml.v3 maragu.dev/gomponents github.com/robfig/cron/v3 " otel " " richtext
        # The provider edge for measurement: kit/app is the only package whose
        # closure may hold an exporter, an SDK or a collector transport. A span
        # anywhere else in the kernel reaches the collector through the global, so
        # nothing else needs these, and a second package that could install a
        # provider is a second answer to where the traces went.
        measurement = otel " go.opentelemetry.io/otel/sdk go.opentelemetry.io/otel/sdk/metric go.opentelemetry.io/otel/exporters/otlp/otlptrace go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc go.opentelemetry.io/otel/exporters/otlp/internal google.golang.org/grpc google.golang.org/protobuf github.com/cenkalti/backoff/v5 github.com/grpc-ecosystem/grpc-gateway/v2 golang.org/x/net golang.org/x/sys golang.org/x/text google.golang.org/genproto/googleapis/api google.golang.org/genproto/googleapis/rpc go.opentelemetry.io/proto/otlp"
        check("kit/entity", uuid)
        check("kit/entity/display", uuid " " p "kit/entity")
        check("kit/locale", "")
        # kit/fault declares the three refusal sentinels a value package wraps.
        # Its closure is the standard library and itself, or wrapping a refusal
        # would link the storage adapter to a package that takes no transaction.
        check("kit/fault", "")
        check("kit/wire", "")
        check("kit/flags", uuid)
        check("kit/tenancy", uuid " " p "kit/internal/syscap")
        # The W3C trace context is a value: the standard library and nothing
        # else. It is a carrier, not a tracer, and its closure is the proof.
        check("kit/trace", "")
        # The one door for shared names: stdlib and UUID, or the package that is
        # supposed to name nothing but a slug would be holding a runner.
        check("kit/appname", uuid)
        # Which call, from where, on which trace: a value the request leaves
        # behind, so the standard library and kit/trace and nothing else. Reading
        # net/http here would put a server in the closure of every worker.
        check("kit/request", p "kit/trace")
        # kit/cache is the value every replica reads: the standard library, uuid and
        # singleflight. A store it talks to is a provider, not this core.
        check("kit/cache", uuid " golang.org/x/sync/singleflight")
        # The one store kit/cache speaks to: the port package and one client, and
        # the four commands the adapter issues are asserted in its own test rather
        # than here — this line is what the provider may link, which is the only
        # direction the compiler cannot refuse. kit/appname rides in with kit/config:
        # the config decodes its own app slug through the door, so a provider that
        # reads configuration reaches the slug values and nothing else.
        check("kit/cache/providers/valkey", p "kit/cache " p "kit/config " appname,
            uuid " github.com/redis/go-redis/v9 github.com/cespare/xxhash/v2 go.uber.org/atomic golang.org/x/sync golang.org/x/sys gopkg.in/yaml.v3", "provider")
        check("modules/task/domain", "")
        check("design", "")
        check("ui/forms", uuid " " p "kit/entity " p "design " p "ui/icon " p "ui/css " p "ui/style " p "ui/components " p "ui/components/examples " markup)
        check("ui/document", p "kit/locale " presentation " " markup)
        check("ui/resource", uuid " " p "kit/entity " p "kit/entity/display " p "kit/locale " presentation " " p "ui/forms " markup)
        check("ui/page", kernel " " presentation, web, "web")
        check("ui/screens", kernel " " presentation " " p "kit/rest " p "kit/entity/display " p "ui/forms " p "ui/page " p "ui/resource", web, "web")
        # The kernel runner selects a transport by name and builds none: neither
        # provider package is in its closure.
        #
        # kit/limit is here because the limit on anonymous public writes is counted
        # on the connection the request already holds, and handing that connection
        # over is the runner role to do. The kernel names the shape it wants
        # (httpx.WriteLimiter, one method wide) and the runner chooses where the
        # count is stored, exactly as it chooses an event transport. The
        # presentation packages do not inherit the dependency, which is why the
        # interface is declared by the consumer instead of imported here.
        check("kit/app", kernel " " p "kit/health " p "kit/limit " p "kit/telemetry " p "migrations", web " " measurement, "web")
        # pkit is the composition vocabulary and the builder, so it is allowed the
        # runner it drives and the design tokens it hands a composition, and
        # nothing else. The falsifiable part is what is absent: no module package
        # and no ui package appears in this closure. That is rule 6 of 0074, "core
        # stays core", read off the link map instead of argued from a reading of
        # the sources. pkit may not reach modules/, because a composition that
        # imported the internals of a module could read a type the resolver never
        # asked for, and may not reach ui/, because kit may not import ui at all,
        # which is why AskForAccess and WorkspaceCatalog are mounts a composition
        # hands in (pkit/skin.go). A bound a person can check with one command
        # outlives a comment that asks for trust. Because pkit drives kit/app it
        # links the measurement provider edge kit/app owns — the exporter, the SDK
        # and the collector transport — which is why measurement is in this list
        # beside web; what stays refused is a *second* package that could install a
        # provider, and pkit installs none.
        check("pkit", kernel " " p "kit/app " p "kit/health " p "kit/limit " p "migrations " p "design " p "pkit", web " " measurement, "web")
        check("kit/events/transport", uuid " " appname)
        check("kit/events/providers/memory", uuid " " delivery)
        check("kit/events", outbox " " p "kit/telemetry", sql " " otel, "trace")
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

# The one measurement rule that has to hold everywhere, not only in the packages
# named above: a second package able to build a provider is a second answer to where
# the traces went, and `kit/telemetry/README.md` promises this in words. The closure
# bound above can only speak for what `go list` was asked about, so this speaks for
# every non-test Go file in the tree — the whole-tree form of the same sentence, and
# the one the compiler has no opinion about. Test files are outside it on purpose:
# the doubles this repository measures with are the SDK's own span recorder and
# manual reader, which the package guide names as the reason.
sdk="$(grep -rl --include='*.go' --exclude='*_test.go' \
	-e 'go.opentelemetry.io/otel/sdk' -e 'go.opentelemetry.io/otel/exporters' "$root" 2>/dev/null |
	 sed "s|^$root/||" | grep -v '^kit/app/' || true)"
if [ -n "$sdk" ]; then
	echo "MEASUREMENT BOUNDARY: an OpenTelemetry SDK or exporter is imported outside kit/app, which is the" >&2
	echo "one package that installs the process's TracerProvider and MeterProvider:" >&2
	printf '%s\n' "$sdk" | sed 's/^/  /' >&2
	exit 1
fi

echo "package boundaries: portable cores, design, forms, documents, resources, pages, screens, the runner and selected providers passed"

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
# which exits 1 on no match, is allowed to fail. The answer is a list of import
# paths, so the commit the app would be stamped from is no part of it — and asking
# for one makes this gate depend on the history of a directory above the checkout,
# which is why check_public_api.py and check_public_module.py pass the same flag.
deps="$(cd "$root" && go list -buildvcs=false -deps ./apps/platformkit)"
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
