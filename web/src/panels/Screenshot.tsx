import { useEffect, useId, useRef, useState } from "react";
import type { Action } from "../types";
import "./Screenshot.css";

export interface ScreenshotProps {
  src?: string;
  action?: Action;
  deviceWidth?: number;
  deviceHeight?: number;
}

const DEFAULT_WIDTH = 1080;
const DEFAULT_HEIGHT = 1920;

export default function Screenshot({ src, action, deviceWidth, deviceHeight }: ScreenshotProps) {
  const [naturalSize, setNaturalSize] = useState<{ width: number; height: number } | null>(null);
  const [loadFailed, setLoadFailed] = useState(false);
  const previousSrc = useRef<string | undefined>(src);
  const reactId = useId();
  const maskId = `screenshot-spotlight-${reactId.replace(/:/g, "")}`;

  useEffect(() => {
    if (previousSrc.current !== src) {
      previousSrc.current = src;
      setLoadFailed(false);
      setNaturalSize(null);
    }
  }, [src]);

  if (!src || loadFailed) {
    return (
      <div className="screenshot-placeholder" data-testid="screenshot-placeholder">
        no screenshot
      </div>
    );
  }

  const width = deviceWidth ?? naturalSize?.width ?? DEFAULT_WIDTH;
  const height = deviceHeight ?? naturalSize?.height ?? DEFAULT_HEIGHT;
  const stroke = Math.max(2, width / 200);
  const tapRadius = Math.max(12, width / 60);
  const arrowHeadSize = Math.max(18, width / 40);
  const spotlightRadius = tapRadius * 3;

  const bounds = action?.resolvedBounds;
  const tap = action?.tapPoint;
  const isSwipe =
    action?.kind === "Swipe" &&
    action.from_x !== undefined &&
    action.from_y !== undefined &&
    action.to_x !== undefined &&
    action.to_y !== undefined;

  return (
    <div
      className="screenshot-frame"
      style={{ aspectRatio: `${width} / ${height}` }}
    >
      <img
        className="screenshot-image"
        src={src}
        alt="device screenshot"
        onLoad={(event) => {
          const img = event.currentTarget;
          if (img.naturalWidth > 0 && img.naturalHeight > 0) {
            setNaturalSize({ width: img.naturalWidth, height: img.naturalHeight });
          }
        }}
        onError={() => setLoadFailed(true)}
      />
      <svg
        className="screenshot-overlay"
        viewBox={`0 0 ${width} ${height}`}
        preserveAspectRatio="xMidYMid meet"
        aria-hidden="true"
      >
        {tap && (
          <>
            <defs>
              <radialGradient
                id={`${maskId}-gradient`}
                cx={tap.x}
                cy={tap.y}
                r={spotlightRadius}
                gradientUnits="userSpaceOnUse"
              >
                <stop offset="0%" stopColor="black" />
                <stop offset="55%" stopColor="black" />
                <stop offset="100%" stopColor="white" />
              </radialGradient>
              <mask id={maskId} maskUnits="userSpaceOnUse" x="0" y="0" width={width} height={height}>
                <rect x="0" y="0" width={width} height={height} fill={`url(#${maskId}-gradient)`} />
              </mask>
            </defs>
            <rect
              data-testid="screenshot-spotlight"
              x="0"
              y="0"
              width={width}
              height={height}
              fill="black"
              opacity="0.5"
              mask={`url(#${maskId})`}
            />
          </>
        )}
        {bounds && (
          <rect
            x={bounds.x}
            y={bounds.y}
            width={bounds.width}
            height={bounds.height}
            fill="none"
            stroke="var(--accent-violation)"
            strokeWidth={stroke}
          />
        )}
        {isSwipe && (
          <SwipeArrow
            fromX={action!.from_x!}
            fromY={action!.from_y!}
            toX={action!.to_x!}
            toY={action!.to_y!}
            stroke={stroke}
            headSize={arrowHeadSize}
          />
        )}
      </svg>
    </div>
  );
}

interface SwipeArrowProps {
  fromX: number;
  fromY: number;
  toX: number;
  toY: number;
  stroke: number;
  headSize: number;
}

function SwipeArrow({ fromX, fromY, toX, toY, stroke, headSize }: SwipeArrowProps) {
  const angle = (Math.atan2(toY - fromY, toX - fromX) * 180) / Math.PI;
  const half = headSize / 2;
  const points = `${toX},${toY} ${toX - headSize},${toY - half} ${toX - headSize},${toY + half}`;
  return (
    <g>
      <line
        x1={fromX}
        y1={fromY}
        x2={toX}
        y2={toY}
        stroke="var(--text-muted)"
        strokeWidth={stroke}
        strokeLinecap="round"
      />
      <polygon
        points={points}
        fill="var(--text-muted)"
        transform={`rotate(${angle} ${toX} ${toY})`}
      />
    </g>
  );
}
