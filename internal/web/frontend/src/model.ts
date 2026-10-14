import type { Bucket, Family, History, LocalTarget, Measurement, Observation, State, Target, Window } from './types';

const labels: Record<string, string> = { ok: 'Observed', na: 'N/A', stale: 'Stale', timeout: 'Timeout', unreachable: 'Unreachable', dns_error: 'DNS error', permission_denied: 'Permission denied', unsupported: 'Unsupported', error: 'Probe error', unavailable: 'Unavailable', fresh: 'Fresh' };
export function statusLabel(value: string) { return labels[value] ?? value; }
export function metric(value: number | null | undefined) { return value == null || !Number.isFinite(value) ? 'N/A' : value.toFixed(1); }
export function ageSeconds(at: string | null | undefined, now: number) { const time = at ? Date.parse(at) : NaN; return Number.isFinite(time) && time <= now ? (now - time) / 1000 : Infinity; }
export function ageText(at: string | null | undefined, now: number) { const age = ageSeconds(at, now); return !Number.isFinite(age) ? 'unavailable' : age < 60 ? `${Math.floor(age)}s ago` : age < 3600 ? `${Math.floor(age / 60)}m ago` : `${Math.floor(age / 3600)}h ago`; }
export function reading(target: Target, _family: Family) { return target.observations[0]; }
export function rollingStatus(observation: Observation | undefined, now: number) { return !observation?.rolling_as_of ? 'unavailable' : ageSeconds(observation.rolling_as_of, now) > 180 ? 'stale' : 'fresh'; }
export function statusOf(observation: Observation | undefined, now: number): string {
  if (!observation || !observation.observed_at) return 'na';
  // Retain the attempted result. Age is separate from probe status so an old
  // timeout cannot turn into loss during an unobserved minute.
  return ageSeconds(observation.observed_at, now) > 180 ? 'stale' : observation.status;
}
export function isRTTMethod(method: string | undefined) { return method === 'icmp'; }
export function safeFavorites(raw: string | null): string[] { try { const parsed: unknown = JSON.parse(raw ?? '[]'); return Array.isArray(parsed) ? parsed.filter((value): value is string => typeof value === 'string').slice(0, 200) : []; } catch { return []; } }
export function presentTarget(target: LocalTarget, latest: Measurement | undefined): Target {
  const family: Family = target.host.includes(':') ? 'ipv6' : 'ipv4';
  const observation: Observation = { family, method: 'icmp', status: latest?.status ?? 'na', observed_at: latest?.at ?? null, median_ms: latest?.status === 'ok' ? latest.latency_ms ?? null : null, detail: latest?.detail ?? '', baseline_ms: null, rolling_as_of: null, loss_pct: null };
  return { ...target, provider: 'ICMP', region: target.label, enabled: true, geography: { coordinates: target.location ? [target.location.longitude, target.location.latitude] : null }, observations: [observation] };
}
export function presentTargets(state: State): Target[] {
  const values = Array.isArray(state.latest) ? state.latest : Object.values(state.latest ?? {});
  return (state.targets ?? []).map(target => {
    const latest = values.filter(item => item.target_id === target.id && item.target_revision === target.revision).sort((a, b) => Date.parse(b.at) - Date.parse(a.at))[0];
    return presentTarget(target, latest);
  });
}
export function historyPath(target: LocalTarget, window: Window) { return `/api/history?target_id=${encodeURIComponent(target.id)}&revision=${target.revision}&minutes=${window === '24h' ? 1440 : 60}`; }
export function presentHistory(target: LocalTarget, window: Window, buckets: Bucket[]): History { return { target_id: target.id, target_revision: target.revision, window, buckets, points: buckets.map(bucket => ({ at: bucket.minute, median_ms: bucket.status === 'ok' && bucket.measurement?.status === 'ok' ? bucket.measurement.latency_ms ?? null : null })) }; }
export function historySummary(buckets: Bucket[]) {
  const attempted = buckets.filter(bucket => !!bucket.measurement);
  const observed = attempted.filter(bucket => bucket.measurement?.status === 'ok');
  const failed = attempted.filter(bucket => bucket.measurement?.status !== 'ok');
  return { observed: observed.length, failed: failed.length, missing: buckets.filter(bucket => bucket.status === 'na').length, failureRate: attempted.length ? failed.length / attempted.length * 100 : null };
}
