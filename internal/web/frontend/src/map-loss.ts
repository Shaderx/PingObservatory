import type { Observation } from './types';
import { isRTTMethod, lossValue, rollingStatus, statusLabel } from './model';
import { compactLoss } from './compact-display';

// The map uses the same observed denominator as the standalone statistics.
export function mapLoss(observation: Observation | undefined, now: number) {
  const rate = lossValue(observation);
  const value = typeof rate === 'number' && Number.isFinite(rate) && rate >= 0 && rate <= 100 ? rate : null;
  const freshness = rollingStatus(observation, now);
  return {
    unknown: value === null, visible: value !== null && value > 0, freshness,
    label: `One-hour ${isRTTMethod(observation?.method) ? 'packet loss rate' : 'failed-attempt rate'}: ${value === null ? 'unavailable' : compactLoss(value)}, ${statusLabel(freshness)}.`,
    scale: (value ?? 0) / 100,
    color: `hsl(0 ${40 + (value ?? 0) * 0.6}% ${65 - (value ?? 0) * 0.17}%)`,
  };
}
