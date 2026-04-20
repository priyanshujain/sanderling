import type { ResidualNode } from "../types";
import "./ResidualNode.css";

export interface ResidualNodeProps {
  node: ResidualNode;
}

function OperatorLabel({ children }: { children: string }) {
  return <span className="op-label">{children}</span>;
}

function Wrapped({ node }: { node: ResidualNode }) {
  return (
    <span className="residual-group">
      (<ResidualNodeView node={node} />)
    </span>
  );
}

function ResidualNodeView({ node }: ResidualNodeProps) {
  switch (node.op) {
    case "true":
      return <span className="residual-leaf">true</span>;
    case "false":
      return <span className="residual-leaf">false</span>;
    case "predicate":
      return (
        <span className="residual-leaf">
          <OperatorLabel>pred</OperatorLabel>
          {node.name ? <>({node.name})</> : null}
        </span>
      );
    case "error":
      return (
        <span className="residual-error" role="status">
          <OperatorLabel>error</OperatorLabel>
          <span className="residual-error-message">{node.message}</span>
        </span>
      );
    case "always":
    case "now":
    case "next":
    case "not":
      return (
        <span className="residual-unary">
          <OperatorLabel>{node.op}</OperatorLabel>
          <Wrapped node={node.arg} />
        </span>
      );
    case "eventually":
      return (
        <span className="residual-unary">
          <OperatorLabel>eventually</OperatorLabel>
          {node.within ? (
            <span className="residual-bound">
              within {node.within.amount} {node.within.unit}
            </span>
          ) : null}
          <Wrapped node={node.arg} />
        </span>
      );
    case "and":
    case "or":
      return (
        <span className="residual-binary">
          <Wrapped node={node.left} />
          <OperatorLabel>{node.op}</OperatorLabel>
          <Wrapped node={node.right} />
        </span>
      );
    case "implies":
      return (
        <span className="residual-binary">
          <Wrapped node={node.left} />
          <OperatorLabel>{"=>"}</OperatorLabel>
          <Wrapped node={node.right} />
        </span>
      );
  }
}

export default function ResidualNodeRoot({ node }: ResidualNodeProps) {
  return (
    <span className="residual">
      <ResidualNodeView node={node} />
    </span>
  );
}
