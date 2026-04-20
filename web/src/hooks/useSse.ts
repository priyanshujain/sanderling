import { useEffect } from "react";

export function useSse(eventName: string, onEvent: () => void) {
  useEffect(() => {
    if (typeof EventSource === "undefined") {
      return;
    }
    const source = new EventSource("/api/events");
    const messageHandler = (event: MessageEvent) => {
      try {
        const payload = JSON.parse(event.data);
        if (payload && typeof payload === "object" && payload.type === eventName) {
          onEvent();
        }
      } catch {
        onEvent();
      }
    };
    source.addEventListener("message", messageHandler);
    return () => {
      source.removeEventListener("message", messageHandler);
      source.close();
    };
  }, [eventName, onEvent]);
}
