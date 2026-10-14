import { expect, it } from 'vitest';
import { prepareConfig } from './config';
import type { Config } from './types';

const saved: Config = { origin: null, retention_days: 7, catalogue_url: '', manual_targets: [{ id: 'manual-local', revision: 3, label: 'Local', host: '127.0.0.1' }] };
it('retains a stable ID and increments the revision when the target changes', () => {
  expect(prepareConfig(saved, saved).manual_targets[0].revision).toBe(3);
  const changed = prepareConfig({ ...saved, manual_targets: [{ ...saved.manual_targets[0], host: '127.0.0.2', verified_at: '2026-10-14T12:00:00Z', verification_source: 'invalid claim', verification_method: 'tcp_connect', verification_family: 'ipv4', verification_destination: '127.0.0.2:443' }] }, saved).manual_targets[0];
  expect(changed).toEqual({ id: 'manual-local', revision: 4, label: 'Local', host: '127.0.0.2' });
});
it('treats a new manual target as revision one', () => {
  const result = prepareConfig({ ...saved, manual_targets: [{ id: 'manual-new', revision: 42, label: 'Test', host: '127.0.0.1' }] }, saved);
  expect(result.manual_targets[0].revision).toBe(1);
});
