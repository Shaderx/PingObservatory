import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties } from 'react';
import * as maplibregl from 'maplibre-gl';
import type { Family, Observation, Target } from './types';
import { ageSeconds, elevatedTiming, isRTTMethod, lossValue, metric, reading, rollingStatus, statusOf, statusLabel } from './model';
import { ARRIVAL_WINDOW_MS, freshCompletedAt } from './batch-arrivals';
import { probeActivity } from './probe-model';
import { retainCurrentTargetGroups } from './probe-layout';
import { updateMarkerAppearance } from './map-marker';

type BodyBounds = { left: number; top: number; width: number; height: number };
type Position = { left: number; top: number };
type PointGroup = { id: string; ids: string[]; coordinates: [number, number]; x: number; y: number; left: number; top: number; offsetX: number; offsetY: number; width: number; height: number; body: BodyBounds; visible: boolean };
type Layout = { width: number; height: number; narrow: boolean; groups: PointGroup[] };
type LabelSize = { width: number; height: number; body: BodyBounds };
const MIN_LEADER_CLEARANCE = 24;
const compact = (value: number | null | undefined) => value == null ? '—' : String(Number(value.toFixed(1)));
function clampPosition(position: Position, labelWidth: number, labelHeight: number, width: number, height: number): Position {
  return { left: Math.max(14, Math.min(width - labelWidth - 14, position.left)), top: Math.max(10, Math.min(height - labelHeight - 22, position.top)) };
}
function bodyClearance(x: number, y: number, position: Position, body: BodyBounds) {
  const left = position.left + body.left, top = position.top + body.top, right = left + body.width, bottom = top + body.height;
  const dx = Math.max(left - x, 0, x - right), dy = Math.max(top - y, 0, y - bottom);
  return Math.hypot(dx, dy);
}
function separateLeader(x: number, y: number, desired: Position, labelWidth: number, labelHeight: number, body: BodyBounds, width: number, height: number, previous?: Position) {
  const bounded = clampPosition(desired, labelWidth, labelHeight, width, height);
  const clearance = (position: Position) => bodyClearance(x, y, position, body);
  const desiredClearance = clearance(bounded);
  if (desiredClearance >= MIN_LEADER_CLEARANCE) return { ...bounded, clearance: desiredClearance, separated: true };

  const bodyRight = body.left + body.width, bodyBottom = body.top + body.height;
  const minLeft = 14, maxLeft = Math.max(minLeft, width - labelWidth - 14);
  const minTop = 10, maxTop = Math.max(minTop, height - labelHeight - 22);
  const candidates = [
    { left: x + MIN_LEADER_CLEARANCE - body.left, top: bounded.top },
    { left: x - MIN_LEADER_CLEARANCE - bodyRight, top: bounded.top },
    { left: bounded.left, top: y + MIN_LEADER_CLEARANCE - body.top },
    { left: bounded.left, top: y - MIN_LEADER_CLEARANCE - bodyBottom },
    { left: minLeft, top: minTop }, { left: minLeft, top: maxTop },
    { left: maxLeft, top: minTop }, { left: maxLeft, top: maxTop },
  ].map(position => clampPosition(position, labelWidth, labelHeight, width, height));
  if (previous) candidates.push(clampPosition(previous, labelWidth, labelHeight, width, height));
  const unique = candidates.filter((candidate, index) => candidates.findIndex(other => other.left === candidate.left && other.top === candidate.top) === index);
  const previousPosition = previous && clampPosition(previous, labelWidth, labelHeight, width, height);
  const distanceFromDesired = (position: Position) => (position.left - bounded.left) ** 2 + (position.top - bounded.top) ** 2;
  const isPrevious = (position: Position) => !!previousPosition && position.left === previousPosition.left && position.top === previousPosition.top;
  const compareNearest = (a: Position, b: Position) => {
    const difference = distanceFromDesired(a) - distanceFromDesired(b);
    if (Math.abs(difference) > 1e-6) return difference;
    if (isPrevious(a) !== isPrevious(b)) return isPrevious(a) ? -1 : 1;
    return 0;
  };
  const valid = unique.filter(position => clearance(position) >= MIN_LEADER_CLEARANCE);
  if (valid.length) {
    const position = valid.sort(compareNearest)[0];
    return { ...position, clearance: clearance(position), separated: true };
  }

  // If the viewport cannot fit the requested gap, keep the whole label legal
  // and use the legal candidate with the greatest available clearance.
  const fallback = [bounded, ...unique].sort((a, b) => {
    const difference = clearance(b) - clearance(a);
    return Math.abs(difference) > 1e-6 ? difference : compareNearest(a, b);
  })[0];
  return { ...fallback, clearance: clearance(fallback), separated: false };
}
const methodCode = { icmp: 'I', udp_echo: 'U', tcp_connect: 'T', https: 'H', dns: 'D' };
function shortProvider(provider: string) { return provider === 'Epic Games' ? 'EP' : provider === 'Amazon Web Services' ? 'AWS' : provider.slice(0, 4).toUpperCase(); }
function conditionCode(o: Observation | undefined, now: number) {
  const codes: Record<string, string> = { na: 'N/A', stale: '+', unverified: 'UV', disabled: 'OFF', timeout: 'NR', unreachable: 'NR', dns_error: 'DNS', permission_denied: 'DEN', unsupported: 'NS', error: 'ERR', unavailable: 'UTM', suspended: 'N/A' };
  return codes[statusOf(o, now)] ?? (elevatedTiming(o, now) ? '↑' : statusOf(o, now) === 'degraded' ? '!' : '');
}
function tone(o: Observation | undefined, now: number, light: boolean) {
  const status = statusOf(o, now);
  if (o && ['timeout', 'unreachable', 'error'].includes(status) && (isRTTMethod(o.method) ? o.sent > 0 : o.attempted > 0)) return light ? '#aa4935' : '#ed9784';
  if (!o || ['na', 'stale', 'unverified', 'unsupported', 'disabled', 'unavailable'].includes(status) || o.median_ms === null || !o.baseline_ms || o.baseline_ms <= 0 || rollingStatus(o, now) !== 'fresh' || ageSeconds(o.baseline_as_of, now) > 180 || o.baseline_sample_count < 5) return light ? '#697665' : '#9ca897';
  const rise = Math.max(0, Math.min(1, o.median_ms / o.baseline_ms - 1));
  return `hsl(${90 - rise * 80} ${light ? 62 : 76}% ${light ? 31 : 69}%)`;
}
function arrange(map: maplibregl.Map, targets: Target[], sizes: Map<string, LabelSize>, previousPositions = new Map<string, Position>()): Layout {
  const width = map.getContainer().clientWidth, height = map.getContainer().clientHeight, narrow = width < 800;
  const groups = new Map<string, PointGroup>();
  for (const t of [...targets].sort((a, b) => a.id.localeCompare(b.id))) {
    if (!t.geography.coordinates) continue;
    const id = t.geography.coordinates.join(',');
    if (!groups.has(id)) {
      const point = map.project(t.geography.coordinates);
      const size = sizes.get(id), labelWidth = size?.width ?? 177;
      groups.set(id, { id, ids: [], coordinates: t.geography.coordinates, x: point.x, y: point.y, left: 0, top: 0, offsetX: 0, offsetY: 0, width: labelWidth, height: 0, body: size?.body ?? { left: 0, top: 15, width: labelWidth, height: 0 }, visible: narrow || point.x >= 0 && point.x <= width && point.y >= 0 && point.y <= height });
    }
    groups.get(id)!.ids.push(t.id);
  }
  const items = [...groups.values()];
  for (const item of items) {
    item.height = sizes.get(item.id)?.height ?? 15 + item.ids.length * 21;
    if (!sizes.has(item.id)) item.body.height = Math.max(0, item.height - item.body.top);
  }
  const placed: PointGroup[] = [];
  for (const item of [...items].filter(g => g.visible).sort((a, b) => b.ids.length - a.ids.length || a.x - b.x)) {
    let best = { x: 0, y: 0, score: Infinity };
    // Above, below, left and right have equal distance cost. Layout does not
    // assume a preferred direction or attach leaders to a fixed corner.
    const xs = [item.x + 14, item.x - item.width - 14, item.x - item.width / 2, ...Array.from({ length: Math.max(1, Math.floor(width / (item.width + 12))) }, (_, i) => 20 + i * (item.width + 12))];
    const ys = [item.y - item.height / 2, item.y - item.height - 14, item.y + 14, ...Array.from({ length: Math.max(1, Math.floor((height - item.height - 30) / 12)) }, (_, i) => 12 + i * 12)];
    for (const rawX of xs) for (const rawY of ys) {
      const position = separateLeader(item.x, item.y, { left: rawX, top: rawY }, item.width, item.height, item.body, width, height, previousPositions.get(item.id));
      const x = position.left, y = position.top;
      const overlap = placed.reduce((sum, p) => sum + Math.max(0, Math.min(x + item.width + 10, p.left + p.width + 10) - Math.max(x, p.left)) * Math.max(0, Math.min(y + item.height + 12, p.top + p.height + 12) - Math.max(y, p.top)), 0);
      const control = x < 55 && y < 95 ? 1e7 : 0;
      const coveringDot = items.some(p => p.x > x - 5 && p.x < x + item.width + 5 && p.y > y - 5 && p.y < y + item.height + 5) ? 30000 : 0;
      const nearestX = Math.max(x, Math.min(x + item.width, item.x)), nearestY = Math.max(y, Math.min(y + item.height, item.y));
      const score = overlap * 1e5 + control + coveringDot + Math.pow(nearestX - item.x, 2) + Math.pow(nearestY - item.y, 2);
      if (score < best.score) best = { x, y, score };
    }
    item.left = best.x; item.top = best.y; item.offsetX = best.x - item.x; item.offsetY = best.y - item.y; placed.push(item);
  }
  // Keep an off-screen placement ready. The label remains hidden until its
  // anchor enters the viewport during a pan.
  for (const item of items.filter(group => !group.visible)) {
    item.left = item.x < 0 ? item.x + 14 : item.x > width ? item.x - item.width - 14 : item.x + 14;
    item.top = item.y < 0 ? item.y + 14 : item.y > height ? item.y - item.height - 14 : item.y - item.height / 2;
    item.offsetX = item.left - item.x; item.offsetY = item.top - item.y;
  }
  return { width, height, narrow, groups: items };
}

function leader(group: PointGroup) {
  const { x, y, left, top } = group, body = { left: left + group.body.left, top: top + group.body.top, width: group.body.width, height: group.body.height };
  const right = body.left + body.width, bottom = body.top + body.height;
  let ax = Math.max(body.left, Math.min(right, x)), ay = Math.max(body.top, Math.min(bottom, y));
  if (x > body.left && x < right && y > body.top && y < bottom) {
    const edges = [{ distance: x - body.left, x: body.left, y }, { distance: right - x, x: right, y }, { distance: y - body.top, x, y: body.top }, { distance: bottom - y, x, y: bottom }].sort((a, b) => a.distance - b.distance);
    ax = edges[0].x; ay = edges[0].y;
  }
  const verticalEdge = ax === body.left || ax === right;
  const bx = verticalEdge ? ax + Math.sign(x - ax) * Math.min(8, Math.abs(x - ax)) : ax;
  const by = verticalEdge ? ay : ay + Math.sign(y - ay) * Math.min(8, Math.abs(y - ay));
  return `M${x},${y} L${bx},${by} L${ax},${ay}`;
}

export function ProbeAtlas({ map, targets, selected, onSelect, family, now, arrivals, motionEnabled, theme }: { map: maplibregl.Map; targets: Target[]; selected: string; onSelect(id: string): void; family: Family; now: number; arrivals: ReadonlyMap<string, number>; motionEnabled: boolean; theme: string }) {
  const [layout, setLayout] = useState<Layout>({ width: 0, height: 0, narrow: false, groups: [] });
  const [reduced, setReduced] = useState(() => matchMedia('(prefers-reduced-motion: reduce)').matches);
  const markers = useRef(new Map<string, maplibregl.Marker>());
  const labelElements = useRef(new Map<string, HTMLDivElement>()), telemetryElements = useRef(new Map<string, HTMLDivElement>()), labelSizes = useRef(new Map<string, LabelSize>());
  const pathElements = useRef(new Map<string, SVGPathElement>());
  const resizeObserver = useRef<ResizeObserver | null>(null), movingRef = useRef(false);
  const seenArrivals = useRef(new Map<string, number>()), arrivalTimers = useRef(new Map<string, number>());
  const activeAnimations = useRef(new Map<string, { family: Family; method: string; expiresAt: number }>());
  const currentFamily = useRef(family);
  const layoutRef = useRef(layout); layoutRef.current = layout;
  const selectRef = useRef(onSelect); selectRef.current = onSelect;
  const targetsRef = useRef(targets); targetsRef.current = targets;
  const geometry = JSON.stringify([...targets].sort((a, b) => a.id.localeCompare(b.id)).map(t => [t.id, t.geography.coordinates]));
  const measureLabelSize = (id: string): LabelSize | undefined => {
    const label = labelElements.current.get(id), telemetry = telemetryElements.current.get(id);
    if (!label || !telemetry) return undefined;
    const labelRect = label.getBoundingClientRect(), bodyRect = telemetry.getBoundingClientRect();
    return {
      width: Math.ceil(labelRect.width), height: Math.ceil(labelRect.height),
      body: { left: bodyRect.left - labelRect.left, top: bodyRect.top - labelRect.top, width: bodyRect.width, height: bodyRect.height },
    };
  };
  const sameLabelSize = (a: LabelSize | undefined, b: LabelSize) => !!a && a.width === b.width && a.height === b.height && a.body.left === b.body.left && a.body.top === b.body.top && a.body.width === b.body.width && a.body.height === b.body.height;
  const previousPositions = () => new Map(layoutRef.current.groups.map(group => [group.id, { left: group.left, top: group.top }]));
  const applyGroupPosition = (group: PointGroup, element = labelElements.current.get(group.id)) => {
    if (!element) return;
    element.style.transform = `translate3d(${group.left}px, ${group.top}px, 0)`;
    element.style.visibility = group.visible ? 'visible' : 'hidden';
    element.setAttribute('aria-hidden', String(!group.visible));
  };
  const syncPositions = () => {
    const current = layoutRef.current;
    if (!current.groups.length || current.narrow) return;
    for (const group of current.groups) {
      const point = map.project(group.coordinates);
      group.x = point.x; group.y = point.y;
      const position = separateLeader(point.x, point.y, { left: point.x + group.offsetX, top: point.y + group.offsetY }, group.width, group.height, group.body, current.width, current.height, { left: group.left, top: group.top });
      group.left = position.left; group.top = position.top;
      group.visible = point.x >= 0 && point.x <= current.width && point.y >= 0 && point.y <= current.height;
      applyGroupPosition(group);
      const path = pathElements.current.get(group.id);
      if (path) { path.setAttribute('d', leader(group)); path.style.visibility = group.visible ? 'visible' : 'hidden'; }
    }
  };
  const attachLabel = (id: string, element: HTMLDivElement | null) => {
    const previous = labelElements.current.get(id);
    if (!element) {
      if (previous) resizeObserver.current?.unobserve(previous);
      labelElements.current.delete(id);
      return;
    }
    labelElements.current.set(id, element); element.dataset.groupId = id;
    resizeObserver.current?.observe(element);
    const group = layoutRef.current.groups.find(item => item.id === id);
    if (!group || layoutRef.current.narrow) {
      element.style.transform = ''; element.style.visibility = 'visible'; element.setAttribute('aria-hidden', 'false');
    } else applyGroupPosition(group, element);
  };
  const attachTelemetry = (id: string, element: HTMLDivElement | null) => {
    const previous = telemetryElements.current.get(id);
    if (!element) {
      if (previous) resizeObserver.current?.unobserve(previous);
      telemetryElements.current.delete(id);
      return;
    }
    telemetryElements.current.set(id, element); element.dataset.groupId = id;
    resizeObserver.current?.observe(element);
  };
  const attachPath = (id: string, element: SVGPathElement | null) => {
    if (!element) { pathElements.current.delete(id); return; }
    pathElements.current.set(id, element);
    const group = layoutRef.current.groups.find(item => item.id === id);
    if (group) { element.setAttribute('d', leader(group)); element.style.visibility = group.visible ? 'visible' : 'hidden'; }
  };
  useEffect(() => { const query = matchMedia('(prefers-reduced-motion: reduce)'); const update = () => setReduced(query.matches); query.addEventListener('change', update); return () => query.removeEventListener('change', update); }, []);
  useLayoutEffect(() => {
    const observer = new ResizeObserver(entries => {
      const changedIds = new Set<string>();
      for (const entry of entries) {
        const id = (entry.target as HTMLElement).dataset.groupId;
        if (!id) continue;
        changedIds.add(id);
      }
      const changed = new Set<string>();
      for (const id of changedIds) {
        const size = measureLabelSize(id), old = labelSizes.current.get(id);
        if (size && !sameLabelSize(old, size)) { labelSizes.current.set(id, size); changed.add(id); }
      }
      if (!changed.size) return;
      if (movingRef.current) {
        for (const id of changed) {
          const size = labelSizes.current.get(id), group = layoutRef.current.groups.find(item => item.id === id);
          if (size && group) { group.width = size.width; group.height = size.height; group.body = size.body; }
        }
        syncPositions();
        return;
      }
      const next = arrange(map, targetsRef.current, labelSizes.current, previousPositions());
      setLayout(next);
    });
    resizeObserver.current = observer;
    for (const [id, element] of labelElements.current) {
      observer.observe(element);
      const telemetry = telemetryElements.current.get(id);
      if (telemetry) observer.observe(telemetry);
      const size = measureLabelSize(id);
      if (size) labelSizes.current.set(id, size);
    }
    const arrangeAtRest = () => setLayout(arrange(map, targetsRef.current, labelSizes.current, previousPositions()));
    const start = () => {
      if (!map.isMoving() || movingRef.current) return; // Resize can emit a movement cycle during an active gesture.
      for (const marker of markers.current.values()) marker.getElement().classList.remove('is-arrival', 'animate');
      for (const timer of arrivalTimers.current.values()) clearTimeout(timer);
      arrivalTimers.current.clear(); activeAnimations.current.clear();
      movingRef.current = true;
    };
    const end = () => {
      if (!movingRef.current || map.isMoving()) return;
      movingRef.current = false;
      const next = arrange(map, targetsRef.current, labelSizes.current, previousPositions());
      setLayout(next);
    };
    const resize = () => {
      if (!movingRef.current) { arrangeAtRest(); return; }
      const current = layoutRef.current, container = map.getContainer();
      current.width = container.clientWidth; current.height = container.clientHeight;
      const narrow = current.width < 800;
      if (narrow !== current.narrow) setLayout({ ...current, narrow });
      syncPositions();
    };
    map.on('render', syncPositions); map.on('resize', resize); map.on('movestart', start); map.on('moveend', end);
    arrangeAtRest();
    return () => {
      observer.disconnect(); resizeObserver.current = null;
      map.off('render', syncPositions); map.off('resize', resize); map.off('movestart', start); map.off('moveend', end);
    };
  }, [map, geometry]);
  useLayoutEffect(() => {
    if (layout.narrow) for (const element of labelElements.current.values()) {
      element.style.transform = ''; element.style.visibility = 'visible'; element.setAttribute('aria-hidden', 'false');
    }
    syncPositions();
  }, [layout.groups, layout.narrow]);
  useEffect(() => {
    const wanted = new Set(targetsRef.current.filter(t => t.geography.coordinates).map(t => t.id));
    for (const [id, marker] of markers.current) if (!wanted.has(id)) {
      marker.remove(); markers.current.delete(id);
      const timer = arrivalTimers.current.get(id); if (timer) clearTimeout(timer);
      arrivalTimers.current.delete(id); activeAnimations.current.delete(id);
    }
    for (const t of targetsRef.current) {
      if (!t.geography.coordinates) continue;
      if (!markers.current.has(t.id)) {
        const button = document.createElement('button'); button.type = 'button'; button.classList.add('probe-geodot');
        button.innerHTML = '<span class="probe-aura"></span><span class="probe-loss-ring"></span><span class="probe-core"></span><span class="probe-ripple"></span>';
        button.addEventListener('click', () => selectRef.current(t.id));
        markers.current.set(t.id, new maplibregl.Marker({ element: button }).setLngLat(t.geography.coordinates).addTo(map));
      } else markers.current.get(t.id)!.setLngLat(t.geography.coordinates);
    }
  }, [map, geometry]);
  useEffect(() => () => {
    for (const timer of arrivalTimers.current.values()) clearTimeout(timer);
    arrivalTimers.current.clear();
    for (const marker of markers.current.values()) marker.remove();
    markers.current.clear();
  }, []);
  const targetsById = new Map(targets.map(target => [target.id, target]));
  const renderGroups = retainCurrentTargetGroups(layout.groups, new Set(targetsById.keys()));
  const visible = new Set(renderGroups.filter(g => g.visible).flatMap(g => g.ids));
  useEffect(() => {
    for (const t of targets) {
      const marker = markers.current.get(t.id); if (!marker) continue;
      const el = marker.getElement(), o = reading(t, family), status = statusOf(o, now);
      el.style.setProperty('--probe-tone', tone(o, now, theme === 'light'));
      el.classList.toggle('has-fresh-loss', (lossValue(o) ?? 0) > 0 && rollingStatus(o, now) === 'fresh');
      updateMarkerAppearance(el, selected === t.id, status, `${t.label} / ${statusLabel(status)} / approximate location`);
    }
  }, [targets, selected, family, now, theme]);
  useEffect(() => {
    const candidates: Target[] = [];
    const familyChanged = currentFamily.current !== family;
    currentFamily.current = family;
    if (familyChanged) {
      for (const marker of markers.current.values()) marker.getElement().classList.remove('is-arrival', 'animate');
      for (const timer of arrivalTimers.current.values()) clearTimeout(timer);
      arrivalTimers.current.clear(); activeAnimations.current.clear();
    }
    for (const [key, acceptedAt] of arrivals) {
      if (seenArrivals.current.get(key) === acceptedAt) continue;
      seenArrivals.current.set(key, acceptedAt);
      const [targetId, eventFamily, eventMethod] = key.split('|');
      const age = Date.now() - acceptedAt;
      if (familyChanged || eventFamily !== family || age < 0 || age >= ARRIVAL_WINDOW_MS) continue;
      const target = targetsRef.current.find(item => item.id === targetId);
      if (!target || !visible.has(targetId) || !target.geography.coordinates) continue;
      const observation = reading(target, family);
      if (!observation || observation.method !== eventMethod || freshCompletedAt(observation.observed_at, now) === undefined || probeActivity(target, observation, now) !== 'unknown' || !markers.current.has(targetId)) continue;
      candidates.push(target);
    }
    const animate = motionEnabled && !reduced && !movingRef.current && !document.hidden;
    const active = new Map<string, Target>();
    if (animate && !familyChanged) for (const [id, timing] of activeAnimations.current) {
      const target = targetsRef.current.find(item => item.id === id), observation = target && reading(target, family);
      if (!target || timing.expiresAt <= Date.now() || timing.family !== family || !visible.has(id) || observation?.method !== timing.method || freshCompletedAt(observation?.observed_at ?? null, now) === undefined || probeActivity(target, observation, now) !== 'unknown') {
        const timer = arrivalTimers.current.get(id); if (timer) clearTimeout(timer);
        arrivalTimers.current.delete(id); activeAnimations.current.delete(id);
        markers.current.get(id)?.getElement().classList.remove('is-arrival', 'animate');
      } else active.set(id, target);
    }
    const selectedCandidates = animate && !familyChanged ? [...new Map([...active, ...candidates.map(target => [target.id, target] as const)]).values()]
      .sort((a, b) => Number(b.id === selected) - Number(a.id === selected) || a.id.localeCompare(b.id))
      .slice(0, layout.narrow ? 2 : 4) : [];
    const retained = new Set(selectedCandidates.map(target => target.id));
    for (const id of activeAnimations.current.keys()) if (!retained.has(id)) {
      const timer = arrivalTimers.current.get(id); if (timer) clearTimeout(timer);
      arrivalTimers.current.delete(id); activeAnimations.current.delete(id);
      markers.current.get(id)?.getElement().classList.remove('is-arrival', 'animate');
    }
    for (const target of selectedCandidates) {
      const element = markers.current.get(target.id)?.getElement();
      if (!element) continue;
      if (active.has(target.id)) continue;
      const oldTimer = arrivalTimers.current.get(target.id);
      if (oldTimer) clearTimeout(oldTimer);
      element.classList.add('is-arrival', 'animate');
      activeAnimations.current.set(target.id, { family, method: reading(target, family)!.method, expiresAt: Date.now() + ARRIVAL_WINDOW_MS });
      arrivalTimers.current.set(target.id, window.setTimeout(() => {
        element.classList.remove('is-arrival', 'animate');
        arrivalTimers.current.delete(target.id); activeAnimations.current.delete(target.id);
      }, ARRIVAL_WINDOW_MS));
    }
  }, [arrivals, family, layout.groups, layout.narrow, motionEnabled, now, reduced, selected, targets]);
  useEffect(() => {
    const clearMotion = () => {
      if (!document.hidden) return;
      for (const [key, acceptedAt] of arrivals) seenArrivals.current.set(key, acceptedAt);
      for (const marker of markers.current.values()) marker.getElement().classList.remove('is-arrival', 'animate');
      for (const timer of arrivalTimers.current.values()) clearTimeout(timer);
      arrivalTimers.current.clear(); activeAnimations.current.clear();
    };
    document.addEventListener('visibilitychange', clearMotion);
    return () => document.removeEventListener('visibilitychange', clearMotion);
  }, [arrivals]);
  useEffect(() => {
    if (motionEnabled && !reduced) return;
    for (const marker of markers.current.values()) marker.getElement().classList.remove('is-arrival', 'animate');
    for (const timer of arrivalTimers.current.values()) clearTimeout(timer);
    arrivalTimers.current.clear(); activeAnimations.current.clear();
  }, [motionEnabled, reduced]);
  return <div className={`probe-atlas ${layout.narrow ? 'narrow' : ''}`} aria-label="Approximate endpoint locations and current measurements on the map">
    {!layout.narrow && <svg className="probe-leaders" width={layout.width} height={layout.height} aria-hidden="true">{renderGroups.map(g => <path key={g.id} ref={element => attachPath(g.id, element)} d={leader(g)} />)}</svg>}
    {renderGroups.map(g => {
      const first = targetsById.get(g.ids[0])!;
      return <div className="probe-callout" key={g.id} ref={element => attachLabel(g.id, element)}>
        <div className="probe-location" title={first.region}>{first.region}</div>
        <div className="probe-telemetry" ref={element => attachTelemetry(g.id, element)}>
          {g.ids.map(id => {
            const t = targetsById.get(id)!, o = reading(t, family), status = statusOf(o, now), roll = rollingStatus(o, now), value = lossValue(o), color = tone(o, now, theme === 'light');
            return <button key={id} type="button" className={`probe-row ${id === selected ? 'selected' : ''} ${status}`} style={{ '--probe-tone': color } as CSSProperties} onClick={() => onSelect(id)} aria-pressed={id === selected} aria-label={`${t.region} ${t.provider}. ${o?.method ?? 'Method unavailable'} ${metric(o?.median_ms)} milliseconds. One-hour ${isRTTMethod(o?.method) ? 'packet loss' : 'failed-attempt rate'} ${metric(value)} percent. ${statusLabel(status)}.`}>
              <span className="probe-reading"><span className="probe-provider">{shortProvider(t.provider)}</span><span className="probe-method">{o ? methodCode[o.method] : '-'}</span><strong>{compact(o?.median_ms)}{o?.median_ms != null && <small>ms</small>}<span className="probe-condition">{conditionCode(o, now)}</span></strong><span className={`probe-loss ${(value ?? 0) > 0 ? 'has-loss' : ''} ${roll}`}>{isRTTMethod(o?.method) ? 'L' : 'F'}{compact(value)}{value != null ? '%' : ''}{roll === 'stale' ? '+' : ''}</span><span className="probe-activity" title="Only completed results are shown; in-flight target activity is unavailable">{probeActivity(t, o, now) === 'unknown' ? '?' : '-'}</span></span>
            </button>;
          })}
        </div>
      </div>;
    })}
  </div>;
}