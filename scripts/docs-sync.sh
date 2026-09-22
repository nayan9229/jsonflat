#!/usr/bin/env bash
# docs-sync.sh [TAG]
#
# Generates the parts of docs/ that come from elsewhere in the repository:
# docs/changelog.md from CHANGELOG.md, and docs/_data/release.yml with the
# version the site documents. Both are git-ignored. The release workflow runs
# this before building the site; `make docs` runs it locally.
set -euo pipefail

tag=${1:-$(git describe --tags --abbrev=0 2>/dev/null || echo "unreleased")}
module=$(awk '$1 == "module" { print $2; exit }' go.mod)
repo=${module#github.com/}

{
	printf -- '---\ntitle: Changelog\nnav_order: 8\n---\n\n'
	cat CHANGELOG.md
} >docs/changelog.md

mkdir -p docs/_data
cat >docs/_data/release.yml <<EOF
tag: "$tag"
date: "$(date -u +%Y-%m-%d)"
module: "$module"
pkgdoc: "https://pkg.go.dev/$module@$tag"
source: "https://github.com/$repo/tree/$tag/docs"
EOF

echo "ok: docs/changelog.md and docs/_data/release.yml for $tag"
