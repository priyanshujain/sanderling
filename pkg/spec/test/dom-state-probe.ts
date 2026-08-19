/// <reference lib="dom" />

// The V8 host's element handle, read back state field by state field.
//
// internal/driver/chrome/element_state_test.go bundles this probe into a live
// page and compares what it returns against the hierarchy dump the goja host
// reads off the SAME page, before and after a real click. buildAx is the
// shipped `state.ax`, so the answer comes from production code rather than a
// copy of it. `attrChecked` rides along because the markup attribute is what an
// implementation reading the wrong source would report, and a failure naming it
// says so instead of just naming the wrong boolean.

import { __testing__ } from "../src/web-runtime.ts";

const { buildAx } = __testing__;

interface Handle {
  checked?: boolean;
  enabled?: boolean;
  focused?: boolean;
  selected?: boolean;
  attrs?: Record<string, string>;
}

interface Ax {
  find(selector: unknown): Handle | undefined;
}

function elementState(id: string): unknown {
  const handle = (buildAx() as Ax).find({ id });
  if (!handle) return null;
  return {
    checked: handle.checked,
    enabled: handle.enabled,
    focused: handle.focused,
    selected: handle.selected,
    attrChecked: handle.attrs?.checked ?? null,
  };
}

type StateGlobal = { __sanderlingElementState__: (id: string) => unknown };

(globalThis as unknown as StateGlobal).__sanderlingElementState__ = elementState;
