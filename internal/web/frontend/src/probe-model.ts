import type { Observation, Target } from './types';

export type ProbeActivity = 'unknown' | 'unavailable';

export function probeActivity(target: Target, observation: Observation | undefined, now: number): ProbeActivity {
  if (!target.enabled || !observation || !observation.observed_at) return 'unavailable';
  if (observation.status === 'na' || observation.status === 'unsupported') return 'unavailable';
  const observedAt = Date.parse(observation.observed_at);
  if (!Number.isFinite(observedAt) || observedAt > now) return 'unavailable';
  return 'unknown';
}
