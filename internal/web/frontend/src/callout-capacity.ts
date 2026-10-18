export type ReadingRect = { left: number; top: number; width: number; height: number };
export type CapacityPolicy = { key: string; list: boolean };
const intersects = (a: ReadingRect, b: ReadingRect, gap = 0) => a.left < b.left + b.width + gap && a.left + a.width + gap > b.left && a.top < b.top + b.height + gap && a.top + a.height + gap > b.top;

// Called at rest with measured labels and the map's actual control rectangles.
// A list retains every reading when floating labels cannot fit safely.
export function needsReadingList(labels: ReadingRect[], controls: ReadingRect[], width: number, height: number) {
  return labels.some((r, i) => ![r.left, r.top, r.width, r.height].every(Number.isFinite)
    || r.width <= 0 || r.height <= 0 || r.left < 0 || r.top < 0 || r.left + r.width > width || r.top + r.height > height
    || controls.some(c => intersects(r, c, 4)) || labels.slice(0, i).some(other => intersects(r, other, 2)));
}
