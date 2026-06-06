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


def element(resource_id, text="", element_class="android.widget.EditText"):
    return {
        "resourceId": resource_id,
        "class": element_class,
        "editable": True,
        "bounds": {"left": 34, "top": 87, "right": 286, "bottom": 135},
        "attrs": {"resource-id": resource_id, "class": element_class, "text": text},
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


def input_action(field, text):
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


# A clean run: empty login fields first, single (not doubled) typed values in
# the following snapshots, sub-second step gaps.
def healthy_steps(gap_ms=600):
    return [
        step(1, gap_ms, login_screen("", ""), input_action("LoginEmail", "demo@folio.app")),
        step(2, gap_ms, login_screen("demo@folio.app", ""), input_action("LoginPassword", "ledger123")),
        step(3, gap_ms, login_screen("demo@folio.app", "ledger123")),
        step(4, gap_ms, login_screen("demo@folio.app", "ledger123")),
    ]


BENIGN_NOISE = [
    "companion pid=4242 listening on 127.0.0.1:51000\n",
    "objc[4242]: Class FBSDKError is implemented in both /a (0x1) and /b (0x2). "
    "One of the two will be used. Which one is undefined.\n",
    "WARNING: All log messages before absl::InitializeLog() is called are written to STDERR\n",
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
        step(1, 600, login_screen("stale@folio.app", "leftover"), input_action("LoginEmail", "demo@folio.app")),
        step(2, 600, login_screen("demo@folio.app", "leftover")),
    ],
)

# G4: after typing into LoginEmail the next snapshot shows the value appended to
# itself (append-vs-replace / double-paste regression).
write_run(
    "g4-doubled-text",
    [
        step(1, 600, login_screen("", ""), input_action("LoginEmail", "demo@folio.app")),
        step(2, 600, login_screen("demo@folio.appdemo@folio.app", "")),
    ],
)

# G5: step gaps far exceed the 2.5s ceiling, driving p95 over the limit.
write_run("g5-slow-p95", healthy_steps(gap_ms=4000))

print("fixtures written")
