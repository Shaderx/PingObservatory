import type { Config, LocalTarget, Method, ProbeSettings } from './types';
import { settings } from './model';

export function methodSettings(method: Method): ProbeSettings {
  return { method, families: ['ipv4'], interval_seconds: 60, packets_per_cycle: method === 'icmp' || method === 'udp_echo' ? 3 : 1, packet_spacing_ms: method === 'udp_echo' || method === 'icmp' ? 1000 : 0, timeout_ms: method === 'icmp' ? 1000 : 3000, payload_bytes: 32,
    ...(method === 'https' ? { http_method: 'HEAD', expected_status_codes: [200] } : {}),
    ...(method === 'dns' ? { dns_query_name: 'example.com', dns_query_type: 'A' } : {}),
  };
}
export function cleanManualTarget(target: LocalTarget): LocalTarget {
  const clean: LocalTarget = { id: target.id, revision: target.revision, label: target.label.trim(), host: target.host.trim(), measurement: settings(target) };
  for (const key of ['port', 'scheme', 'path', 'location', 'provider', 'category', 'region', 'world_area', 'country_code', 'role', 'tags'] as const) { if (target[key] !== undefined) Object.assign(clean, { [key]: target[key] }); }
  return clean;
}
export function prepareConfig(draft: Config, saved: Config): Config {
  if (draft.manual_targets.length > 16) throw new Error('Add at most 16 manual targets.');
  return { ...draft, manual_targets: draft.manual_targets.map(target => {
    const clean = cleanManualTarget(target), previous = saved.manual_targets.find(item => item.id === target.id);
    const comparable = (item: LocalTarget) => { const { revision: _revision, ...fields } = cleanManualTarget(item); return JSON.stringify(fields); };
    clean.revision = previous ? previous.revision + Number(comparable(clean) !== comparable(previous)) : 1;
    return clean;
  }) };
}
export function newManualTarget(): LocalTarget { return { id: `manual-${crypto.randomUUID()}`, revision: 1, label: '', host: '', measurement: methodSettings('icmp') }; }
