import { useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import "./Tabs.css";

export interface TabDefinition {
  id: string;
  label: string;
  content: ReactNode;
  badge?: ReactNode;
}

export interface TabsProps {
  tabs: TabDefinition[];
  defaultTabId?: string;
  ariaLabel?: string;
}

export default function Tabs({ tabs, defaultTabId, ariaLabel }: TabsProps) {
  const initial = defaultTabId && tabs.some((t) => t.id === defaultTabId) ? defaultTabId : tabs[0]?.id;
  const [activeId, setActiveId] = useState<string | undefined>(initial);
  const buttonRefs = useRef<Map<string, HTMLButtonElement>>(new Map());

  if (tabs.length === 0) return null;
  const active = tabs.find((t) => t.id === activeId) ?? tabs[0];

  const focusTab = (id: string) => {
    setActiveId(id);
    const node = buttonRefs.current.get(id);
    if (node) node.focus();
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const currentIndex = tabs.findIndex((t) => t.id === active.id);
    if (currentIndex < 0) return;
    switch (event.key) {
      case "ArrowRight":
      case "ArrowDown": {
        event.preventDefault();
        const next = tabs[(currentIndex + 1) % tabs.length];
        focusTab(next.id);
        return;
      }
      case "ArrowLeft":
      case "ArrowUp": {
        event.preventDefault();
        const prev = tabs[(currentIndex - 1 + tabs.length) % tabs.length];
        focusTab(prev.id);
        return;
      }
      case "Home": {
        event.preventDefault();
        focusTab(tabs[0].id);
        return;
      }
      case "End": {
        event.preventDefault();
        focusTab(tabs[tabs.length - 1].id);
        return;
      }
    }
  };

  const panelId = `${ariaLabel ?? "tabs"}-panel`.replace(/\s+/g, "-");

  return (
    <div className="tabs">
      <div
        className="tabs-header"
        role="tablist"
        aria-label={ariaLabel}
        onKeyDown={handleKeyDown}
      >
        {tabs.map((tab) => {
          const isActive = tab.id === active.id;
          return (
            <button
              key={tab.id}
              ref={(node) => {
                if (node) {
                  buttonRefs.current.set(tab.id, node);
                } else {
                  buttonRefs.current.delete(tab.id);
                }
              }}
              type="button"
              role="tab"
              id={`${panelId}-tab-${tab.id}`}
              className="tabs-tab"
              data-active={isActive ? "true" : "false"}
              aria-selected={isActive}
              aria-controls={panelId}
              tabIndex={isActive ? 0 : -1}
              onClick={() => setActiveId(tab.id)}
            >
              <span>{tab.label}</span>
              {tab.badge !== undefined ? <span className="tabs-badge">{tab.badge}</span> : null}
            </button>
          );
        })}
      </div>
      <div
        className="tabs-panel"
        role="tabpanel"
        id={panelId}
        aria-labelledby={`${panelId}-tab-${active.id}`}
      >
        {active.content}
      </div>
    </div>
  );
}
