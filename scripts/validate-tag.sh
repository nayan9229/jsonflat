#!/usr/bin/env bash
# validate-tag.sh TAG [go.mod]
#
# A release tag must be a plain semantic version, vMAJOR.MINOR.PATCH, and its
# major version must match the module path: no suffix for v0 and v1, /vN for
# anything higher. A tag that gets this wrong is permanent once the Go module
# proxy has seen it, so this runs before anything is created.
set -euo pipefail

tag=${1:?usage: validate-tag.sh TAG [go.mod]}
gomod=${2:-go.mod}

if [[ ! $tag =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
	echo "::error::tag '$tag' is not a plain semantic version of the form vMAJOR.MINOR.PATCH (no pre-release or build suffix, no leading zeros)"
	exit 1
fi
major=${BASH_REMATCH[1]}

module=$(awk '$1 == "module" { print $2; exit }' "$gomod")
if [[ -z $module ]]; then
	echo "::error::no module line in $gomod"
	exit 1
fi

suffix=""
if [[ $module =~ /v([0-9]+)$ ]]; then
	suffix=${BASH_REMATCH[1]}
fi

if [[ -z $suffix && $major -gt 1 ]]; then
	echo "::error::tag $tag has major version $major, but module '$module' has no /v$major suffix"
	exit 1
fi
if [[ -n $suffix && $suffix != "$major" ]]; then
	echo "::error::tag $tag has major version $major, but module '$module' ends in /v$suffix"
	exit 1
fi

echo "ok: $tag is a release tag for module $module"
