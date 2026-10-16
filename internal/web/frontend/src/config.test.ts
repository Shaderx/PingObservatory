import { expect, it } from 'vitest';
import { prepareConfig } from './config';
import { settings } from './model';
import type { Config } from './types';

const saved: Config = { origin: null, retention_days: 7, catalogue_url: '', manual_targets: [{ id: 'manual-local', revision: 3, label: 'Local', host: '127.0.0.1' }] };
it('retains a stable ID and increments the revision when the target changes', () => {
  expect(prepareConfig(saved, saved).manual_targets[0].revision).toBe(3);
  const changed = prepareConfig({ ...saved, manual_targets: [{ ...saved.manual_targets[0], host: '127.0.0.2', verified_at: '2026-10-14T12:00:00Z', verification_source: 'invalid claim', verification_method: 'tcp_connect', verification_family: 'ipv4', verification_destination: '127.0.0.2:443' }] }, saved).manual_targets[0];
  expect(changed).toMatchObject({ id: 'manual-local', revision: 4, label: 'Local', host: '127.0.0.2' }); expect(changed).not.toHaveProperty('verified_at'); expect(changed).not.toHaveProperty('verification_method'); expect(changed).not.toHaveProperty('verification_family'); expect(changed).not.toHaveProperty('verification_destination'); expect(changed).not.toHaveProperty('verification_source');
});
it('treats a new manual target as revision one', () => {
  const result = prepareConfig({ ...saved, manual_targets: [{ id: 'manual-new', revision: 42, label: 'Test', host: '127.0.0.1' }] }, saved);
  expect(result.manual_targets[0].revision).toBe(1);
});
it('increments revision for probe settings and strips per-family verification claims', () => {
  const changed = prepareConfig({ ...saved, manual_targets: [{ ...saved.manual_targets[0], measurement: { ...settings(saved.manual_targets[0]), packets_per_cycle: 3 }, admission: { ipv4: { state: 'verified', evidence: { method: 'icmp', family: 'ipv4', destination: '127.0.0.1', observed_at: '2026-10-14T12:00:00Z', source_url: 'https://example.com' } } } }] }, saved).manual_targets[0];
  expect(changed.revision).toBe(4); expect(changed.measurement?.packets_per_cycle).toBe(3); expect(changed).not.toHaveProperty('admission');
});
