import { describe, expect, it } from 'vitest';
import { historySummary, presentHistory, presentTargets, statusOf } from './model';
import type { Bucket, LocalTarget, Measurement, State } from './types';

const target: LocalTarget = { id: 'manual-test', revision: 2, label: 'Local test', host: '127.0.0.1' };
const at = '2026-10-14T12:00:00Z';
const state: State = { config: { manual_targets: [], retention_days: 7, catalogue_url: '' }, targets: [target], latest: [{ target_id: target.id, target_revision: 1, at, status: 'ok', latency_ms: 9 }], catalogue: { version: 1, source: 'bundled', last_error: '' }, collector: { started_at: at, active_probes: 0, last_error: '' }, csrf_token: 'test' };

describe('local measurement presentation', () => {
  it('preserves the exact catalogue verification metadata without inferring ICMP', () => {
    const metadata = { verified_at: at, verification_source: 'Owner observation', verification_method: 'tcp_connect', verification_family: 'ipv4', verification_destination: '127.0.0.1:443' };
    const presented = presentTargets({ ...state, targets: [{ ...target, ...metadata }] })[0];
    expect(presented).toMatchObject(metadata);
    expect(presented.observations[0].method).toBe('icmp');
  });
  it('excludes previous target revisions and preserves attempted failure status', () => {
    expect(presentTargets(state)[0].observations[0].status).toBe('na');
    const failed = presentTargets({ ...state, latest: [...state.latest as Measurement[], { target_id: target.id, target_revision: 2, at, status: 'timeout', detail: 'Echo timed out' }] })[0].observations[0];
    expect(failed.status).toBe('timeout');
    expect(failed.median_ms).toBeNull();
    expect(statusOf(failed, Date.parse(at) + 1000)).toBe('timeout');
    expect(statusOf(failed, Date.parse(at) + 181000)).toBe('stale');
    expect(failed.status).toBe('timeout');
  });
  it('keeps N/A out of the attempted failure rate and breaks chart lines', () => {
    const buckets: Bucket[] = [
      { minute: at, status: 'na' },
      { minute: '2026-10-14T12:01:00Z', status: 'timeout', measurement: { target_id: target.id, target_revision: 2, at, status: 'timeout' } },
      { minute: '2026-10-14T12:02:00Z', status: 'ok', measurement: { target_id: target.id, target_revision: 2, at, status: 'ok', latency_ms: 4 } },
    ];
    expect(historySummary(buckets)).toEqual({ observed: 1, failed: 1, missing: 1, failureRate: 50 });
    expect(presentHistory(target, '1h', buckets).points.map(point => point.median_ms)).toEqual([null, null, 4]);
  });
});
