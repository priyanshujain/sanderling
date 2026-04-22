import { useEffect } from "react";

export function useSse(eventName: string, onEvent: () => void) {
  useEffect(() => {
    if (typeof EventSource === "undefined") {
      return;
    }
    const source = new EventSource("/api/events");
    const messageHandler = () => {
      onEvent();
    };
    source.addEventListener(eventName, messageHandler);
    return () => {
      source.removeEventListener(eventName, messageHandler);
      source.close();
    };
  }, [eventName, onEvent]);
}
