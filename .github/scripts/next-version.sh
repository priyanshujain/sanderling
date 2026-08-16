#!/usr/bin/env bash
# Resolves the version a release is cutting. The tags this repo carries are the
# record of what has been released, so the version is counted off them and
# nothing in the tree holds it: no commit has to land on master to advance a
# version, and a release cannot disagree with a package.json someone edited.
#
# BUMP is major, minor or patch. Writes `version`, `tag`, `released_tag` and
# `previous_tag` to $GITHUB_OUTPUT when it is set. `released_tag` is the release
# this one follows, and is the commit a promotion re-cuts. `previous_tag` is how
# far back the release notes should reach.
set -euo pipefail

bump="${BUMP:-patch}"

# Only a stable tag counts as a release. v0.0.1-rc4 is a candidate for 0.0.1, so
# counting a patch off it would skip the very version it was a candidate for.
# `sort -V` puts 0.10.0 above 0.9.0, which a lexical sort does not, and
# `sed -n p` reports no matches as an empty line rather than as the failure
# `grep` would return under pipefail.
releases() { # <sed script selecting the tags to consider>
  git tag -l 'v*' | sed -n "$1" | sort -V
}

stable='s/^v\([0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*\)$/\1/p'
released="$(releases "$stable" | tail -1)"
released_tag=""
if [ -n "$released" ]; then released_tag="v$released"; fi

base="${released:-0.0.0}"
IFS=. read -r major minor patch <<<"$base"

case "$bump" in
  major)
    version="$((major + 1)).0.0"
    level='s/^v\([0-9][0-9]*\.0\.0\)$/\1/p'
    ;;
  minor)
    version="$major.$((minor + 1)).0"
    level='s/^v\([0-9][0-9]*\.[0-9][0-9]*\.0\)$/\1/p'
    ;;
  patch)
    version="$major.$minor.$((patch + 1))"
    level=""
    ;;
  *)
    echo "next-version: '$bump' is not a bump; use major, minor or patch" >&2
    exit 1
    ;;
esac

# A patch follows the release before it, which is what GoReleaser assumes on its
# own, so it is left alone to assume it. A milestone consolidates every patch
# since the last release at its own level, and its notes have to reach back that
# far or they describe the one merge that happened to be last. With nothing at
# that level yet, they reach back to the first release there has ever been.
previous_tag=""
if [ -n "$level" ]; then
  previous="$(releases "$level" | tail -1)"
  previous="${previous:-$(releases "$stable" | head -1)}"
  if [ -n "$previous" ]; then previous_tag="v$previous"; fi
fi

tag="v$version"

echo "next-version: releasing $version, a $bump off $base"
if [ -n "$previous_tag" ]; then
  echo "next-version: the notes reach back to $previous_tag"
fi

if [ -n "${GITHUB_OUTPUT:-}" ]; then
  {
    echo "version=$version"
    echo "tag=$tag"
    echo "released_tag=$released_tag"
    echo "previous_tag=$previous_tag"
  } >> "$GITHUB_OUTPUT"
fi
