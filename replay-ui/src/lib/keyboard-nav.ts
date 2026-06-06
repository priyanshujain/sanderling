export interface KeyboardNavOptions {
  onPrev: () => void;
  onNext: () => void;
  onJumpStart: () => void;
  onJumpEnd: () => void;
  onJumpPrev10: () => void;
  onJumpNext10: () => void;
  onJumpNextViolation: () => void;
}

export interface KeyEvent {
  key: string;
  shiftKey?: boolean;
  metaKey?: boolean;
  ctrlKey?: boolean;
  altKey?: boolean;
  target?: HTMLElement | null;
}

const NAVIGATION_TAGS = new Set(["INPUT", "TEXTAREA", "SELECT"]);
const ARROW_OWNING_ROLES = new Set([
  "tab",
  "tablist",
  "option",
  "listbox",
  "menuitem",
  "menu",
]);

export function targetOwnsArrowKeys(target: HTMLElement | null): boolean {
  let node: HTMLElement | null = target;
  while (node) {
    const role = node.getAttribute?.("role");
    if (role && ARROW_OWNING_ROLES.has(role)) return true;
    node = node.parentElement;
  }
  return false;
}

// Returns true when the key was consumed (caller should preventDefault).
export function dispatchKey(
  event: KeyEvent,
  options: KeyboardNavOptions,
): boolean {
  const target = event.target ?? null;
  if (target && (NAVIGATION_TAGS.has(target.tagName) || target.isContentEditable)) {
    return false;
  }
  if (event.key.startsWith("Arrow") && targetOwnsArrowKeys(target)) {
    return false;
  }
  if (event.metaKey || event.ctrlKey || event.altKey) {
    return false;
  }
  switch (event.key) {
    case "ArrowLeft":
    case "k":
      (event.shiftKey ? options.onJumpPrev10 : options.onPrev)();
      return true;
    case "ArrowRight":
    case "j":
      (event.shiftKey ? options.onJumpNext10 : options.onNext)();
      return true;
    case "g":
      options.onJumpStart();
      return true;
    case "G":
      options.onJumpEnd();
      return true;
    case ".":
      options.onJumpNextViolation();
      return true;
    default:
      return false;
  }
}
