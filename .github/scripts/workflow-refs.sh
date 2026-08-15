#!/usr/bin/env bash
# Checks that everything the workflows name actually exists: composite actions,
# make targets, and the scripts a run: block invokes.
#
# This is the class actionlint does not cover. `uses: ./.github/actions/typo`
# lints clean and fails only when the job runs, and these workflows are
# dispatch-only or push-triggered, so that first run is after merge.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

ROOT="$root" python3 - <<'PY'
import glob
import json
import os
import re
import sys

root = os.environ["ROOT"]
problems = []
checked = 0


def report(ok, label, detail=""):
    global checked
    checked += 1
    if not ok:
        problems.append("%s%s" % (label, detail))
    print("  %-4s %s%s" % ("ok" if ok else "MISS", label, detail))


def workflow_files():
    return (sorted(glob.glob(os.path.join(root, ".github/workflows/*.yml")))
            + sorted(glob.glob(os.path.join(root, ".github/actions/*/action.yml"))))


def rel(path):
    return os.path.relpath(path, root)


# --- composite actions -------------------------------------------------------
print("local action references:")
local_refs = 0
for path in workflow_files():
    # Comments are not references. They mention paths as examples, and a version
    # comment trails the `uses:` line of every pinned action.
    body = re.sub(r"#[^\n]*", "", open(path).read())
    found = re.findall(r"^\s*-?\s*uses:\s*(\./\S+)\s*$", body, re.M)
    local_refs += len(found)
    for ref in found:
        target = os.path.join(root, ref[2:], "action.yml")
        report(os.path.isfile(target), ref, "  (from %s)" % rel(path))
    # A checker that silently matches nothing reports a safety it never looked
    # for. If the file names a local action in a form the pattern above does not
    # read, that is a broken checker, not a clean file.
    mentions = len(re.findall(r"\./\.github/actions/", body))
    if mentions > len(found):
        sys.exit("workflow-refs: %s mentions ./.github/actions/ %d time(s) but this "
                 "check only parsed %d `uses:` reference(s) out of it, so it is not "
                 "reading the file it claims to read" % (rel(path), mentions, len(found)))

if local_refs == 0:
    sys.exit("workflow-refs: found no `uses: ./...` at all, so this check is not "
             "reading the workflows it claims to read")

# --- make targets ------------------------------------------------------------
print("\nmake targets named by a run: step:")
makefile = open(os.path.join(root, "Makefile")).read()
targets = set(re.findall(r"^([A-Za-z0-9_.-]+):", makefile, re.M))
wanted = set()
for path in sorted(glob.glob(os.path.join(root, ".github/workflows/*.yml"))):
    body = open(path).read()
    # Comments are not run, and prose in them says things like "make the run
    # always open the same way", which is not a target.
    commands = re.sub(r"#[^\n]*", "", body)
    for name in re.findall(r"\bmake\s+([a-z][a-z0-9-]*)\b", commands):
        wanted.add(name)
    # `make "sanderling-$SANDERLING"` is resolved from the matrix that feeds it
    if "sanderling-$SANDERLING" in body:
        table = re.search(r"examples='(\[.*?\])'", body, re.S)
        if table is None:
            sys.exit("workflow-refs: %s builds a make target from $SANDERLING but its "
                     "examples table could not be read" % rel(path))
        for entry in json.loads(table.group(1)):
            wanted.add("sanderling-%s" % entry["sanderling"])
for name in sorted(wanted):
    report(name in targets, "make %s" % name)

# --- scripts a run: step invokes ---------------------------------------------
print("\nscripts a run: step invokes:")
scripts = set()
for path in workflow_files():
    scripts |= set(re.findall(r"\.github/scripts/[A-Za-z0-9_.-]+\.sh", open(path).read()))
for name in sorted(scripts):
    full = os.path.join(root, name)
    report(os.path.isfile(full), name)
    if os.path.isfile(full):
        report(os.access(full, os.X_OK), "%s is executable" % name)

print("\n%d references checked" % checked)
if problems:
    sys.exit("workflow-refs: unresolved: %s" % ", ".join(problems))
print("workflow-refs: ok")
PY
