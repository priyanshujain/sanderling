#!/usr/bin/env bash
#
# Installs the sanderling CLI on macOS or Linux.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/priyanshujain/sanderling/master/install.sh | bash
#
# Environment:
#   SANDERLING_VERSION  Tag to install (default: latest release)
#   SANDERLING_INSTALL  Install prefix (default: $HOME/.sanderling); binary lands in $prefix/bin

set -euo pipefail

REPO="priyanshujain/sanderling"
PREFIX="${SANDERLING_INSTALL:-$HOME/.sanderling}"
BIN_DIR="$PREFIX/bin"

os="$(uname -s)"
arch="$(uname -m)"
case "$os" in
  Darwin) os=darwin ;;
  Linux)  os=linux ;;
  *) echo "sanderling: unsupported OS '$os' (need Darwin or Linux)" >&2; exit 1 ;;
esac
case "$arch" in
  x86_64|amd64)  arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "sanderling: unsupported arch '$arch' (need amd64 or arm64)" >&2; exit 1 ;;
esac

version="${SANDERLING_VERSION:-}"
if [ -z "$version" ]; then
  # /releases/latest skips pre-releases; fall back to /releases for the
  # newest tag of any kind so alphas remain installable.
  version="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null \
    | awk -F'"' '/"tag_name":/ {print $4; exit}')"
fi
if [ -z "$version" ]; then
  version="$(curl -fsSL "https://api.github.com/repos/$REPO/releases?per_page=1" \
    | awk -F'"' '/"tag_name":/ {print $4; exit}')"
fi
if [ -z "$version" ]; then
  echo "sanderling: could not resolve a release tag" >&2; exit 1
fi

stripped="${version#v}"
tarball="sanderling_${stripped}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/${version}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "sanderling: downloading $tarball ($version)"
curl -fsSL -o "$tmp/$tarball"      "$base/$tarball"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"

if command -v sha256sum >/dev/null 2>&1; then
  ( cd "$tmp" && grep "  $tarball\$" checksums.txt | sha256sum -c - >/dev/null )
elif command -v shasum >/dev/null 2>&1; then
  ( cd "$tmp" && grep "  $tarball\$" checksums.txt | shasum -a 256 -c - >/dev/null )
else
  echo "sanderling: no sha256 tool available, skipping checksum verification" >&2
fi

tar -xzf "$tmp/$tarball" -C "$tmp"
mkdir -p "$BIN_DIR"
mv "$tmp/sanderling" "$BIN_DIR/sanderling"
chmod +x "$BIN_DIR/sanderling"

echo "sanderling: installed $version to $BIN_DIR/sanderling"

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) echo "sanderling: add $BIN_DIR to PATH, e.g. 'export PATH=\"$BIN_DIR:\$PATH\"'" ;;
esac
