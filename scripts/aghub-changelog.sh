#!/usr/bin/env bash
#
# aghub-changelog.sh generates the release notes of a version from the git
# history, grouped by the type of the conventional commit.
#
# Usage:
#
#	bash ./scripts/aghub-changelog.sh <version> [repository]
#
# The notes go to the standard output.  The repository is used for the compare
# link and defaults to $GITHUB_REPOSITORY.
#
# The section headers are in Chinese because the notes are shown in the AGHub
# update page, and the commit subjects are kept as they are written.

set -e -f -u

if [ "${#}" -lt 1 ]; then
	echo 'usage: aghub-changelog.sh <version> [repository]' >&2

	exit 1
fi

version="${1}"
repo="${2:-${GITHUB_REPOSITORY:-}}"

# The tag of the previous release.  The tag of this version may not exist yet
# when the workflow is run by hand, so the parent commit is used as the anchor.
prev=''

if git rev-parse --verify --quiet "${version}^{commit}" > /dev/null; then
	anchor="${version}^"
else
	anchor='HEAD'
fi

if ! prev="$(git describe --tags --abbrev=0 --match 'v*' "${anchor}" 2> /dev/null)"; then
	prev=''
fi

if [ -n "${prev}" ] && [ "${prev}" != "${version}" ]; then
	range="${prev}..${version}"
else
	range="${version}"
	prev=''
fi

# The subjects of the commits, one per line.  A failure here is not fatal: an
# empty history still produces a usable body.
subjects="$(git log --no-merges --pretty=format:'%s' "${range}" 2> /dev/null || true)"

if [ -z "${prev}" ]; then
	# Without a previous release the range would be the whole history, which
	# is not a changelog and is far too large for a release body.
	printf '首个发布版本。\n'

	exit 0
fi

added=''
fixed=''
improved=''
other=''

while IFS= read -r subject; do
	if [ -z "${subject}" ]; then
		continue
	fi

	# The type is the part before the colon of a conventional commit, with
	# the optional scope removed.  Anything else goes to "other".
	type="${subject%%:*}"
	type="${type%%(*}"

	# The body is the part after the colon, or the whole subject when it is
	# not a conventional commit.
	body="${subject}"
	case "${subject}" in
		*': '*)
			body="${subject#*: }"
			;;
	esac

	line="- ${body}"

	case "${type}" in
		feat | feature)
			added="${added}${line}
"
			;;
		fix | bugfix)
			fixed="${fixed}${line}
"
			;;
		perf | refactor | style | build | ci | docs | test | chore)
			improved="${improved}${line}
"
			;;
		*)
			other="${other}${line}
"
			;;
	esac
done <<< "${subjects}"

section() {
	if [ -n "${2}" ]; then
		printf '### %s\n\n%s\n' "${1}" "${2}"
	fi
}

if [ -z "${added}${fixed}${improved}${other}" ]; then
	echo '本次发布没有代码变更。'
else
	section '新增' "${added}"
	section '修复' "${fixed}"
	section '其他改进' "${improved}"
	section '其他' "${other}"
fi

if [ -n "${repo}" ] && [ -n "${prev}" ]; then
	printf '**完整变更**：https://github.com/%s/compare/%s...%s\n' \
		"${repo}" "${prev}" "${version}"
fi
