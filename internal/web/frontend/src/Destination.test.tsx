import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { DestinationDetails } from './Destination';
import { emptyObservation, presentTargets, reading, rollingStatus, statusOf } from './model';
import type { DestinationState, LocalTarget, State } from './types';

const at = '2026-10-14T12:00:00Z';
const destination: DestinationState = { baseline_ip: '1.1.1.1', baseline_observed_at: '2026-10-14T11:50:00Z', last_ip: '1.1.1.1', last_observed_at: at, change_count: 2, history_limit: 64, history: [{ observed_at: '2026-10-14T11:55:00Z', old_ip: '1.1.1.1', new_ip: '8.8.8.8' }, { observed_at: at, old_ip: '8.8.8.8', new_ip: '1.1.1.1' }] };

describe('local destination provenance', () => {
  it('keeps unknown local peers separate from catalogue evidence', () => {
    const html = renderToStaticMarkup(<DestinationDetails/>);
    expect(html).toContain('No local destination has been captured.');
    expect(html).toContain('Catalogue evidence does not seed this history.');
    expect(rollingStatus(emptyObservation('icmp', 'ipv4'), Date.parse(at))).toBe('na');
  });
  it('shows bounded informational changes and actual capture times without changing health', () => {
    const html = renderToStaticMarkup(<DestinationDetails destination={destination}/>);
    expect(html).toContain('2 observed changes');
    expect(html).toContain('Last attempted address');
    expect(html).toContain('Captured at batch completion');
    expect(html).toContain('First captured baseline');
    expect(html).toContain('8.8.8.8');
    expect(html).toContain('destination changes do not alter endpoint health');
    expect(html).toContain('Up to 64');
    const local = { ...emptyObservation('icmp', 'ipv4'), status: 'ok' as const, destination, destination_ip: destination.last_ip };
    expect(statusOf(local, Date.parse(at))).toBe('ok');
  });
  it('preserves saved local destination state when admission becomes pending', () => {
    const target: LocalTarget = { id: 'catalogue-peer', revision: 1, label: 'Fixture', host: 'fixture.example', admission: { ipv4: { state: 'unverified', reason: 'No retained curator success.' } } };
    const state: State = { config: { retention_days: 30, catalogue_url: '', manual_targets: [] }, targets: [target], latest: [], statistics: [{ target_id: target.id, target_revision: 1, observation: { ...emptyObservation('icmp', 'ipv4'), destination } }], catalogue: { version: 5, source: 'remote', last_error: '' }, collector: { started_at: at, last_error: '', active_probes: 0, enabled: false }, csrf_token: 'fixture' };
    const shown = reading(presentTargets(state)[0], 'ipv4');
    expect(shown?.status).toBe('unverified');
    expect(shown?.destination).toEqual(destination);
    expect(shown?.observed_at).toBeNull();
    expect(shown?.sent).toBe(0);
  });
});
