#!/usr/bin/env bash
# Resolves the version a release is cutting. The tags this repo carries are the
# record of what has been released, so the version is counted off them and
# nothing in the tree holds it: no commit has to land on master to advance a
# version, and a release cannot disagree with a package.json someone edited.
#
# BUMP is major, minor or patch. VERSION overrides it with a version named
# outright. Writes `version`, `tag` and `released_tag` to $GITHUB_OUTPUT when it
# is set; `released_tag` is the release this one follows, and is empty in a
# repository that has never cut one.
set -euo pipefail

bump="${BUMP:-patch}"
named="${VERSION:-}"

semver='^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+(\.[0-9A-Za-z]+)*)?$'

# Only a stable tag counts as a release. v0.0.1-rc4 is a candidate for 0.0.1, so
# counting a patch off it would skip the very version it was a candidate for.
# `sort -V` puts 0.10.0 above 0.9.0, which a lexical sort does not, and
# `sed -n p` reports no matches as an empty line rather than as the failure
# `grep` would return under pipefail.
released="$(git tag -l 'v*' \
  | sed -n 's/^v\([0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*\)$/\1/p' \
  | sort -V \
  | tail -1)"
released_tag=""
[ -n "$released" ] && released_tag="v$released"

if [ -n "$named" ]; then
  if [[ ! "$named" =~ $semver ]]; then
    echo "next-version: '$named' is not a version this releases" >&2
    echo "next-version: a version is MAJOR.MINOR.PATCH with an optional -prerelease, e.g. 0.1.0 or 1.0.0-rc1" >&2
    exit 1
  fi
  version="$named"
  from="named outright"
else
  base="${released:-0.0.0}"
  from="a $bump off ${base}"
  IFS=. read -r major minor patch <<<"$base"
  case "$bump" in
    major) version="$((major + 1)).0.0" ;;
    minor) version="$major.$((minor + 1)).0" ;;
    patch) version="$major.$minor.$((patch + 1))" ;;
    *)
      echo "next-version: '$bump' is not a bump; use major, minor or patch" >&2
      exit 1
      ;;
  esac
fi

tag="v$version"

# A tag that is already there means this version was already cut. Moving it
# would relabel a release that people have installed.
if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
  echo "next-version: $tag is already tagged, so there is nothing to release at $version" >&2
  exit 1
fi

echo "next-version: releasing $version, $from"
if [ -n "${GITHUB_OUTPUT:-}" ]; then
  {
    echo "version=$version"
    echo "tag=$tag"
    echo "released_tag=$released_tag"
  } >> "$GITHUB_OUTPUT"
fi
