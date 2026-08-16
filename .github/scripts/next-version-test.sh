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

resolve() { # <case> <bump> <tag>...
  local name="$1" bump="$2"
  shift 2
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
  (cd "$repo" && BUMP="$bump" GITHUB_OUTPUT="$outputs" \
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


# How far back GoReleaser reaches for the notes. Empty leaves it on its own
# default, which is the release immediately before this one.
expect_previous_tag() { # <want, empty for none> <case>
  grep -qxF -- "previous_tag=$1" "$outputs" \
    || fail "$2: $(grep '^previous_tag=' "$outputs" || echo 'no previous_tag'), want previous_tag=$1"
}

expect_refused() { # <case> <message fragment>
  [ "$status" != 0 ] || fail "$1: exit 0, want a refusal"
  grep -q -- "$2" "$stderr" || fail "$1: refused with '$(cat "$stderr")', want it to mention '$2'"
  [ ! -s "$outputs" ] || fail "$1: refused but still wrote an output"
}

# A repository with nothing released yet starts the line at 0.0.1 rather than
# reissuing 0.0.0, and has no earlier release to write notes against.
resolve first patch
expect_version 0.0.1 first
expect_previous_tag "" first

# The rc tags this repository carries are candidates for 0.0.1, so the first
# stable release is 0.0.1 and not 0.0.2.
resolve rcs patch v0.0.1-rc1 v0.0.1-rc4
expect_version 0.0.1 rcs

resolve patch patch v1.2.3
expect_version 1.2.4 patch
# A patch already follows the release before it, so GoReleaser is left alone.
expect_previous_tag "" patch

resolve minor minor v1.2.3
expect_version 1.3.0 minor

resolve major major v1.2.3
expect_version 2.0.0 major

# Lexically 0.9.0 sorts above 0.10.0, so a version-blind sort would count the
# next patch off the wrong release and hand back 0.9.1.
resolve ordering patch v0.9.0 v0.10.0
expect_version 0.10.1 ordering

# A tag that is not a release is not a base to count from.
resolve noise patch v1.2.3 nightly v2.0.0-rc1 vfoo
expect_version 1.2.4 noise

# A bump counts off the highest release, so releasing twice in a row advances
# twice rather than landing on the tag the first one just cut.
resolve consecutive patch v1.2.3 v1.2.4
expect_version 1.2.5 consecutive

resolve bad-bump sideways v1.2.3
expect_refused bad-bump "is not a bump"

# --- how far back a milestone's notes reach ----------------------------------
# The whole point of consolidating: 0.2.0's notes have to cover every patch
# since 0.1.0, not just the merge that happened to be last before it.
resolve minor-notes minor v0.1.0 v0.1.1 v0.1.2
expect_version 0.2.0 minor-notes
expect_previous_tag v0.1.0 minor-notes

# The last release at this level, not the first one ever seen at it.
resolve minor-notes-latest minor v0.1.0 v0.2.0 v0.2.1
expect_version 0.3.0 minor-notes-latest
expect_previous_tag v0.2.0 minor-notes-latest

# A major counts as a milestone for a minor's notes: 1.0.0 is where the patches
# being consolidated started.
resolve minor-notes-major minor v0.9.0 v1.0.0 v1.0.1
expect_version 1.1.0 minor-notes-major
expect_previous_tag v1.0.0 minor-notes-major

# The same version-aware ordering the base needs.
resolve minor-notes-ordering minor v0.9.0 v0.10.0 v0.10.1
expect_version 0.11.0 minor-notes-ordering
expect_previous_tag v0.10.0 minor-notes-ordering

# A major reaches back to the last major, not to the last minor.
resolve major-notes major v1.0.0 v1.1.0 v1.1.3
expect_version 2.0.0 major-notes
expect_previous_tag v1.0.0 major-notes

# The first milestone of its kind has nothing at its own level to reach back to,
# so it reaches back to the first release there has ever been.
resolve minor-notes-firstever minor v0.0.1 v0.0.2 v0.0.3
expect_version 0.1.0 minor-notes-firstever
expect_previous_tag v0.0.1 minor-notes-firstever

resolve major-notes-firstever major v0.1.0 v0.2.0 v0.2.1
expect_version 1.0.0 major-notes-firstever
expect_previous_tag v0.1.0 major-notes-firstever

if [ "$failed" = 0 ]; then
  echo "next-version-test: ok"
else
  echo "next-version-test: failures above" >&2
  exit 1
fi
