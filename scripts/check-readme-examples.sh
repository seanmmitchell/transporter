#!/usr/bin/env bash
# Builds and vets every ```go block in README.md as its own program against
# this checkout, so the documentation cannot drift from the API.
# Each block must be a complete `package main` program.
set -euo pipefail

# Ignore any go.work in a parent of the temp dir (or GOWORK in the caller's
# environment): the examples must build as their own module.
export GOWORK=off

root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# awk writes each block to $work/exN.go; the path reaches awk through the
# environment, never through a shell or awk's escape processing.
WORK="$work" awk '
	/^```go[[:space:]]*$/ { n++; inblock = 1; out = ENVIRON["WORK"] "/ex" n ".go"; next }
	/^```[[:space:]]*$/   { if (inblock) close(out); inblock = 0; next }
	inblock               { print > out }
' "$root/README.md"

count=0
for src in "$work"/ex*.go; do
	[ -e "$src" ] || continue
	dir="${src%.go}"
	mkdir "$dir"
	mv "$src" "$dir/main.go"
	count=$((count + 1))
done
if [ "$count" -eq 0 ]; then
	echo "no Go examples found in README.md" >&2
	exit 1
fi

cd "$work"
printf 'module readmeexamples\n\ngo 1.22\n\nrequire github.com/seanmmitchell/transporter/v2 v2.0.0\n' > go.mod
# go mod edit quotes the path correctly whatever characters it contains.
go mod edit -replace "github.com/seanmmitchell/transporter/v2=$root"
go mod tidy
go vet ./...
go build ./...
echo "README examples OK: built and vetted $count program(s)"
