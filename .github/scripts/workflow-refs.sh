#!/usr/bin/env bash
# Checks that everything the workflow names actually exists: composite actions,
# make targets, and the scripts a run: block invokes. Then checks that no run:
# block interpolates a `${{ }}`.
#
# This is the class actionlint does not cover. `uses: ./.github/actions/typo`
# lints clean and fails only when the job runs, and the folio jobs and the
# release job never run on a pull request, so that first run is after merge.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

ROOT="$root" python3 - <<'PY'
import glob
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

# --- expressions in a run: block ---------------------------------------------
# A `${{ }}` is substituted into the script text before bash reads the line, so
# an expression carrying text someone else wrote runs as a command. Values reach
# a run: block through env instead. actionlint flags only the contexts it knows
# are attacker-controlled, and a matrix value or a dispatch input is not on that
# list.
print("\nrun: blocks free of ${{ }}:")


def run_blocks(text):
    lines = text.split("\n")
    i = 0
    while i < len(lines):
        head = re.match(r"^(\s*(?:-\s+)?)run:(.*)$", lines[i])
        if head is None:
            i += 1
            continue
        column, rest = len(head.group(1)), head.group(2).strip()
        start, body = i + 1, []
        if rest in ("|", "|-", "|+", ">", ">-", ">+", ""):
            i += 1
            while i < len(lines) and (not lines[i].strip()
                                      or len(lines[i]) - len(lines[i].lstrip()) > column):
                body.append(lines[i])
                i += 1
        else:
            body.append(rest)
            i += 1
        yield start, "\n".join(body)


blocks = 0
for path in workflow_files():
    hits = []
    for line, body in run_blocks(open(path).read()):
        blocks += 1
        hits += ["line %d: %s" % (line, hit) for hit in re.findall(r"\$\{\{.*?\}\}", body, re.S)]
    report(not hits, rel(path), "  (%s)" % ("; ".join(hits) if hits else "clean"))

# Same reason as the local action count above: a scanner that reads no run:
# block at all would pass every file it never looked at.
if blocks == 0:
    sys.exit("workflow-refs: found no run: block at all, so this check is not "
             "reading the workflows it claims to read")

print("\n%d references checked" % checked)
if problems:
    sys.exit("workflow-refs: unresolved: %s" % ", ".join(problems))
print("workflow-refs: ok")
PY
