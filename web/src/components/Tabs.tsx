import { useState, type ReactNode } from "react";
import "./Tabs.css";

export interface TabDefinition {
  id: string;
  label: string;
  content: ReactNode;
}

export interface TabsProps {
  tabs: TabDefinition[];
  defaultTabId?: string;
  ariaLabel?: string;
}

export default function Tabs({ tabs, defaultTabId, ariaLabel }: TabsProps) {
  const initial = defaultTabId && tabs.some((t) => t.id === defaultTabId) ? defaultTabId : tabs[0]?.id;
  const [activeId, setActiveId] = useState<string | undefined>(initial);

  if (tabs.length === 0) return null;
  const active = tabs.find((t) => t.id === activeId) ?? tabs[0];

  return (
    <div className="tabs" role="tablist" aria-label={ariaLabel}>
      <div className="tabs-header">
        {tabs.map((tab) => (
          <button
            key={tab.id}
            type="button"
            role="tab"
            className="tabs-tab"
            data-active={tab.id === active.id ? "true" : "false"}
            aria-selected={tab.id === active.id}
            onClick={() => setActiveId(tab.id)}
          >
            {tab.label}
          </button>
        ))}
      </div>
      <div className="tabs-panel" role="tabpanel">
        {active.content}
      </div>
    </div>
  );
}
