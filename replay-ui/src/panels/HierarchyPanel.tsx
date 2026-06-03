import { useMemo, useState } from "react";
import type { HierarchyElement, Hierarchy } from "../types";
import "./HierarchyPanel.css";

interface HierarchyPanelProps {
  hierarchy?: Hierarchy;
}

export default function HierarchyPanel({ hierarchy }: HierarchyPanelProps): JSX.Element {
  const [filter, setFilter] = useState("");
  const elements = hierarchy?.elements ?? [];

  const rows = useMemo(() => {
    const lower = filter.trim().toLowerCase();
    if (!lower) return elements;
    return elements.filter((element) => matches(element, lower));
  }, [elements, filter]);

  if (elements.length === 0) {
    return <div className="hierarchy-empty">no hierarchy captured</div>;
  }

  return (
    <div className="hierarchy-panel">
      <input
        type="text"
        className="hierarchy-filter"
        placeholder="filter by id, text, desc, class, tag..."
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
      />
      <div className="hierarchy-table-wrap">
        <table className="hierarchy-table">
          <thead>
            <tr>
              <th>tag/class</th>
              <th>id</th>
              <th>text/desc</th>
              <th>bounds</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((element, index) => (
              <HierarchyRow key={index} element={element} />
            ))}
          </tbody>
        </table>
      </div>
      <div className="hierarchy-count">
        {rows.length} of {elements.length}
      </div>
    </div>
  );
}

function HierarchyRow({ element }: { element: HierarchyElement }): JSX.Element {
  const tag = element.tag ?? element.class ?? "";
  const text = (element.text ?? element.description ?? "").slice(0, 80);
  const bounds = element.bounds;
  return (
    <tr>
      <td className="hierarchy-tag">{tag}</td>
      <td className="hierarchy-id">{element.resourceId ?? ""}</td>
      <td className="hierarchy-text">{text}</td>
      <td className="hierarchy-bounds">
        [{bounds.left},{bounds.top},{bounds.right},{bounds.bottom}]
      </td>
    </tr>
  );
}

function matches(element: HierarchyElement, needle: string): boolean {
  const fields = [
    element.resourceId,
    element.text,
    element.description,
    element.class,
    element.tag,
  ];
  for (const value of fields) {
    if (value && value.toLowerCase().includes(needle)) return true;
  }
  if (element.attributes) {
    for (const value of Object.values(element.attributes)) {
      if (value.toLowerCase().includes(needle)) return true;
    }
  }
  return false;
}
