#!/usr/bin/env bash
# Build the uatu docs site using pandoc.
# Outputs static HTML to build/site/ ready for GitHub Pages.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="$REPO_ROOT/docs"
OUT="$REPO_ROOT/build/site"
TEMPLATE="$SRC/_template/page.html"
ASSETS="$SRC/_assets"

if ! command -v pandoc >/dev/null 2>&1; then
  echo "pandoc not found on PATH. Install from https://pandoc.org/ or 'brew install pandoc'." >&2
  exit 1
fi

rm -rf "$OUT"
mkdir -p "$OUT/_assets"
cp -R "$ASSETS"/. "$OUT/_assets/"

build_one() {
  local src="$1"
  local rel="${src#$SRC/}"
  local out="$OUT/${rel%.md}.html"
  local dir; dir=$(dirname "$rel")

  local root=""
  if [ "$dir" != "." ]; then
    local depth; depth=$(awk -F/ '{print NF}' <<< "$dir")
    for ((i = 0; i < depth; i++)); do root="../$root"; done
  fi

  mkdir -p "$(dirname "$out")"
  pandoc "$src" \
    --from=gfm \
    --to=html5 \
    --standalone \
    --highlight-style=tango \
    --template="$TEMPLATE" \
    -o "$out"

  # macOS sed and GNU sed both accept `-i.bak + rm`; avoids `-i ''` portability issues.
  sed -i.bak "s|__ROOT__|$root|g" "$out" && rm "$out.bak"
}

count=0
while IFS= read -r -d '' f; do
  build_one "$f"
  count=$((count + 1))
done < <(find "$SRC" -type f -name '*.md' -not -path "$SRC/_*" -print0)

echo "built $count pages to $OUT"
