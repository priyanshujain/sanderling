#!/usr/bin/env bash
#
# Builds the embeddable simulator companion tarball.
#
# Copies the runtime files from a local install, preserving the bin/ and
# Frameworks/ layout (the binary resolves frameworks through @rpath, so the
# two directories must stay siblings). Build-time metadata that is never
# needed at runtime is stripped to keep the embedded payload small. The
# stripped layout is re-signed ad-hoc and proven to execute before it is
# packaged.
#
# Output: assets/companion-1.1.8.tar.gz next to this script.

set -euo pipefail

# Keep the archiver from emitting AppleDouble (._name) sidecar entries that
# would otherwise duplicate extended attributes into the payload.
export COPYFILE_DISABLE=1

VERSION="1.1.8"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ASSETS_DIR="${SCRIPT_DIR}/assets"
OUTPUT="${ASSETS_DIR}/companion-${VERSION}.tar.gz"

# Default source is the local formula install. Resolve the symlink so the
# rsync below copies real files rather than a dangling link.
SOURCE="${COMPANION_SOURCE:-/opt/homebrew/opt/idb-companion}"
if [ ! -e "${SOURCE}" ]; then
  echo "source not found: ${SOURCE}" >&2
  echo "set COMPANION_SOURCE to the install prefix" >&2
  exit 1
fi
SOURCE="$(cd "${SOURCE}" && pwd -P)"

if [ ! -x "${SOURCE}/bin/idb_companion" ]; then
  echo "no companion binary at ${SOURCE}/bin/idb_companion" >&2
  exit 1
fi

STAGE="$(mktemp -d)"
PROOF="$(mktemp -d)"
trap 'rm -rf "${STAGE}" "${PROOF}"' EXIT

echo "staging from ${SOURCE}"

# Copy the runtime layout. -a preserves symlinks (the macOS framework
# Versions/Current links must survive intact).
mkdir -p "${STAGE}/bin"
cp -a "${SOURCE}/bin/idb_companion" "${STAGE}/bin/idb_companion"
cp -aR "${SOURCE}/Frameworks" "${STAGE}/Frameworks"

# Strip build-time metadata from every framework. The framework binary, its
# Info.plist, the code signature, and any runtime dylibs are kept; headers and
# the Swift module/source-info artifacts are not loaded at runtime.
for framework in "${STAGE}"/Frameworks/*.framework; do
  version_dir="${framework}/Versions/A"
  [ -d "${version_dir}" ] || continue

  rm -rf "${version_dir}/Headers" "${version_dir}/PrivateHeaders"
  rm -rf "${version_dir}/Modules"
  rm -f "${framework}/Headers" "${framework}/PrivateHeaders" "${framework}/Modules"

  find "${version_dir}" -name '*.swiftmodule' -prune -exec rm -rf {} + 2>/dev/null || true
  find "${version_dir}" \( -name '*.swiftdoc' -o -name '*.swiftsourceinfo' \) -delete 2>/dev/null || true
done

# Re-sign each framework and the main binary ad-hoc. Removing files breaks the
# existing signature seal, which the loader rejects on arm64 macOS.
for framework in "${STAGE}"/Frameworks/*.framework; do
  codesign --force --sign - --timestamp=none "${framework}" >/dev/null 2>&1
done
codesign --force --sign - --timestamp=none "${STAGE}/bin/idb_companion" >/dev/null 2>&1

# Prove the stripped, re-signed layout still loads and runs. tar stores
# symlinks as symlinks so the extracted copy mirrors the embedded payload.
# --version dynamically links every framework and exits zero on success,
# which fails if a strip or re-sign broke a load command or the seal.
STRIPPED_BYTES="$(find "${STAGE}" -type f -exec stat -f%z {} + | awk '{sum += $1} END {print sum}')"

PROOF_TAR="${PROOF}/payload.tar.gz"
tar -czf "${PROOF_TAR}" -C "${STAGE}" .
tar -xzf "${PROOF_TAR}" -C "${PROOF}"

if ! "${PROOF}/bin/idb_companion" --version >/dev/null 2>&1; then
  echo "stripped companion failed to execute" >&2
  exit 1
fi
echo "stripped layout executes"

mkdir -p "${ASSETS_DIR}"
tar -czf "${OUTPUT}" -C "${STAGE}" .

SHA="$(shasum -a 256 "${OUTPUT}" | awk '{print $1}')"

echo "wrote ${OUTPUT}"
echo "stripped uncompressed size: ${STRIPPED_BYTES} bytes"
echo "sha256: ${SHA}"
