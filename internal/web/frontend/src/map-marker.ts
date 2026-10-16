export type MarkerElement = {
  classList: Pick<DOMTokenList, 'add' | 'remove' | 'toggle'>;
  title: string;
  setAttribute(name: string, value: string): void;
};

const statusClasses = ['ok', 'degraded', 'na', 'timeout', 'unreachable', 'dns_error', 'permission_denied', 'unsupported', 'error', 'stale', 'unavailable', 'unverified', 'disabled', 'suspended'];

export function updateMarkerAppearance(element: MarkerElement, selected: boolean, status: string, label: string) {
  // Marker.addTo owns layout/anchor classes and its geographic transform.
  // Replacing className moves the button into normal flow and offsets that transform.
  element.classList.remove(...statusClasses);
  element.classList.add('map-pin', status);
  element.classList.toggle('selected', selected);
  element.title = label;
  element.setAttribute('aria-label', label);
  element.setAttribute('aria-pressed', String(selected));
}
