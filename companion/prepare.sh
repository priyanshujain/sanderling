#!/usr/bin/env bash
#
# Builds the embeddable simulator runner tarball.
#
# Regenerates the Xcode project, builds the UI test bundle for a generic
# simulator destination (so no booted simulator is needed), then stages a
# relocatable test root: the runner app alongside its xctestrun. A port
# placeholder is injected into the xctestrun so the driver can substitute the
# real port before launching. The staged tree is packaged as a tarball.
#
# Output: ../internal/driver/ioscompanion/runnerassets/assets/runner-1.0.0.tar.gz

set -euo pipefail

# Keep the archiver from emitting AppleDouble (._name) sidecar entries that
# would otherwise duplicate extended attributes into the payload.
export COPYFILE_DISABLE=1

VERSION="1.0.0"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ASSETS_DIR="${SCRIPT_DIR}/../internal/driver/ioscompanion/runnerassets/assets"
OUTPUT="${ASSETS_DIR}/runner-${VERSION}.tar.gz"

cd "${SCRIPT_DIR}"

# Regenerate the project from its spec. The .xcodeproj is gitignored, so a
# fresh checkout must produce it before building.
/opt/homebrew/bin/xcodegen --spec project.yml

# Build the UI test bundle for a generic simulator destination so no booted
# simulator is required; products land under build/Build/Products/.
xcodebuild build-for-testing \
  -project CompanionRunner.xcodeproj \
  -scheme CompanionRunner \
  -destination 'generic/platform=iOS Simulator' \
  -derivedDataPath build \
  CODE_SIGNING_ALLOWED=NO

PRODUCTS="${SCRIPT_DIR}/build/Build/Products"

STAGE="$(mktemp -d)"
trap 'rm -rf "${STAGE}"' EXIT

# The xctestrun name embeds the simulator SDK version, so resolve it rather
# than hardcoding it.
XCTESTRUN="$(find "${PRODUCTS}" -maxdepth 1 -name '*.xctestrun' -print -quit)"
if [ -z "${XCTESTRUN}" ]; then
  echo "no xctestrun under ${PRODUCTS}" >&2
  exit 1
fi
cp "${XCTESTRUN}" "${STAGE}/runner.xctestrun"

# Copy the runner app into the staged products directory. The .swiftmodule
# sibling is build-time only and is not copied.
mkdir -p "${STAGE}/Debug-iphonesimulator"
cp -R \
  "${PRODUCTS}/Debug-iphonesimulator/CompanionRunnerUITests-Runner.app" \
  "${STAGE}/Debug-iphonesimulator/CompanionRunnerUITests-Runner.app"

# Inject the port placeholder the driver substitutes at launch time.
/usr/libexec/PlistBuddy \
  -c "Add :CompanionRunnerUITests:EnvironmentVariables:COMPANION_PORT string __COMPANION_PORT__" \
  "${STAGE}/runner.xctestrun"

STAGED_BYTES="$(find "${STAGE}" -type f -exec stat -f%z {} + | awk '{sum += $1} END {print sum}')"

mkdir -p "${ASSETS_DIR}"
tar -czf "${OUTPUT}" -C "${STAGE}" .

SHA="$(shasum -a 256 "${OUTPUT}" | awk '{print $1}')"

echo "wrote ${OUTPUT}"
echo "staged uncompressed size: ${STAGED_BYTES} bytes"
echo "sha256: ${SHA}"
