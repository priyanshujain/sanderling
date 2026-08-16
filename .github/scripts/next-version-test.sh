#!/usr/bin/env bash
# Drives next-version.sh against repositories whose tags are planted by hand.
# Run under the flags GitHub Actions uses for a `run:` block, because that is
# where a swallowed failure hides.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/next-version.sh"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

failed=0
outputs=""
status=0
stderr=""

resolve() { # <case> <bump> <version> <tag>...
  local name="$1" bump="$2" version="$3"
  shift 3
  local repo="$work/$name"
  rm -rf "$repo"
  mkdir -p "$repo"
  git -C "$repo" init -q
  git -C "$repo" -c user.email=t@t -c user.name=t commit -q --allow-empty -m base
  local tag
  for tag in "$@"; do git -C "$repo" tag "$tag"; done
  outputs="$work/$name.out"
  stderr="$work/$name.err"
  : > "$outputs"
  status=0
  (cd "$repo" && BUMP="$bump" VERSION="$version" GITHUB_OUTPUT="$outputs" \
    bash -eo pipefail "$script") >/dev/null 2>"$stderr" || status=$?
}

fail() {
  echo "FAIL: $*" >&2
  failed=1
}

expect_version() { # <want> <case>
  local want="version=$1"
  grep -qxF -- "$want" "$outputs" || fail "$2: resolved $(tr '\n' ' ' <"$outputs"), want $want"
  grep -qxF -- "tag=v$1" "$outputs" || fail "$2: tag does not match the version it resolved"
  [ "$status" = 0 ] || fail "$2: exit $status, want 0"
}

expect_refused() { # <case> <message fragment>
  [ "$status" != 0 ] || fail "$1: exit 0, want a refusal"
  grep -q -- "$2" "$stderr" || fail "$1: refused with '$(cat "$stderr")', want it to mention '$2'"
  [ ! -s "$outputs" ] || fail "$1: refused but still wrote an output"
}

# A repository with nothing released yet starts the line at 0.0.1 rather than
# reissuing 0.0.0.
resolve first patch "" 
expect_version 0.0.1 first

# The rc tags this repository carries are candidates for 0.0.1, so the first
# stable release is 0.0.1 and not 0.0.2.
resolve rcs patch "" v0.0.1-rc1 v0.0.1-rc4
expect_version 0.0.1 rcs

resolve patch patch "" v1.2.3
expect_version 1.2.4 patch

resolve minor minor "" v1.2.3
expect_version 1.3.0 minor

resolve major major "" v1.2.3
expect_version 2.0.0 major

# Lexically 0.9.0 sorts above 0.10.0, so a version-blind sort would count the
# next patch off the wrong release and hand back 0.9.1.
resolve ordering patch "" v0.9.0 v0.10.0
expect_version 0.10.1 ordering

# A tag that is not a release is not a base to count from.
resolve noise patch "" v1.2.3 nightly v2.0.0-rc1 vfoo
expect_version 1.2.4 noise

resolve named "" 2.5.0 v1.2.3
expect_version 2.5.0 named

# A named version wins over the bump rather than being combined with it.
resolve named-over-bump major 0.4.0 v1.2.3
expect_version 0.4.0 named-over-bump

resolve named-prerelease "" 1.0.0-rc1 v0.9.0
expect_version 1.0.0-rc1 named-prerelease

resolve named-junk "" "1.0" v1.2.3
expect_refused named-junk "is not a version this releases"

# The refusal has to survive text that would otherwise reach a shell or forge a
# second $GITHUB_OUTPUT key.
resolve named-injection "" '1.0.0; touch /tmp/pwned' v1.2.3
expect_refused named-injection "is not a version this releases"

resolve named-newline "" '1.0.0
version=9.9.9' v1.2.3
expect_refused named-newline "is not a version this releases"

resolve bad-bump sideways "" v1.2.3
expect_refused bad-bump "is not a bump"

# A bump counts off the highest release, so releasing twice in a row advances
# twice rather than landing on the tag the first one just cut.
resolve consecutive patch "" v1.2.3 v1.2.4
expect_version 1.2.5 consecutive

# Naming a version that is already tagged would relabel a release people have
# already installed.
resolve named-already "" 1.2.3 v1.2.3
expect_refused named-already "is already tagged"

if [ "$failed" = 0 ]; then
  echo "next-version-test: ok"
else
  echo "next-version-test: failures above" >&2
  exit 1
fi
