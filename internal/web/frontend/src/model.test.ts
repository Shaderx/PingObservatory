import { describe, expect, it } from 'vitest';
import { comparable, elevatedTiming, emptyObservation, historyPath, historySummary, lossValue, presentHistory, presentTargets, reading, selectCompatibleHistories, settings, statusOf } from './model';
import type { Bucket, History, LocalTarget, Observation, State } from './types';
import { createHistoryChartOption } from './HistoryChart';

const at = '2026-10-14T12:00:00Z', now = Date.parse(at);
const target: LocalTarget = { id: 'manual-test', revision: 2, label: 'Local test', host: 'localhost' };
const observation: Observation = { ...emptyObservation('icmp', 'ipv4'), status: 'ok', observed_at: at, batch_ended_at: at, median_ms: 30, min_ms: 25, max_ms: 35, sent: 3, received: 3, rolling_1h_sent: 180, rolling_1h_received: 175, rolling_as_of: at, rolling_window_ended_at: at, baseline_ms: 20, baseline_as_of: at, baseline_sample_count: 5, stddev_ms: 4, loss_pct: 100 * 5 / 180 };
const state: State = { config: { manual_targets: [], retention_days: 30, catalogue_url: '' }, targets: [target], latest: [], statistics: [{ target_id: target.id, target_revision: 2, observation }], catalogue: { version: 1, source: 'bundled', last_error: '' }, collector: { started_at: at, active_probes: 0, last_error: '', enabled: false }, csrf_token: 'test' };
function bucket(changes: Partial<Bucket> = {}): Bucket { return { retention_expired: false, minute: at, end: '2026-10-14T12:01:00Z', status: 'na', sent: 0, unobserved_sent: 0, received: 0, attempted: 0, unable_to_measure: 0, median_ms: null, min_ms: null, max_ms: null, p95_ms: null, stddev_ms: null, loss_pct: null, failure_pct: null, ...changes }; }
function history(changes: Partial<History> = {}): History { return { target_id: target.id, target_revision: target.revision, method: 'icmp', family: 'ipv4', window: '1h', step_seconds: 60, start: at, end: '2026-10-14T13:00:00Z', as_of: at, retention_days: 30, retained_from: '2026-09-14T12:00:00Z', buckets: [], points: [], ...changes }; }

describe('local observation fidelity', () => {
  it('keeps current and rolling totals and variance separate', () => {
    const current = reading(presentTargets(state)[0], 'ipv4')!;
    expect(current.sent).toBe(3); expect(current.rolling_1h_sent).toBe(180);
    expect(current.received).toBe(3); expect(current.rolling_1h_received).toBe(175);
    expect(current.stddev_ms).toBe(4); expect(lossValue(current)).toBe(observation.loss_pct);
  });
  it('uses the target revision, method and family key without importing other series', () => {
    const wrong = { ...state, statistics: [{ target_id: target.id, target_revision: 1, observation }, { target_id: target.id, target_revision: 2, observation: { ...observation, method: 'udp_echo' as const } }] };
    expect(reading(presentTargets(wrong)[0], 'ipv4')?.status).toBe('na');
    expect(reading(presentTargets(state)[0], 'ipv6')?.status).toBe('unsupported');
  });
  it('preserves explicit inability errors and excludes unavailable send outcomes from degradation', () => {
    const unable = { ...observation, status: 'permission_denied' as const, sent: 0, received: 0, unable_to_measure: 1, median_ms: null, loss_pct: null };
    expect(statusOf(unable, now)).toBe('permission_denied'); expect(lossValue(unable)).toBeNull();
    const localErrorAfterSend = { ...observation, sent: 3, received: 2, unobserved_sent: 1, median_ms: 10 };
    expect(statusOf(localErrorAfterSend, now)).toBe('ok');
  });
  it('requires a fresh positive baseline with five samples and both elevation thresholds', () => {
    expect(elevatedTiming(observation, now)).toBe(true);
    for (const changed of [{ baseline_ms: 0 }, { baseline_sample_count: 4 }, { median_ms: 29.99 }, { baseline_as_of: '2026-10-14T11:56:59Z' }, { rolling_as_of: null }]) expect(elevatedTiming({ ...observation, ...changed }, now)).toBe(false);
    expect(statusOf({ ...observation, status: 'timeout' }, now)).toBe('timeout');
    expect(statusOf(observation, now + 181000)).toBe('stale');
  });
  it('keeps packet loss exclusive to RTT and preserves non-RTT failure rate', () => {
    const tcp = { ...observation, method: 'tcp_connect' as const, loss_pct: null, failure_pct: 25 };
    expect(lossValue(tcp)).toBe(25); expect(lossValue(observation)).toBe(observation.loss_pct);
  });
  it('does not use verification for another protocol or family to admit a catalogue target', () => {
    const catalogue = { ...target, id: 'public-test', measurement: settings(target), admission: { ipv4: { state: 'unverified' as const, reason: 'Different protocol evidence' } }, verification_method: 'tcp_connect', verification_family: 'ipv4', verification_destination: '127.0.0.1:443' };
    const presented = presentTargets({ ...state, targets: [catalogue] })[0];
    expect(reading(presented, 'ipv4')?.status).toBe('unverified');
    expect(presented.verification_method).toBe('tcp_connect');
  });
  it('limits comparisons to four targets with matching methods', () => {
    const presented = presentTargets(state)[0];
    expect(comparable([presented], { ...presented, measurement: { ...presented.measurement, method: 'tcp_connect' } }, 'ipv4')).toBe(false);
    expect(comparable([presented, presented, presented, presented], presented, 'ipv4')).toBe(false);
  });
});
describe('bounded history view contract', () => {
  it('excludes the previous comparison while a new family is loading', () => {
    const compared = Array.from({ length: 4 }, (_, index) => ({ ...target, id: `manual-compare-${index}` }));
    const previous = compared.map(item => history({ target_id: item.id }));
    expect(selectCompatibleHistories(previous, compared, 'ipv4', '1h')).toHaveLength(4);
    const selected = compared.slice(0, 1);
    expect(selectCompatibleHistories(previous, selected, 'ipv6', '1h')).toEqual([]);
    const refreshed = history({ target_id: selected[0].id, family: 'ipv6' });
    expect(selectCompatibleHistories([...previous, refreshed], selected, 'ipv6', '1h')).toEqual([refreshed]);
  });
  it('withholds a partial comparison when a middle target revision changes', () => {
    const compared = Array.from({ length: 3 }, (_, index) => ({ ...target, id: `manual-compare-${index}` }));
    const previous = compared.map(item => history({ target_id: item.id }));
    const updated = compared.map((item, index) => index === 1 ? { ...item, revision: item.revision + 1 } : item);
    expect(selectCompatibleHistories(previous, updated, 'ipv4', '1h')).toEqual([]);
    const next = history({ target_id: updated[1].id, target_revision: updated[1].revision });
    const displayed = selectCompatibleHistories([...previous].reverse().concat(next), updated, 'ipv4', '1h');
    expect(displayed.map(item => [item.target_id, item.target_revision])).toEqual(updated.map(item => [item.id, item.revision]));
  });
  it('requests the exact method/family and all four named windows', () => {
    for (const window of ['1h', '24h', '7d', '30d'] as const) expect(historyPath(target, 'ipv4', window)).toContain(`method=icmp&family=ipv4&window=${window}`);
  });
  it('preserves missing, failed and unable buckets without inferring loss from raw sends', () => {
    const input = history({ buckets: [bucket(), bucket({ status: 'unavailable', sent: 2, unobserved_sent: 2, unable_to_measure: 2 }), bucket({ status: 'ok', sent: 3, received: 2, median_ms: 4, loss_pct: 100 / 3 })] });
    const presented = presentHistory(target, 'ipv4', '1h', input);
    expect(presented.points.map(point => point.median_ms)).toEqual([null, null, 4]);
    expect(historySummary(presented)).toMatchObject({ missing: 1, unable: 2, sent: 5, unobservedSent: 2, latestRate: 100 / 3 });
  });
  it('rejects a mismatched history series or grid', () => {
    expect(() => presentHistory(target, 'ipv4', '1h', history({ family: 'ipv6' }))).toThrow();
    expect(() => presentHistory(target, 'ipv4', '1h', history({ step_seconds: 30 }))).toThrow();
  });
  it('separates expired coverage from stopped or unobserved N/A buckets', () => {
    expect(historySummary(history({ buckets: [bucket({ retention_expired: true }), bucket()] }))).toMatchObject({ expired: 1, missing: 1, latestRate: null });
  });
  it('keeps long-window chart bounds and breaks missing timings', () => {
    const long = history({ window: '7d', start: '2026-10-07T12:00:00Z', end: at, points: [{ at, median_ms: null, loss_pct: null, failure_pct: null }] });
    const option = createHistoryChartOption([long], ['Test'], 'dark');
    expect(option.xAxis.min).toBe(Date.parse(long.start)); expect(option.xAxis.max).toBe(now);
    expect(option.series[0].connectNulls).toBe(false); expect(option.series[0].data[0][1]).toBeNull();
  });
});
