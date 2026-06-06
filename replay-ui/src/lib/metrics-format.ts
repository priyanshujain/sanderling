const MB = 1024 * 1024;

export function formatHeap(bytes: number): string {
  if (bytes <= 0) return "0B";
  if (bytes < MB) return `${Math.round(bytes / 1024)}K`;
  if (bytes < 1024 * MB) return `${Math.round(bytes / MB)}M`;
  return `${(bytes / (1024 * MB)).toFixed(1)}G`;
}

export function formatTime(millis: number): string {
  const safe = Math.max(0, Math.floor(millis));
  const seconds = Math.floor(safe / 1000);
  const mm = String(Math.floor(seconds / 60)).padStart(2, "0");
  const ss = String(seconds % 60).padStart(2, "0");
  return `${mm}:${ss}`;
}

export function fractionFor(index: number, count: number): number {
  if (count <= 1) return 0.5;
  return index / (count - 1);
}

export function buildPath<T>(
  samples: T[],
  getValue: (sample: T) => number | undefined,
  ceiling: number,
): string {
  const segments: string[] = [];
  let pendingCommand = "M";
  samples.forEach((sample, index) => {
    const value = getValue(sample);
    if (value === undefined) {
      pendingCommand = "M";
      return;
    }
    const x = fractionFor(index, samples.length);
    const ratio = ceiling === 0 ? 0 : Math.min(value / ceiling, 1);
    const y = 1 - ratio;
    segments.push(`${pendingCommand}${x.toFixed(4)},${y.toFixed(4)}`);
    pendingCommand = "L";
  });
  return segments.join(" ");
}
