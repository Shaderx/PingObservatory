export const VIEWPORT_SOUTH = -60;
export const VIEWPORT_NORTH = 65;
// MapLibre wraps exact -180° and +180° bounds to the same world x coordinate.
export const WORLD_EDGE = 180 - 1e-6;
export const ENDPOINT_BOUNDS: [[number, number], [number, number]] = [
  [-WORLD_EDGE, VIEWPORT_SOUTH],
  [WORLD_EDGE, VIEWPORT_NORTH],
];

export function mercatorY(latitude: number) {
  const radians = latitude * Math.PI / 180;
  return (1 - Math.asinh(Math.tan(radians)) / Math.PI) / 2;
}

export const MAP_ASPECT_RATIO = (2 * WORLD_EDGE / 360) / (mercatorY(VIEWPORT_SOUTH) - mercatorY(VIEWPORT_NORTH));
