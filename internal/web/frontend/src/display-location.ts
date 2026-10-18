import type { Target } from './types';

type DisplayLocation = { id: string; label: string; coordinates: [number, number] };
type Alias = { id: string; label: string; coordinates: [number, number] };
// These are display indicators, not endpoint positions. Source coordinates and
// provenance stay on each Target; collection and per-provider identities do not change.
const aliases: Record<string, Alias> = {};
function add(country: string, names: string[], label: string, coordinates: [number, number]) {
  for (const name of names) aliases[`${country}:${normalize(name)}`] = { id: `${country}:${label}`, label, coordinates };
}
function normalize(region: string) {
  return region.normalize('NFKD').replace(/[\u0300-\u036f]/g, '').toLowerCase().replace(/[.,]/g, '').replace(/\s+/g, ' ').trim();
}
// Stable indicators use previously admitted display anchors; grouping does not
// depend on coordinate precision, current filters, provider or measurement family.
add('SG', ['Singapore', 'SIN'], 'SIN', [103.82, 1.35]);
add('AU', ['Sydney', 'SYD'], 'SYD', [151.2, -33.87]);
add('JP', ['Tokyo', 'TYO'], 'TYO', [139.69, 35.68]);
add('IN', ['Mumbai', 'BOM'], 'BOM', [72.88, 19.08]);
add('US', ['Texas', 'Dallas, TX', 'TX'], 'TX', [-99.9, 31.97]);
add('GB', ['United Kingdom', 'London', 'GB', 'LON'], 'GB', [-2, 54]);
add('DE', ['Frankfurt', 'Berlin', 'Germany', 'GER'], 'GER', [8.68, 50.11]);
add('BR', ['Sao Paulo', 'SAO'], 'SAO', [-46.6, -23.5]);

const labels: Record<string, string> = { 'N. California': 'N.CAL', 'N. Virginia': 'VA', 'Cape Town': 'CPT', Bahrain: 'BAH' };
export function displayLocation(target: Target): DisplayLocation | undefined {
  // Do not create a point for targets whose map admission remains pending.
  if (!target.geography.coordinates) return undefined;
  const id = target.country_code ? `${target.country_code}:${normalize(target.region)}` : `point:${target.geography.coordinates.join(',')}`;
  const alias = aliases[id];
  if (alias) return alias;
  return { id, label: labels[target.region] ?? target.region, coordinates: target.geography.coordinates };
}
