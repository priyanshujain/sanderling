#!/usr/bin/env python3
# Regenerates the canned gate-analyzer fixtures. Each fixture is a minimal but
# format-accurate run directory (trace.jsonl + output.log + exit_status) that
# isolates one gate's pass or fail condition. Run from this directory:
#   python3 generate.py
import json
import os
from datetime import datetime, timedelta, timezone

HERE = os.path.dirname(os.path.abspath(__file__))
BASE = datetime(2026, 6, 6, 12, 0, 0, tzinfo=timezone(timedelta(hours=5, minutes=30)))


# What a typed value is written to the trace as when the target reports no
# secure fact for the field. Android reports none for any field, so every
# InputText it records reads this (internal/verifier/redaction.go).
REDACTED = "[redacted]"


def element(resource_id, text="", element_class="android.widget.EditText", secure=None):
    attrs = {"resource-id": resource_id, "class": element_class, "text": text}
    if secure is not None:
        attrs["secure"] = "true" if secure else "false"
    return {
        "resourceId": resource_id,
        "class": element_class,
        "editable": True,
        "bounds": {"left": 34, "top": 87, "right": 286, "bottom": 135},
        "attrs": attrs,
    }


def login_screen(email_text="", password_text=""):
    return {
        "elements": [
            element("LoginScreen", element_class="android.view.View"),
            element("LoginEmail", email_text),
            element("LoginPassword", password_text),
            element("LoginSubmit", element_class="android.view.View"),
        ]
    }


# iOS states the secure fact on every editable field, so the typed value reaches
# the trace for any field the platform reports as not a secure entry.
def ios_login_screen(email_text=""):
    return {
        "elements": [
            element("LoginScreen", element_class="XCUIElementTypeOther"),
            element("LoginEmail", email_text,
                    element_class="XCUIElementTypeTextField", secure=False),
            element("LoginPassword", element_class="XCUIElementTypeSecureTextField",
                    secure=True),
        ]
    }


def input_action(field, text=REDACTED):
    return {
        "kind": "InputText",
        "text": text,
        "selector": f"testTag:LoginScreen > testTag:{field}",
    }


def step(index, gap_ms, hierarchy, next_action=None):
    timestamp = (BASE + timedelta(milliseconds=index * gap_ms)).isoformat()
    record = {"step": index, "timestamp": timestamp, "hierarchy": hierarchy}
    if next_action is not None:
        record["next_action"] = next_action
    return record


def write_run(name, steps, exit_status="0", output_lines=None):
    directory = os.path.join(HERE, name)
    os.makedirs(directory, exist_ok=True)
    with open(os.path.join(directory, "trace.jsonl"), "w") as handle:
        for record in steps:
            handle.write(json.dumps(record) + "\n")
    with open(os.path.join(directory, "exit_status"), "w") as handle:
        handle.write(exit_status)
    lines = output_lines if output_lines is not None else ["run complete\n"]
    with open(os.path.join(directory, "output.log"), "w") as handle:
        handle.writelines(lines)


# A clean run on the android backend: empty login fields first, single (not
# doubled) values observed in the following snapshots, sub-second step gaps.
def healthy_steps(gap_ms=600):
    return [
        step(1, gap_ms, login_screen("", ""), input_action("LoginEmail")),
        step(2, gap_ms, login_screen("demo@folio.app", ""), input_action("LoginPassword")),
        step(3, gap_ms, login_screen("demo@folio.app", "ledger123")),
        step(4, gap_ms, login_screen("demo@folio.app", "ledger123")),
    ]


BENIGN_NOISE = [
    "companion pid=4242 listening on 127.0.0.1:51000\n",
    "objc[4242]: Class FBSDKError is implemented in both /a (0x1) and /b (0x2). "
    "One of the two will be used. Which one is undefined.\n",
    "WARNING: All log messages before absl::InitializeLog() is called are written to STDERR\n",
    # An incidental ERROR substring inside a longer word: the G2 scan is
    # word-bounded, so this must not trip the gate.
    "archived previous runs under runs/ERRORS_archive\n",
]

write_run("pass", healthy_steps())

write_run("g1-nonzero-exit", healthy_steps(), exit_status="1")

write_run("g2-benign-only", healthy_steps(), output_lines=BENIGN_NOISE + ["run complete\n"])

write_run(
    "g2-real-error",
    healthy_steps(),
    output_lines=BENIGN_NOISE
    + ["error: hierarchy fetch failed: companion connection lost\n"],
)

# G3: first snapshot already carries text in the login fields (clear-state
# failed to wipe the previous session).
write_run(
    "g3-dirty-field",
    [
        step(1, 600, login_screen("stale@folio.app", "leftover"), input_action("LoginEmail")),
        step(2, 600, login_screen("demo@folio.app", "leftover")),
    ],
)

# G4: after typing into LoginEmail the next snapshot shows the value appended to
# itself (append-vs-replace / double-paste regression). The typed value is the
# redaction placeholder, as it is on every android InputText, so the doubling is
# only visible in the observed field value.
write_run(
    "g4-doubled-text",
    [
        step(1, 600, login_screen("", ""), input_action("LoginEmail")),
        step(2, 600, login_screen("demo@folio.appdemo@folio.app", "")),
    ],
)

# G4: two corpus values that read as their own doubling. "a" repeated and a pair
# of spaces are each one character over and over, so neither says anything about
# how many times the driver typed it, and neither may fail the gate.
write_run(
    "g4-repeated-character",
    [
        step(1, 600, login_screen("", ""), input_action("LoginEmail")),
        step(2, 600, login_screen("a" * 4096, ""), input_action("LoginEmail")),
        step(3, 600, login_screen("  ", "")),
    ],
)

# G4: an action typed at coordinates names no field. The step carries nothing
# the gate can check, and it may not stop the gate checking the rest.
write_run(
    "g4-selectorless-input",
    [
        step(1, 600, login_screen("", ""), {"kind": "InputText", "text": REDACTED}),
        step(2, 600, login_screen("demo@folio.app", ""), input_action("LoginPassword")),
        step(3, 600, login_screen("demo@folio.app", "ledger123")),
    ],
)

# G4: the same regression on a backend that records the typed value. The driver
# appended the value twice to what the field already held, so the observed value
# is not its own doubling and only the recorded text reveals it.
write_run(
    "g4-recorded-text",
    [
        step(1, 600, ios_login_screen("demo@folio.app"),
             input_action("LoginEmail", "ledger123")),
        step(2, 600, ios_login_screen("demo@folio.appledger123ledger123")),
    ],
)

# G5: step gaps far exceed the 2.5s ceiling, driving p95 over the limit.
write_run("g5-slow-p95", healthy_steps(gap_ms=4000))

print("fixtures written")
