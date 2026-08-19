#!/usr/bin/env bash
# Runs the readability budget: golangci-lint (see .golangci.yml) plus a file
# length check, which golangci-lint has no linter for — every length rule it
# ships measures a function or a single line, never a whole file.
#
# Usage:  ./lint.sh            # report
#         ./lint.sh --fix      # apply what the linters can fix themselves
set -uo pipefail
cd "$(dirname "$0")"

# The one knob that does not live in .golangci.yml. A file past this is
# usually several files that have not been separated yet.
MAX_FILE_LINES=600

fail=0

if ! command -v golangci-lint >/dev/null; then
    # go install puts it in GOPATH/bin, which is not always on PATH.
    if [ -x "$(go env GOPATH)/bin/golangci-lint" ]; then
        PATH="$(go env GOPATH)/bin:$PATH"
    else
        echo "golangci-lint is not installed. Install it with:"
        echo "  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"
        exit 1
    fi
fi

golangci-lint run "$@" || fail=1

# gofmt is not part of golangci-lint's default set, so without this the linter
# passes locally on files CI then rejects. Same check CI runs.
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
    if [ "${1:-}" = "--fix" ]; then
        gofmt -w .
        echo "reformatted:"
        echo "$unformatted"
    else
        printf '%s:1:1: file is not gofmt-ed (run ./lint.sh --fix)\n' $unformatted
        fail=1
    fi
fi

# File length. Tests are exempt: a table-driven test grows with its table,
# and splitting one across files makes it harder to read, not easier.
while IFS= read -r f; do
    n=$(wc -l <"$f" | tr -d ' ')
    if [ "$n" -gt "$MAX_FILE_LINES" ]; then
        printf '%s:1:1: file is %s lines long, which exceeds the maximum of %s (filelen)\n' \
            "$f" "$n" "$MAX_FILE_LINES"
        fail=1
    fi
done < <(find . -name '*.go' -not -name '*_test.go' -not -path './.git/*')

exit $fail
