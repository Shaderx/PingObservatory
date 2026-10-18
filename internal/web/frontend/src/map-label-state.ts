import type { Observation, Target } from './types';
import { ageSeconds, elevatedTiming, methods, metric, statusLabel, statusOf } from './model';
import { mapLoss } from './map-loss';

export type MapLabelCue = 'performance' | 'information' | 'failure' | null;
export function mapLabelState(target: Target, observation: Observation | undefined, now: number) {
  const status = statusOf(observation, now), loss = mapLoss(observation, now);
  const current = ageSeconds(observation?.observed_at, now) <= 180;
  const elevated = elevatedTiming(observation, now);
  const measuredFailure = current && !!observation && observation.sent > observation.unobserved_sent
    && ['timeout', 'unreachable', 'error'].includes(observation.status);
  let cue: MapLabelCue = null;
  if (measuredFailure) cue = 'failure';
  else if (!current || ['na', 'stale', 'unverified', 'disabled', 'unsupported', 'permission_denied', 'dns_error', 'unavailable', 'suspended', 'error', 'unreachable', 'timeout'].includes(status) || loss.unknown || loss.freshness !== 'fresh') cue = 'information';
  else if (elevated || loss.visible || status === 'degraded') cue = 'performance';
  const median = observation?.median_ms;
  const reasons = [target.label, `Provider: ${target.provider}. ${observation ? methods[observation.method] : 'Probe method unavailable'}.`,
    typeof median === 'number' && Number.isFinite(median) ? `Timing: ${metric(median)} milliseconds.` : 'Timing unavailable.',
    `${current ? 'Current result' : 'Last recorded result'}: ${statusLabel(observation?.status ?? 'na')}.`, loss.label];
  if (observation?.detail) reasons.push(observation.detail);
  if (!current) reasons.push(observation?.observed_at ? 'The last local reading is stale or its timestamp is invalid.' : 'No local result has been recorded for this series.');
  if (observation?.unable_to_measure) reasons.push(`${observation.unable_to_measure} unable-to-measure outcomes; unsent and unknown outcomes are excluded from loss or failure rates.`);
  if (elevated) reasons.push(`Timing is elevated against the fresh positive 24-hour baseline of ${metric(observation?.baseline_ms)} milliseconds: at least 50% and at least 10 milliseconds higher, with at least five samples.`);
  return { cue, elevatedTiming: elevated, loss, reasons, description: reasons.join(' ') };
}
