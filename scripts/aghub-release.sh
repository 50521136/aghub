#!/bin/sh
#
# AGHub Release Packing Script
#
# Builds and packs the release archives of AGHub.  The archive names must match
# the ones expected by internal/selfupdate, otherwise the online update will not
# find its asset:
#
#	aghub_<version>_<os>_<arch>.tar.gz
#
# where <version> has no leading "v".  Each archive contains the "aghub"
# binary, the systemd unit file and the license text.  A checksums.txt with the
# SHA-256 sums of all archives is written next to them as well.
#
# Usage:
#
#	VERSION=v1.0.0 REPO=owner/aghub sh ./scripts/aghub-release.sh
#
# Environment:
#
#	VERSION    required, the version to build, for example "v1.0.0"
#	REPO       required, the GitHub repository in the "owner/name" format
#	DIST_DIR   optional, the output directory, "dist-aghub" by default
#	PLATFORMS  optional, a space-separated "os/arch" list to build
#	SKIP_JS    optional, set to "1" to reuse the existing frontend build

set -e -f -u

# pipefail is not in POSIX, and dash — the /bin/sh of Debian and Ubuntu — aborts
# the whole script with "Illegal option -o pipefail", which is what broke the
# GitHub runner.  Set it only where it exists.
if (set -o pipefail) 2>/dev/null; then
	# shellcheck disable=SC3040
	set -o pipefail
fi

version="${VERSION:?please set VERSION}"
repo="${REPO:?please set REPO}"
dist="${DIST_DIR:-dist-aghub}"

# The output directory is used as a path relative to the repository root.  An
# absolute DIST_DIR would therefore be read as "./tmp/whatever" and quietly
# create a "tmp" directory inside the repo, so resolve it once, here.
case "$dist" in
/*) dist_dir="$dist" ;;
*) dist_dir="./${dist}" ;;
esac
readonly dist_dir

# The asset name uses the version without the leading "v".
plain_version="${version#v}"

readonly version repo dist plain_version

version_pkg='github.com/AdguardTeam/AdGuardHome/internal/version'
selfupdate_pkg='github.com/AdguardTeam/AdGuardHome/internal/selfupdate'

# The default platform list.  It covers the architectures supported by
# internal/selfupdate's asset naming.
platforms="${PLATFORMS:-linux/amd64 linux/arm64 linux/arm/7 linux/386 darwin/amd64 darwin/arm64 freebsd/amd64 windows/amd64}"
readonly platforms

printf 'building AGHub %s for %s\n' "$version" "$repo" 1>&2

if [ "${SKIP_JS:-0}" != '1' ]; then
	printf 'building the frontend\n' 1>&2
	(cd ./client_v2 && npm ci --no-audit --no-fund --loglevel=error && npm run build-prod)
fi

if [ ! -d './build/static' ]; then
	printf 'error: ./build/static is missing, run the frontend build first\n' 1>&2

	exit 1
fi

committime="$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
readonly committime

rm -rf "$dist_dir"
mkdir -p "$dist_dir"

# pack builds the binary of a single platform and packs it into an archive.
pack() {
	pack_os="$1"
	pack_arch="$2"
	pack_arm="$3"

	pack_name="aghub_${plain_version}_${pack_os}_${pack_arch}"
	pack_dir="$dist_dir/${pack_name}"

	mkdir -p "$pack_dir"

	printf 'building %s\n' "$pack_name" 1>&2

	pack_bin='aghub'
	pack_goarm=''
	pack_gomips=''
	pack_ext=''

	case "$pack_arch" in
	'arm')
		pack_goarm="$pack_arm"
		;;
	'mips'*)
		pack_gomips='softfloat'
		;;
	esac

	case "$pack_os" in
	'windows')
		pack_ext='.exe'
		;;
	esac

	pack_ldflags="-s -w"
	pack_ldflags="${pack_ldflags} -X ${version_pkg}.version=${version}"
	pack_ldflags="${pack_ldflags} -X ${version_pkg}.channel=release"
	pack_ldflags="${pack_ldflags} -X ${version_pkg}.committime=${committime}"
	pack_ldflags="${pack_ldflags} -X ${selfupdate_pkg}.Version=${version}"
	pack_ldflags="${pack_ldflags} -X ${selfupdate_pkg}.Repo=${repo}"

	if [ "$pack_goarm" != '' ]; then
		pack_ldflags="${pack_ldflags} -X ${version_pkg}.goarm=${pack_goarm}"
	fi

	if [ "$pack_gomips" != '' ]; then
		pack_ldflags="${pack_ldflags} -X ${version_pkg}.gomips=${pack_gomips}"
	fi

	env \
		CGO_ENABLED='0' \
		GOOS="$pack_os" \
		GOARCH="$pack_arch" \
		GOARM="$pack_goarm" \
		GOMIPS="$pack_gomips" \
		go build \
		-trimpath \
		-ldflags="$pack_ldflags" \
		-o "${pack_dir}/${pack_bin}${pack_ext}" \
		. \
		;

	cp ./scripts/aghub.service "$pack_dir/"
	cp ./LICENSE.txt "$pack_dir/"

	# Windows prefers ZIP archives; the rest, gzipped tarballs.  The online
	# update only understands tar.gz, so it stays disabled on Windows.
	case "$pack_os" in
	'windows')
		(cd "$pack_dir" && zip -9 -q -r "../../${pack_name}.zip" .)
		;;
	*)
		chmod 0755 "${pack_dir}/${pack_bin}"

		# The binary must come first, so that the fallback lookup of the
		# updater finds it even if the binary has been renamed.
		tar -C "$pack_dir" -c -f - "./${pack_bin}" './aghub.service' './LICENSE.txt' \
			| gzip -9 - >"$dist_dir/${pack_name}.tar.gz"
		;;
	esac

	rm -rf "$pack_dir"
}

for platform in $platforms; do
	os="${platform%%/*}"
	rest="${platform#*/}"
	arch="${rest%%/*}"

	if [ "$arch" != "$rest" ]; then
		arm="${rest#*/}"
	else
		arm=''
	fi

	pack "$os" "$arch" "$arm"
done

printf 'calculating checksums\n' 1>&2

# Note that globbing is disabled by set -f, so the archives are looked up with
# find instead of with a wildcard pattern.
: >"$dist_dir/checksums.txt"

find "$dist_dir" -maxdepth 1 -type f \( -name '*.tar.gz' -o -name '*.zip' \) \
	| sort \
	| while read -r f; do
		sum="$(sha256sum "$f" | cut -d' ' -f1)"
		printf '%s  %s\n' "$sum" "$(basename "$f")" >>"$dist_dir/checksums.txt"
	done

if [ ! -s "$dist_dir/checksums.txt" ]; then
	printf 'error: no archives were produced\n' 1>&2

	exit 1
fi

printf 'done:\n' 1>&2
ls -1 "$dist_dir" 1>&2
