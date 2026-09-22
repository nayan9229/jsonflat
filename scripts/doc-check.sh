#!/usr/bin/env bash
# doc-check.sh
#
# The documentation checks that go vet and staticcheck do not do:
# 1. go doc renders every package (go doc takes one package at a time);
# 2. every exported identifier has a doc comment that starts with its name,
#    and every package has a package comment (internal/doccheck);
# 3. the pinned tool versions are the same in both workflows and the Makefile,
#    so a bump in one place cannot be forgotten in another.
set -euo pipefail

for pkg in $(go list ./...); do
	go doc -all "$pkg" >/dev/null
done

go run ./internal/doccheck ./...

tools() { grep -hoE 'run [a-z][^ ]*@[^ ]+' "$1" | sort -u; }
for f in .github/workflows/release.yml Makefile; do
	if ! diff <(tools .github/workflows/ci.yml) <(tools "$f") >/dev/null; then
		echo "::error::pinned tool versions differ between .github/workflows/ci.yml and $f:" >&2
		diff <(tools .github/workflows/ci.yml) <(tools "$f") >&2 || true
		exit 1
	fi
done

echo "ok: docs render, exported identifiers are documented, tool versions agree"
