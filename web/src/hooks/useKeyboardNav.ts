import { useEffect } from "react";

export interface UseKeyboardNavOptions {
  onPrev: () => void;
  onNext: () => void;
  onJumpStart: () => void;
  onJumpEnd: () => void;
  onJumpPrev10: () => void;
  onJumpNext10: () => void;
  onJumpNextViolation: () => void;
}

const NAVIGATION_TAGS = new Set(["INPUT", "TEXTAREA", "SELECT"]);
const ARROW_OWNING_ROLES = new Set(["tab", "tablist", "option", "listbox", "menuitem", "menu"]);

function targetOwnsArrowKeys(target: HTMLElement | null): boolean {
  let node: HTMLElement | null = target;
  while (node) {
    const role = node.getAttribute?.("role");
    if (role && ARROW_OWNING_ROLES.has(role)) return true;
    node = node.parentElement;
  }
  return false;
}

export function useKeyboardNav(options: UseKeyboardNavOptions) {
  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      if (target && (NAVIGATION_TAGS.has(target.tagName) || target.isContentEditable)) {
        return;
      }
      const isArrow = event.key.startsWith("Arrow");
      if (isArrow && targetOwnsArrowKeys(target)) {
        return;
      }
      if (event.metaKey || event.ctrlKey || event.altKey) {
        return;
      }
      switch (event.key) {
        case "ArrowLeft":
        case "k":
          if (event.shiftKey) {
            options.onJumpPrev10();
          } else {
            options.onPrev();
          }
          event.preventDefault();
          return;
        case "ArrowRight":
        case "j":
          if (event.shiftKey) {
            options.onJumpNext10();
          } else {
            options.onNext();
          }
          event.preventDefault();
          return;
        case "g":
          options.onJumpStart();
          event.preventDefault();
          return;
        case "G":
          options.onJumpEnd();
          event.preventDefault();
          return;
        case ".":
          options.onJumpNextViolation();
          event.preventDefault();
          return;
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [options]);
}
