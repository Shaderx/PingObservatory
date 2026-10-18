import type { Family, Method, Target } from './types';

const MAX_AGE_MS = 180_000;
export const ARRIVAL_WINDOW_MS = 900;

export type BatchArrivalTracker = {
  latestByKey: ReadonlyMap<string, number>;
  arrivals: ReadonlyMap<string, number>;
  initialized: boolean;
  deliveryUnavailable: boolean;
};

export function batchArrivalKey(targetId: string, family: Family, method: Method, revision: number) {
  return `${targetId}|${family}|${method}|${revision}`;
}

export function createBatchArrivalTracker(): BatchArrivalTracker {
  return { latestByKey: new Map(), arrivals: new Map(), initialized: false, deliveryUnavailable: false };
}

export function markDeliveryUnavailable(previous: BatchArrivalTracker): BatchArrivalTracker {
  return { ...previous, arrivals: new Map(), deliveryUnavailable: true };
}

export function freshCompletedAt(value: string | null, now: number): number | undefined {
  if (!value) return undefined;
  const at = Date.parse(value);
  if (!Number.isFinite(at) || at > now || now - at > MAX_AGE_MS) return undefined;
  return at;
}

function canAnimate(target: Target, status: string) {
  return target.enabled && !['na', 'unsupported', 'unverified', 'disabled', 'unavailable', 'suspended'].includes(status);
}

export function acceptLocalSnapshot(
  previous: BatchArrivalTracker,
  targets: Target[],
  now: number,
  motionAllowed = true,
): BatchArrivalTracker {
  const keys = new Set(targets.flatMap(target => target.observations.map(o => batchArrivalKey(target.id, o.family, o.method, target.revision))));
  const latestByKey = new Map([...previous.latestByKey].filter(([key]) => keys.has(key)));
  const recovering = !previous.initialized || previous.deliveryUnavailable;
  const arrivals = new Map<string, number>();

  if (!recovering && motionAllowed) {
    for (const [key, acceptedAt] of previous.arrivals) {
      if (acceptedAt <= now && now - acceptedAt < ARRIVAL_WINDOW_MS) arrivals.set(key, acceptedAt);
    }
  }

  for (const target of targets) {
    for (const observation of target.observations) {
      const key = batchArrivalKey(target.id, observation.family, observation.method, target.revision);
      const completedAt = freshCompletedAt(observation.observed_at, now);
      if (completedAt === undefined) continue;

      const prior = latestByKey.get(key);
      if (!recovering && motionAllowed && canAnimate(target, observation.status) && prior !== undefined && completedAt > prior) {
        arrivals.set(key, now);
      }
      // Keep the newest valid source timestamp. An old or invalid local snapshot
      // cannot move the baseline backward or make a later replay look new.
      if (prior === undefined || completedAt > prior) latestByKey.set(key, completedAt);
    }
  }

  return { latestByKey, arrivals, initialized: true, deliveryUnavailable: false };
}
