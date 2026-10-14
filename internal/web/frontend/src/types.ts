export type Family = 'ipv4' | 'ipv6';
export type Method = 'icmp';
export type ProbeStatus = 'ok' | 'timeout' | 'unreachable' | 'dns_error' | 'permission_denied' | 'unsupported' | 'error';
export type Status = ProbeStatus | 'na' | 'stale';
export type Window = '1h' | '24h';
export type Point = { latitude: number; longitude: number };
export type LocalTarget = { id: string; revision: number; label: string; host: string; location?: Point; verified_at?: string; verification_source?: string; verification_method?: string; verification_family?: string; verification_destination?: string };
export type Measurement = { target_id: string; target_revision: number; at: string; status: ProbeStatus; latency_ms?: number; detail?: string };
export type Bucket = { minute: string; status: Status; measurement?: Measurement };
export type Config = { origin?: Point | null; manual_targets: LocalTarget[]; catalogue_url: string; retention_days: number };
export type State = { config: Config; targets: LocalTarget[]; latest: Measurement[] | Record<string, Measurement>; catalogue: { version: number; source: string; last_error: string }; collector: { started_at: string; last_error: string; active_probes: number }; csrf_token: string };

// The retained atlas consumes this presentation model. Only measured values
// enter it; rolling statistics and baselines are absent until implemented.
export type Observation = {
  family: Family; method: Method; status: Status; observed_at: string | null;
  median_ms: number | null; baseline_ms: number | null; rolling_as_of: string | null;
  loss_pct: number | null; detail: string;
};
export type Target = LocalTarget & {
  provider: string; region: string; enabled: boolean;
  geography: { coordinates: [number, number] | null };
  observations: Observation[];
};
export type History = { target_id: string; target_revision: number; window: Window; buckets: Bucket[]; points: { at: string; median_ms: number | null }[] };
