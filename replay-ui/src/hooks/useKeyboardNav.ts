import { useEffect } from "react";
import { dispatchKey, type KeyboardNavOptions } from "../lib/keyboard-nav";

export type UseKeyboardNavOptions = KeyboardNavOptions;

export function useKeyboardNav(options: UseKeyboardNavOptions) {
  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      const handled = dispatchKey(
        {
          key: event.key,
          shiftKey: event.shiftKey,
          metaKey: event.metaKey,
          ctrlKey: event.ctrlKey,
          altKey: event.altKey,
          target: event.target as HTMLElement | null,
        },
        options,
      );
      if (handled) {
        event.preventDefault();
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [options]);
}
