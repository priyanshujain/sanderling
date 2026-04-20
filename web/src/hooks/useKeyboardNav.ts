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

export function useKeyboardNav(options: UseKeyboardNavOptions) {
  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      if (target && (NAVIGATION_TAGS.has(target.tagName) || target.isContentEditable)) {
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
