import type { Window } from './types';
export const historyWindows: Record<Window, { label: string; bucketSeconds: number; count: number; durationMS: number; long: boolean }> = {
  '1h': { label: '1 hour', bucketSeconds: 60, count: 60, durationMS: 3600000, long: false },
  '24h': { label: '24 hours', bucketSeconds: 300, count: 288, durationMS: 86400000, long: false },
  '7d': { label: '7 days', bucketSeconds: 1800, count: 336, durationMS: 604800000, long: true },
  '30d': { label: '30 days', bucketSeconds: 7200, count: 360, durationMS: 2592000000, long: true },
};
export const windows = Object.keys(historyWindows) as Window[];
export function historyDate(at?: string) { return at && Number.isFinite(Date.parse(at)) ? new Date(at).toLocaleString() : 'unavailable'; }
