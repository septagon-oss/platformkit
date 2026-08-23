module github.com/septagon-oss/platformkit

go 1.26

require (
	github.com/jackc/pgx/v5 v5.10.0
	github.com/septagon-oss/pk-apps v0.15.0
	github.com/spf13/cobra v1.10.2
	modernc.org/sqlite v1.54.0
)

require (
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/pprof v0.0.0-20260709232956-b9395ee17fa0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/septagon-oss/pk-core v0.1.0 // indirect
	github.com/septagon-oss/pk-design v0.3.0 // indirect
	github.com/septagon-oss/pk-modules v0.18.0 // indirect
	github.com/septagon-oss/pk-runtime v0.1.0 // indirect
	github.com/septagon-oss/pk-shared v0.2.0 // indirect
	github.com/septagon-oss/pk-ui v0.3.0 // indirect
	github.com/septagon-oss/styleengine v0.1.0 // indirect
	github.com/septagon-oss/tw v0.2.2 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/tdewolff/minify/v2 v2.24.14 // indirect
	github.com/tdewolff/parse/v2 v2.8.14 // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	golang.org/x/tools v0.48.0 // indirect
	maragu.dev/gomponents v1.3.0 // indirect
	modernc.org/libc v1.74.3 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)

// v0.4.0, v0.5.0, and v0.5.1 were withdrawn: their history was rewritten to
// correct commit attribution, so those tags no longer resolve to the content the
// module proxy recorded. v0.6.0 is the same code with clean history.
retract [v0.4.0, v0.5.1]
