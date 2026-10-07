#!/usr/bin/env bash
# Builds and vets every ```go block in README.md as its own program against
# this checkout, so the documentation cannot drift from the API.
# Each block must be a complete `package main` program.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

awk -v dir="$work" '
	/^```go[[:space:]]*$/ { n++; inblock = 1; out = dir "/ex" n "/main.go"; system("mkdir -p \"" dir "/ex" n "\""); next }
	/^```[[:space:]]*$/   { inblock = 0; next }
	inblock               { print > out }
' "$root/README.md"

count="$(find "$work" -mindepth 1 -maxdepth 1 -type d -name 'ex*' | wc -l | tr -d ' ')"
if [ "$count" -eq 0 ]; then
	echo "no Go examples found in README.md" >&2
	exit 1
fi

printf 'module readmeexamples\n\ngo 1.22\n\nrequire github.com/seanmmitchell/transporter/v2 v2.0.0\n\nreplace github.com/seanmmitchell/transporter/v2 => %s\n' "$root" > "$work/go.mod"

cd "$work"
go mod tidy
go vet ./...
go build ./...
echo "README examples OK: built and vetted $count program(s)"
