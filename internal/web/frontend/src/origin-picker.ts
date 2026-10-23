import type { Point } from './types';

// A tenth of a degree is sufficient for the display reference and avoids
// recording a more precise position than the user picked on this map.
export function approximateOrigin(longitude: number, latitude: number): Point | null {
  if (!Number.isFinite(longitude) || !Number.isFinite(latitude) || latitude < -90 || latitude > 90) return null;
  const wrapped = ((longitude + 180) % 360 + 360) % 360 - 180;
  return { latitude: Math.round(latitude * 10) / 10 || 0, longitude: Math.round(wrapped * 10) / 10 || 0 };
}
