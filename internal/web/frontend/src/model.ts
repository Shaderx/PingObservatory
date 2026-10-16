import type { Family, History, LocalTarget, Method, Observation, ProbeSettings, State, Target, Window } from './types';
import { historyWindows } from './history-windows';

export const methods: Record<Method, string> = { icmp: 'ICMP RTT', udp_echo: 'UDP echo RTT', tcp_connect: 'TCP connect', https: 'HTTPS duration', dns: 'DNS query' };
export const BASELINE_POLICY = 'A fresh successful batch is compared with its fresh, positive 24-hour baseline for the same target revision, method and address family. Timing is elevated only when it is at least 50% and at least 10 ms above baseline, and rolling-hour data is fresh.';
const labels: Record<string, string> = { ok: 'Observed', degraded: 'Degraded', na: 'N/A', stale: 'Stale', timeout: 'No response', unreachable: 'Unreachable', dns_error: 'DNS error', permission_denied: 'Permission denied', unsupported: 'Unsupported', error: 'Local error', suspended: 'N/A', unavailable: 'Unable to measure', fresh: 'Fresh', unverified: 'Unverified', disabled: 'Disabled' };
export function statusLabel(value: string) { return labels[value] ?? value.replaceAll('_', ' '); }
export function metric(value: number | null | undefined) { return value == null || !Number.isFinite(value) ? 'N/A' : value.toFixed(1); }
export function ageSeconds(at: string | null | undefined, now: number) { const time = at ? Date.parse(at) : NaN; return Number.isFinite(time) && time <= now ? (now - time) / 1000 : Infinity; }
export function ageText(at: string | null | undefined, now: number) { const age = ageSeconds(at, now); return !Number.isFinite(age) ? 'unavailable' : age < 60 ? `${Math.floor(age)}s ago` : age < 3600 ? `${Math.floor(age / 60)}m ago` : `${Math.floor(age / 3600)}h ago`; }
export function reading(target: Target, family: Family) { return target.observations.find(observation => observation.family === family && observation.method === target.measurement.method); }
export function rollingStatus(observation: Observation | undefined, now: number) { return !observation?.rolling_as_of ? 'unavailable' : ageSeconds(observation.rolling_as_of, now) > 180 ? 'stale' : 'fresh'; }
export function elevatedTiming(observation: Observation | undefined, now: number) {
  if (!observation || observation.status !== 'ok' || rollingStatus(observation, now) !== 'fresh' || ageSeconds(observation.observed_at, now) > 180 || ageSeconds(observation.baseline_as_of, now) > 180 || observation.baseline_sample_count < 5) return false;
  const median = observation.median_ms, baseline = observation.baseline_ms;
  return median !== null && baseline !== null && Number.isFinite(median) && Number.isFinite(baseline) && baseline > 0 && median >= baseline * 1.5 && median - baseline >= 10;
}
export function statusOf(observation: Observation | undefined, now: number): string {
  if (!observation) return 'na';
  if (['unverified', 'unsupported', 'disabled'].includes(observation.status)) return observation.status;
  if (observation.observed_at && ageSeconds(observation.observed_at, now) > 180) return 'stale';
  if (elevatedTiming(observation, now)) return 'degraded';
  if (observation.status === 'ok' && isRTTMethod(observation.method) && observation.sent - observation.unobserved_sent > 0 && observation.received < observation.sent - observation.unobserved_sent) return 'degraded';
  return observation.status;
}
export function isRTTMethod(method: string | undefined) { return method === 'icmp' || method === 'udp_echo'; }
export function lossLabel(method: string | undefined) { return isRTTMethod(method) ? 'LOSS' : 'FAILURES'; }
export function lossValue(observation: Observation | undefined) { return isRTTMethod(observation?.method) ? observation?.loss_pct : observation?.failure_pct; }
export function timingVariationLabel(method: string | undefined) { return isRTTMethod(method) ? 'RTT VARIATION' : 'TIMING VARIATION'; }
export function safeFavorites(raw: string | null): string[] { try { const parsed: unknown = JSON.parse(raw ?? '[]'); return Array.isArray(parsed) ? parsed.filter((value): value is string => typeof value === 'string').slice(0, 500) : []; } catch { return []; } }
export function settings(target: LocalTarget): ProbeSettings { return target.measurement ?? { method: 'icmp', families: ['ipv4'], interval_seconds: 60, packets_per_cycle: 1, packet_spacing_ms: 0, timeout_ms: 3000, payload_bytes: 32 }; }
export function emptyObservation(method: Method, family: Family, status: Observation['status'] = 'na', detail = ''): Observation { return { method, family, status, detail, observed_at: null, batch_started_at: null, batch_ended_at: null, sent: 0, unobserved_sent: 0, received: 0, attempted: 0, unable_to_measure: 0, median_ms: null, min_ms: null, max_ms: null, rolling_1h_sent: 0, rolling_1h_unobserved_sent: 0, rolling_1h_received: 0, loss_pct: null, failure_pct: null, p95_ms: null, stddev_ms: null, rolling_as_of: null, rolling_window_ended_at: null, last_success_at: null, baseline_ms: null, baseline_as_of: null, baseline_sample_count: 0 }; }
export function presentTargets(state: State): Target[] {
  return (state.targets ?? []).map(target => {
    const measurement = settings(target);
    const observations = (['ipv4', 'ipv6'] as Family[]).map(family => {
      const admission = target.admission?.[family];
      if (!measurement.families.includes(family)) return emptyObservation(measurement.method, family, 'unsupported', 'This target is not configured for this address family.');
      if (!target.id.startsWith('manual-') && admission?.state !== 'verified') return emptyObservation(measurement.method, family, admission?.state ?? 'unverified', admission?.reason ?? 'No successful owner verification for this exact method and family. Automatic probing is disabled.');
      return state.statistics?.find(series => series.target_id === target.id && series.target_revision === target.revision && series.observation.method === measurement.method && series.observation.family === family)?.observation ?? emptyObservation(measurement.method, family);
    });
    return { ...target, provider: target.provider || 'User-provided', category: target.category || 'manual', region: target.region || target.label, world_area: target.world_area || 'User location', country_code: target.country_code || '', role: target.role || '', measurement, enabled: true, geography: { coordinates: target.location ? [target.location.longitude, target.location.latitude] : null }, observations };
  });
}
export function comparable(selected: Target[], candidate: Target, family: Family) { return selected.length < 4 && candidate.measurement.families.includes(family) && selected.every(target => target.measurement.method === candidate.measurement.method && target.measurement.families.includes(family)); }
export function historyPath(target: LocalTarget, family: Family, window: Window) { return `/api/history?target_id=${encodeURIComponent(target.id)}&revision=${target.revision}&method=${settings(target).method}&family=${family}&window=${window}`; }
export function selectCompatibleHistories(histories: History[], targets: LocalTarget[], family: Family, window: Window) {
  const selected: History[] = [];
  for (const target of targets) {
    const match = histories.find(history => history.family === family && history.window === window && history.target_id === target.id && history.target_revision === target.revision && history.method === settings(target).method);
    if (!match) return [];
    selected.push(match);
  }
  return selected;
}
export function presentHistory(target: LocalTarget, family: Family, window: Window, history: Omit<History, 'points'>): History {
  const spec = historyWindows[window];
  if (history.target_id !== target.id || history.target_revision !== target.revision || history.method !== settings(target).method || history.family !== family || history.window !== window || history.step_seconds !== spec.bucketSeconds || history.buckets.length > spec.count) throw new Error('Local history does not match this target, method, family and window.');
  return { ...history, points: history.buckets.map(bucket => ({ at: bucket.minute, median_ms: bucket.retention_expired ? null : bucket.median_ms, loss_pct: bucket.loss_pct, failure_pct: bucket.failure_pct })) };
}
export function historySummary(history: History) {
  const totals = history.buckets.reduce((sum, bucket) => ({ sent: sum.sent + bucket.sent, unobservedSent: sum.unobservedSent + bucket.unobserved_sent, received: sum.received + bucket.received, attempted: sum.attempted + bucket.attempted, unable: sum.unable + bucket.unable_to_measure, observed: sum.observed + Number(bucket.median_ms !== null), missing: sum.missing + Number(bucket.status === 'na' && !bucket.retention_expired), expired: sum.expired + Number(bucket.retention_expired) }), { sent: 0, unobservedSent: 0, received: 0, attempted: 0, unable: 0, observed: 0, missing: 0, expired: 0 });
  const latestRate = [...history.buckets].reverse().find(bucket => (isRTTMethod(history.method) ? bucket.loss_pct : bucket.failure_pct) !== null);
  return { ...totals, latestRate: latestRate ? isRTTMethod(history.method) ? latestRate.loss_pct : latestRate.failure_pct : null };
}
