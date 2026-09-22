#!/usr/bin/env bash
# changelog-section.sh VERSION [CHANGELOG.md]
#
# Prints the body of the "## [VERSION] - YYYY-MM-DD" section of the changelog.
# Fails unless exactly one such heading exists and the section has content.
# The release workflow uses the output as the release notes.
set -euo pipefail

version=${1:?usage: changelog-section.sh VERSION [CHANGELOG.md]}
file=${2:-CHANGELOG.md}

escaped=${version//./\\.}
heading="^## \\[$escaped\\] - [0-9]{4}-[0-9]{2}-[0-9]{2}\$"
count=$(grep -cE "$heading" "$file" || true)
if [[ $count -ne 1 ]]; then
	echo "::error::$file must have exactly one heading '## [$version] - YYYY-MM-DD'; found $count" >&2
	exit 1
fi

# From the heading to the next "## " heading or the link references at the
# bottom, without the leading and trailing blank lines.
body=$(awk -v prefix="## [$version] - " '
	on && (/^## / || /^\[[^]]+\]: /) { exit }
	on { print }
	index($0, prefix) == 1 { on = 1 }
' "$file" | sed -e '/./,$!d' | sed -e :a -e '/^\n*$/{$d;N;ba' -e '}')

if [[ -z ${body//[[:space:]]/} ]]; then
	echo "::error::the changelog section for $version is empty" >&2
	exit 1
fi
printf '%s\n' "$body"
