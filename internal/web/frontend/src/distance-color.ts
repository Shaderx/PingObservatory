import type { Observation, Target } from './types';
import { ageSeconds, isRTTMethod } from './model';

export function distanceTiming(target: Target, observation: Observation | undefined, origin: [number, number] | undefined, now: number, accessAllowanceMS = 10) {
  const destination = target.geography.coordinates;
  if (!origin || !destination || !observation || !isRTTMethod(observation.method) || observation.median_ms === null || !Number.isFinite(observation.median_ms) || observation.median_ms < 0 || ![...origin, ...destination, accessAllowanceMS].every(Number.isFinite) || accessAllowanceMS < 0 || ageSeconds(observation.observed_at, now) > 180 || !['ok', 'degraded'].includes(observation.status)) return undefined;
  const radians = (degrees: number) => degrees * Math.PI / 180;
  const [longitude, latitude] = destination, [originLongitude, originLatitude] = origin;
  const a = Math.sin(radians(latitude - originLatitude) / 2) ** 2 + Math.cos(radians(originLatitude)) * Math.cos(radians(latitude)) * Math.sin(radians(longitude - originLongitude) / 2) ** 2;
  const distanceKm = 6371 * 2 * Math.atan2(Math.sqrt(Math.min(1, a)), Math.sqrt(Math.max(0, 1 - a)));
  const referenceMS = Math.max(10, distanceKm * 0.01 + accessAllowanceMS);
  return { distanceKm, referenceMS, ratio: observation.median_ms / referenceMS };
}

export function distanceColor(target: Target, observation: Observation | undefined, origin: [number, number] | undefined, now: number, light: boolean, accessAllowanceMS = 10) {
  const timing = distanceTiming(target, observation, origin, now, accessAllowanceMS);
  if (!timing) return light ? '#4e5a4a' : '#9ca897';
  const progress = Math.max(0, Math.min(1, (timing.ratio - 1.5) / 1.5));
  // Dark text on the light theme and bright text on the dark theme. Numeric
  // readings, loss, failure status, and the formula disclosure remain visible.
  return `hsl(${110 * (1 - progress)} ${light ? 70 : 76}% ${light ? 22 : 69}%)`;
}
