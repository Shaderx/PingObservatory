import { useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type CSSProperties } from 'react';
import * as maplibregl from 'maplibre-gl';
import type { Family, Observation, Target } from './types';
import { ageSeconds, reading, statusOf, statusLabel } from './model';
import { ARRIVAL_WINDOW_MS, batchArrivalKey, freshCompletedAt } from './batch-arrivals';
import { probeActivity } from './probe-model';
import { retainCurrentTargetGroups } from './probe-layout';
import { updateMarkerAppearance } from './map-marker';
import { compactProvider } from './compact-display';
import { mapLabelState } from './map-label-state';
import { ReadingTooltip } from './ReadingTooltip';
import { displayLocation } from './display-location';
import { distanceColor } from './distance-color';
import { buildArrivalGeometry, playArrivalSignal, type ArrivalGeometry, type SignalHandle } from './arrival-signal';
import { ArrivalLayoutGate, sameArrivalBounds, type ArrivalBounds } from './arrival-layout';
import { MapMovementGate } from './map-movement';
import { DemandPacer } from './demand-pacing';
import { needsReadingList, type CapacityPolicy } from './callout-capacity';

type BodyBounds = { left: number; top: number; width: number; height: number };
type Position = { left: number; top: number };
type PointGroup = { id: string; label: string; ids: string[]; coordinates: [number, number]; x: number; y: number; left: number; top: number; offsetX: number; offsetY: number; width: number; height: number; body: BodyBounds; visible: boolean };
type Layout = { width: number; height: number; narrow: boolean; docked: boolean; groups: PointGroup[] };
type LabelSize = { width: number; height: number; body: BodyBounds };
const MIN_LEADER_CLEARANCE = 24;
const ARRIVAL_NODE_MS = 900;
const ARRIVAL_MAX_QUEUE_AGE_MS = 15000;
type Arrival = { family: Family; method: string; completedAt: number; acceptedAt: number; acceptedMono: number; revision: number };
const shortMethod = { icmp: 'I', udp_echo: 'U', tcp_connect: 'TCP', https: 'HTTPS', dns: 'DNS' };
const compact = (value: number | null | undefined) => value == null || !Number.isFinite(value) ? '\u2014' : String(Number(value.toFixed(1)));
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
function arrange(map: maplibregl.Map, targets: Target[], sizes: Map<string, LabelSize>, previousPositions = new Map<string, Position>(), policy: CapacityPolicy = { key: '', list: false }): Layout {
  const container = map.getContainer(), frame = container.closest<HTMLElement>('.atlas-frame') ?? container;
  const width = container.clientWidth, height = container.clientHeight, physicalNarrow = frame.clientWidth < 800;
  let narrow = physicalNarrow;
  const groups = new Map<string, PointGroup>();
  for (const t of [...targets].sort((a, b) => a.id.localeCompare(b.id))) {
    const location = displayLocation(t);
    if (!location) continue;
    const { id, label, coordinates } = location;
    if (!groups.has(id)) {
      const point = map.project(coordinates);
      const size = sizes.get(id), labelWidth = size?.width ?? 100;
      groups.set(id, { id, label, ids: [], coordinates, x: point.x, y: point.y, left: 0, top: 0, offsetX: 0, offsetY: 0, width: labelWidth, height: 0, body: size?.body ?? { left: 0, top: 14, width: labelWidth, height: 0 }, visible: narrow || point.x >= 0 && point.x <= width && point.y >= 0 && point.y <= height });
    }
    groups.get(id)!.ids.push(t.id);
  }
  const items = [...groups.values()];
  for (const item of items) {
    item.height = sizes.get(item.id)?.height ?? 14 + item.ids.length * 16;
    if (!sizes.has(item.id)) item.body.height = Math.max(0, item.height - item.body.top);
  }
  const capacityKey = JSON.stringify([frame.clientWidth, frame.clientHeight, items.map(g => [g.id, g.ids, g.coordinates])]);
  if (policy.key !== capacityKey) { policy.key = capacityKey; policy.list = false; }
  if (physicalNarrow || policy.list) return { width, height, narrow: true, docked: !physicalNarrow, groups: items.map(g => ({ ...g, visible: true })) };
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
  if (items.every(g => sizes.has(g.id))) {
    const bounds = container.getBoundingClientRect();
    const controls = [...container.querySelectorAll<HTMLElement>('.maplibregl-ctrl')].map(el => { const r = el.getBoundingClientRect(); return { left: r.left - bounds.left, top: r.top - bounds.top, width: r.width, height: r.height }; });
    if (needsReadingList(placed, controls, width, height)) { narrow = true; policy.list = true; for (const item of items) item.visible = true; }
  }
  return { width, height, narrow, docked: narrow && !physicalNarrow, groups: items };
}

function leaderGeometry(group: PointGroup) {
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
  return { d: `M${x},${y} L${bx},${by} L${ax},${ay}`, end: { x: ax, y: ay } };
}
function leader(group: PointGroup) { return leaderGeometry(group).d; }
// Compare against the last accepted baseline; noise at half a CSS pixel does
// not replace that baseline, so fractional jitter cannot walk between bins.
const near = (a: number, b: number) => Math.abs(a - b) <= 0.5;
function sameLayout(a: Layout, b: Layout) {
  if (a.width !== b.width || a.height !== b.height || a.narrow !== b.narrow || a.docked !== b.docked || a.groups.length !== b.groups.length) return false;
  return a.groups.every((left, index) => {
    const right = b.groups[index];
    return left.id === right.id && left.label === right.label && left.visible === right.visible
      && left.ids.length === right.ids.length && left.ids.every((id, idIndex) => id === right.ids[idIndex])
      && left.coordinates[0] === right.coordinates[0] && left.coordinates[1] === right.coordinates[1]
      && near(left.x, right.x) && near(left.y, right.y) && near(left.left, right.left) && near(left.top, right.top)
      && near(left.offsetX, right.offsetX) && near(left.offsetY, right.offsetY)
      && near(left.width, right.width) && near(left.height, right.height)
      && near(left.body.left, right.body.left) && near(left.body.top, right.body.top)
      && near(left.body.width, right.body.width) && near(left.body.height, right.body.height);
  });
}

export function ProbeAtlas({ map, targets, registeredTargetIds, selected, onSelect, family, now, arrivals, motionEnabled, theme, origin, accessAllowanceMS }: { map: maplibregl.Map; targets: Target[]; registeredTargetIds: ReadonlySet<string>; selected: string; onSelect(id: string): void; family: Family; now: number; arrivals: ReadonlyMap<string, number>; motionEnabled: boolean; theme: string; origin?: [number, number]; accessAllowanceMS?: number }) {
  const [layout, setLayout] = useState<Layout>({ width: 0, height: 0, narrow: false, docked: false, groups: [] });
  const [reduced, setReduced] = useState(() => matchMedia('(prefers-reduced-motion: reduce)').matches);
  const [geometryRevision, setGeometryRevision] = useState(0);
  const [helpTarget, setHelpTarget] = useState<{ id: string; anchor: HTMLButtonElement } | null>(null);
  const helpId = `map-reading-${useId().replaceAll(':', '')}`;
  const helpTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const helpDismissed = useRef(false);
  const helpPointer = useRef({ x: NaN, y: NaN });
  const keepHelp = useCallback(() => { clearTimeout(helpTimer.current); }, []);
  const dismissHelp = useCallback(() => { clearTimeout(helpTimer.current); helpDismissed.current = true; setHelpTarget(null); }, []);
  const leaveHelp = useCallback(() => {
    clearTimeout(helpTimer.current);
    helpTimer.current = setTimeout(() => setHelpTarget(current => document.activeElement === current?.anchor ? current : null), 150);
  }, []);
  const showHelp = (id: string, anchor: HTMLButtonElement, hover = false) => {
    if (hover && helpDismissed.current) return;
    helpDismissed.current = false; keepHelp(); setHelpTarget({ id, anchor });
  };
  useEffect(() => {
    const moved = (event: MouseEvent) => {
      if (event.clientX !== helpPointer.current.x || event.clientY !== helpPointer.current.y) helpDismissed.current = false;
      helpPointer.current = { x: event.clientX, y: event.clientY };
    };
    document.addEventListener('mousemove', moved, true);
    return () => document.removeEventListener('mousemove', moved, true);
  }, []);
  useEffect(() => () => clearTimeout(helpTimer.current), []);
  useEffect(dismissHelp, [family, dismissHelp]);
  const geometryRevisionRef = useRef(0);
  const snapshot = useMemo(() => ({}), [targets, arrivals, family, registeredTargetIds, layout, motionEnabled, reduced, geometryRevision]);
  const layoutGate = useRef(new ArrivalLayoutGate<object>());
  const deferredDrain = useRef<number | undefined>(undefined), expiryTimer = useRef<number | undefined>(undefined);
  const expiryDeadline = useRef<number | undefined>(undefined);
  const expireRef = useRef<() => void>(() => {}), requestDrainRef = useRef<() => void>(() => {});
  // Close admission in the commit before any completion promise can observe
  // newer readings. Render-time drain closures also check this snapshot token.
  useLayoutEffect(() => {
    if (layoutGate.current.isCurrent(snapshot)) return;
    layoutGate.current.holdSnapshot(snapshot);
    clearDeferredDrain();
  }, [snapshot]);
  const capacityPolicy = useRef<CapacityPolicy>({ key: '', list: false });
  // Only actual launches advance a location's fair service cursor. Expired or
  // canceled queued work never gains a new age or replays on a later snapshot.
  const locationServiceCursor = useRef(new Map<string, string>());
  const listContainer = useRef<HTMLDivElement>(null);
  const markers = useRef(new Map<string, maplibregl.Marker>());
  const labelElements = useRef(new Map<string, HTMLDivElement>()), telemetryElements = useRef(new Map<string, HTMLDivElement>()), labelSizes = useRef(new Map<string, LabelSize>());
  const labelRefCallbacks = useRef(new Map<string, (element: HTMLDivElement | null) => void>()), telemetryRefCallbacks = useRef(new Map<string, (element: HTMLDivElement | null) => void>());
  const pathElements = useRef(new Map<string, SVGPathElement>());
  const pathRefCallbacks = useRef(new Map<string, (element: SVGPathElement | null) => void>()), rowRefCallbacks = useRef(new Map<string, (element: HTMLButtonElement | null) => void>());
  const signalSVG = useRef<SVGSVGElement | null>(null);
  const signalGeometry = useRef(new Map<string, ArrivalGeometry>()), signalHandles = useRef(new Map<string, SignalHandle>());
  const activeSignalGroups = useRef(new Map<string, string>());
  const geometryBounds = useRef(new Map<string, ArrivalBounds>()), activeBounds = useRef(new Map<string, ArrivalBounds>());
  const resizeObserver = useRef<ResizeObserver | null>(null), movingRef = useRef(false);
  const movementGate = useRef(new MapMovementGate());
  const seenArrivals = useRef(new Map<string, number>()), arrivalTimers = useRef(new Map<string, number>());
  const arrivalMarkers = useRef(new Map<string, maplibregl.Marker>());
  // Active overlap follows demand and measured browser pressure; each location has one owner.
  const activeAnimations = useRef(new Map<string, Arrival>());
  const labelSlots = useRef(new Set<string>());
  const activeLocationGroups = useRef(new Map<string, string>());
  const pacing = useRef(new DemandPacer());
  const drainRef = useRef<(frame: number) => void>(() => {});
  const liveAdmissionCursor = useRef<string | null>(null);
  const currentFamily = useRef(family);
  const rowElements = useRef(new Map<string, HTMLButtonElement>());
  const pendingArrivals = useRef(new Map<string, Arrival>());
  const requestDrain = () => {
    if (!layoutGate.current.canDrain(snapshot) || deferredDrain.current !== undefined) return;
    const epoch = layoutGate.current.epoch;
    const frame = window.requestAnimationFrame(timestamp => {
      if (deferredDrain.current !== frame) return;
      deferredDrain.current = undefined;
      if (layoutGate.current.epoch === epoch && layoutGate.current.canDrain(snapshot)) drainRef.current(timestamp);
    });
    deferredDrain.current = frame;
  };
  requestDrainRef.current = requestDrain;
  const armExpiry = (newDeadline?: number) => {
    let expiresAt = Number.POSITIVE_INFINITY;
    if (newDeadline !== undefined && expiryTimer.current !== undefined && expiryDeadline.current !== undefined) {
      expiresAt = Math.min(expiryDeadline.current, newDeadline);
    } else {
      for (const event of pendingArrivals.current.values()) expiresAt = Math.min(expiresAt, event.acceptedMono + ARRIVAL_MAX_QUEUE_AGE_MS);
    }
    if (expiryTimer.current !== undefined && expiryDeadline.current === expiresAt) return;
    if (expiryTimer.current !== undefined) clearTimeout(expiryTimer.current);
    expiryTimer.current = undefined; expiryDeadline.current = undefined;
    if (!pendingArrivals.current.size) return;
    expiryDeadline.current = expiresAt;
    const timer = window.setTimeout(() => {
      if (expiryTimer.current !== timer) return;
      expiryTimer.current = undefined; expiryDeadline.current = undefined;
      expireRef.current();
    }, Math.max(0, expiresAt - performance.now()));
    expiryTimer.current = timer;
  };
  const dropPending = (id: string, _event: Arrival, _reason: string, _clock?: number) => {
    if (!pendingArrivals.current.has(id)) return;
    pendingArrivals.current.delete(id);
    // A withdrawn minimum may leave one early wakeup; it never delays expiry.
    if (!pendingArrivals.current.size) armExpiry();
  };
  expireRef.current = () => {
    const clock = performance.now();
    for (const [id, event] of pendingArrivals.current) {
      if (clock - event.acceptedMono >= ARRIVAL_MAX_QUEUE_AGE_MS) dropPending(id, event, 'queue_expired', clock);
    }
    armExpiry();
    requestDrainRef.current();
  };
  const clearLabelSlot = (id: string) => {
    labelSlots.current.delete(id);
    const groupId = activeLocationGroups.current.get(id);
    if (groupId && activeSignalGroups.current.get(groupId) === id) activeSignalGroups.current.delete(groupId);
    activeLocationGroups.current.delete(id);
  };
  const clearDeferredDrain = () => {
    if (deferredDrain.current !== undefined) cancelAnimationFrame(deferredDrain.current);
    deferredDrain.current = undefined;
    pacing.current.pauseFrames();
  };
  const invalidateGeometry = () => {
    // Stop every start path before changing placements or measured bounds.
    layoutGate.current.invalidateGeometry();
    clearDeferredDrain();
    geometryRevisionRef.current++;
    setGeometryRevision(geometryRevisionRef.current);
  };
  const stopArrivalPulse = (id: string) => {
    const timer = arrivalTimers.current.get(id);
    if (timer !== undefined) clearTimeout(timer);
    arrivalTimers.current.delete(id);
    const marker = arrivalMarkers.current.get(id);
    if (marker) { marker.remove(); arrivalMarkers.current.delete(id); }
  };
  const startArrivalPulse = (id: string) => {
    const marker = markers.current.get(id);
    if (!marker) return;
    stopArrivalPulse(id);
    const element = document.createElement('div');
    element.className = 'probe-arrival-layer'; element.dataset.targetId = id;
    const tone = getComputedStyle(marker.getElement()).getPropertyValue('--probe-tone').trim();
    if (tone) element.style.setProperty('--probe-tone', tone);
    const ring = document.createElement('span'); ring.className = 'probe-ripple'; element.appendChild(ring);
    const pulse = new maplibregl.Marker({ element, anchor: 'center' }).setLngLat(marker.getLngLat()).addTo(map);
    arrivalMarkers.current.set(id, pulse);
    const timer = window.setTimeout(() => {
      if (arrivalTimers.current.get(id) !== timer || arrivalMarkers.current.get(id) !== pulse) return;
      arrivalTimers.current.delete(id); arrivalMarkers.current.delete(id); pulse.remove();
    }, ARRIVAL_NODE_MS);
    arrivalTimers.current.set(id, timer);
  };
  const releaseSignal = (id: string, groupId: string, handle?: SignalHandle, redrain = true) => {
    if (handle && signalHandles.current.get(id) !== handle) return;
    if (handle) signalHandles.current.delete(id);
    activeAnimations.current.delete(id);
    activeBounds.current.delete(id);
    stopArrivalPulse(id);
    clearLabelSlot(id);
    if (activeSignalGroups.current.get(groupId) === id) activeSignalGroups.current.delete(groupId);
    if (activeLocationGroups.current.get(id) === groupId) activeLocationGroups.current.delete(id);
    if (!activeAnimations.current.size && !pendingArrivals.current.size) pacing.current.idle();
    if (redrain) requestDrainRef.current();
  };
  const cancelPair = (id: string, _reason = 'eligibility_changed') => {
    const handle = signalHandles.current.get(id);
    // Remove the identity before cancel(): the old finished promise cannot
    // release a replacement lane or clear a newer animation for this target.
    if (handle) { signalHandles.current.delete(id); handle.cancel(); }
    activeAnimations.current.delete(id);
    activeBounds.current.delete(id);
    stopArrivalPulse(id);
    clearLabelSlot(id);
    if (!activeAnimations.current.size && !pendingArrivals.current.size) pacing.current.idle();
  };
  const cancelAllPairs = (reason = 'scheduler_cancelled') => {
    clearDeferredDrain();
    for (const id of new Set([...activeAnimations.current.keys(), ...signalHandles.current.keys(), ...arrivalTimers.current.keys(), ...arrivalMarkers.current.keys(), ...labelSlots.current])) cancelPair(id, reason);
    for (const [id, event] of pendingArrivals.current) dropPending(id, event, reason);
    labelSlots.current.clear(); activeLocationGroups.current.clear(); activeSignalGroups.current.clear();
    pacing.current.idle();
  };

  const layoutRef = useRef(layout); layoutRef.current = layout;
  const selectRef = useRef(onSelect); selectRef.current = onSelect;
  const targetsRef = useRef(targets); targetsRef.current = targets;
  const geometry = JSON.stringify([...targets].sort((a, b) => a.id.localeCompare(b.id)).map(t => [t.id, displayLocation(t)]));
  const measureLabelSize = (id: string): LabelSize | undefined => {
    const label = labelElements.current.get(id), telemetry = telemetryElements.current.get(id);
    if (!label || !telemetry) return undefined;
    return {
      width: label.offsetWidth, height: label.offsetHeight,
      body: { left: telemetry.offsetLeft, top: telemetry.offsetTop, width: telemetry.offsetWidth, height: telemetry.offsetHeight },
    };
  };
  const sameLabelSize = (a: LabelSize | undefined, b: LabelSize) => !!a && near(a.width, b.width) && near(a.height, b.height) && near(a.body.left, b.body.left) && near(a.body.top, b.body.top) && near(a.body.width, b.body.width) && near(a.body.height, b.body.height);
  const setLayoutIfChanged = (next: Layout) => {
    if (sameLayout(layoutRef.current, next)) return;
    invalidateGeometry();
    setLayout(next);
  };
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
      const position = separateLeader(point.x, point.y, { left: point.x + group.offsetX, top: point.y + group.offsetY }, group.width, group.height, group.body, current.width, current.height, { left: group.left, top: group.top });
      const visible = point.x >= 0 && point.x <= current.width && point.y >= 0 && point.y <= current.height;
      if (!movementGate.current.geometryPending && !map.isMoving() && (!near(group.x, point.x) || !near(group.y, point.y)
        || !near(group.left, position.left) || !near(group.top, position.top) || group.visible !== visible)) invalidateGeometry();
      group.x = point.x; group.y = point.y;
      group.left = position.left; group.top = position.top;
      group.visible = visible;
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
  const labelRef = (id: string) => {
    let callback = labelRefCallbacks.current.get(id);
    if (!callback) { callback = element => attachLabel(id, element); labelRefCallbacks.current.set(id, callback); }
    return callback;
  };
  const telemetryRef = (id: string) => {
    let callback = telemetryRefCallbacks.current.get(id);
    if (!callback) { callback = element => attachTelemetry(id, element); telemetryRefCallbacks.current.set(id, callback); }
    return callback;
  };
  const pathRef = (id: string) => {
    let callback = pathRefCallbacks.current.get(id);
    if (!callback) { callback = element => attachPath(id, element); pathRefCallbacks.current.set(id, callback); }
    return callback;
  };
  const rowRef = (id: string) => {
    let callback = rowRefCallbacks.current.get(id);
    if (!callback) {
      callback = element => {
        if (element) rowElements.current.set(id, element);
        else {
          rowElements.current.delete(id);
          cancelPair(id, 'callout_unmounted');
          const pending = pendingArrivals.current.get(id);
          if (pending) dropPending(id, pending, 'callout_unmounted');
        }
      };
      rowRefCallbacks.current.set(id, callback);
    }
    return callback;
  };
  const rebuildSignalGeometry = () => {
    const svg = signalSVG.current;
    if (!svg || movementGate.current.geometryPending || map.isMoving() || geometryRevision !== geometryRevisionRef.current || !layoutGate.current.isCurrent(snapshot)) return;
    const epoch = layoutGate.current.epoch;
    const origin = svg.getBoundingClientRect();
    if (origin.width <= 0 || origin.height <= 0) {
      for (const id of activeAnimations.current.keys()) cancelPair(id, 'layout_changed');
      signalGeometry.current.clear(); geometryBounds.current.clear();
      layoutGate.current.commitGeometry(epoch, false);
      return;
    }
    const relative = (rect: DOMRect) => ({
      left: rect.left - origin.left, top: rect.top - origin.top,
      width: rect.width, height: rect.height,
    });
    const liveTargetIds = new Set(targetsRef.current.map(target => target.id));
    const next = new Map<string, ArrivalGeometry>(), bounds = new Map<string, ArrivalBounds>();
    const visibleGroups = layoutRef.current.groups.filter(group => group.visible);
    for (const group of visibleGroups) {
      const telemetry = telemetryElements.current.get(group.id);
      if (!telemetry?.isConnected) continue;
      const body = relative(telemetry.getBoundingClientRect());
      const node = layoutRef.current.narrow
        ? { x: body.left - 8, y: body.top + Math.min(4, body.height / 2) }
        : { x: group.x, y: group.y };
      for (const id of group.ids) {
        if (!liveTargetIds.has(id)) continue;
        const row = rowElements.current.get(id);
        if (!row?.isConnected) continue;
        const rowRect = row.getBoundingClientRect(), viewport = listContainer.current?.getBoundingClientRect();
        if (layoutRef.current.narrow && viewport && (rowRect.bottom <= viewport.top || rowRect.top >= viewport.bottom || rowRect.right <= viewport.left || rowRect.left >= viewport.right)) continue;
        const inputs = { node, body, row: relative(rowRect), element: row };
        const shape = buildArrivalGeometry(node, body, inputs.row);
        if (shape) { next.set(id, shape); bounds.set(id, inputs); }
      }
    }
    // Repacking can move unreported groups, so inspect every active owner.
    // Cancel a consumed event only; it must never return to the pending queue.
    for (const id of activeAnimations.current.keys()) {
      if (!sameArrivalBounds(activeBounds.current.get(id), bounds.get(id))) cancelPair(id, 'layout_changed');
    }
    signalGeometry.current = next;
    geometryBounds.current = bounds;
    const measured = visibleGroups.length > 0 && visibleGroups.every(group => {
      const size = measureLabelSize(group.id);
      return !!size && size.width > 0 && size.height > 0 && size.body.width > 0 && size.body.height > 0;
    });
    layoutGate.current.commitGeometry(epoch, measured && next.size > 0);
    // The passive admission pass for this commit schedules the only resume.
  };
  const cleanupPartialSignal = (svg: SVGSVGElement, oldChildren: Set<Element>, row: HTMLButtonElement, oldAnimations: Set<Animation>) => {
    for (const child of Array.from(svg.children)) {
      if (oldChildren.has(child)) continue;
      for (const element of [child, ...Array.from(child.querySelectorAll('*'))]) {
        try {
          for (const animation of element.getAnimations()) {
            animation.onfinish = null; animation.oncancel = null; animation.cancel();
          }
        } catch { /* Remove the partial SVG even if a browser animation cannot be inspected. */ }
      }
      child.remove();
    }
    const provider = row.querySelector('.probe-provider');
    for (const element of [row, ...(provider ? [provider] : [])]) {
      try {
        for (const animation of element.getAnimations()) {
          if (oldAnimations.has(animation)) continue;
          animation.onfinish = null; animation.oncancel = null; animation.cancel();
        }
      } catch { /* The signal group and lane cleanup still proceeds. */ }
    }
  };
  const startArrivalSignal = (id: string, groupId: string, shape: ArrivalGeometry, row: HTMLButtonElement) => {
    const svg = signalSVG.current;
    if (!svg || !row.isConnected) return false;
    const oldChildren = new Set(Array.from(svg.children));
    const provider = row.querySelector('.probe-provider');
    const oldAnimations = new Set([...row.getAnimations(), ...(provider ? provider.getAnimations() : [])]);
    let handle: SignalHandle | undefined;
    try {
      startArrivalPulse(id);
      handle = playArrivalSignal(svg, shape, row);
      signalHandles.current.set(id, handle);
      const bounds = geometryBounds.current.get(id);
      if (bounds) activeBounds.current.set(id, bounds);
      void handle.finished.then(
        () => {
          if (signalHandles.current.get(id) !== handle) return;
          releaseSignal(id, groupId, handle);
        },
        () => {
          if (signalHandles.current.get(id) !== handle) return;
          releaseSignal(id, groupId, handle, false);
          handle?.cancel();
          requestDrainRef.current();
        },
      );
      return true;
    } catch {
      if (handle && signalHandles.current.get(id) === handle) {
        signalHandles.current.delete(id);
        handle.cancel();
      }
      cleanupPartialSignal(svg, oldChildren, row, oldAnimations);
      stopArrivalPulse(id);
      return false;
    }
  };
  useEffect(() => { const query = matchMedia('(prefers-reduced-motion: reduce)'); const update = () => setReduced(query.matches); query.addEventListener('change', update); return () => query.removeEventListener('change', update); }, []);
  useLayoutEffect(() => {
    movementGate.current = new MapMovementGate();
    movingRef.current = false;
    const observer = new ResizeObserver(entries => {
      const changedIds = new Set(entries.map(entry => (entry.target as HTMLElement).dataset.groupId).filter((id): id is string => !!id));
      const changed = new Map<string, LabelSize>();
      for (const id of changedIds) {
        const size = measureLabelSize(id);
        if (size && !sameLabelSize(labelSizes.current.get(id), size)) changed.set(id, size);
      }
      if (!changed.size) return;
      invalidateGeometry();
      for (const [id, size] of changed) labelSizes.current.set(id, size);
      if (movementGate.current.geometryPending || map.isMoving()) {
        for (const [id, size] of changed) {
          const group = layoutRef.current.groups.find(item => item.id === id);
          if (group) { group.width = size.width; group.height = size.height; group.body = size.body; }
        }
        syncPositions();
        return;
      }
      setLayoutIfChanged(arrange(map, targetsRef.current, labelSizes.current, previousPositions(), capacityPolicy.current));
    });
    resizeObserver.current = observer;
    for (const [id, element] of labelElements.current) {
      observer.observe(element);
      const telemetry = telemetryElements.current.get(id);
      if (telemetry) observer.observe(telemetry);
      const size = measureLabelSize(id);
      if (size) labelSizes.current.set(id, size);
    }
    const arrangeAtRest = () => setLayoutIfChanged(arrange(map, targetsRef.current, labelSizes.current, previousPositions(), capacityPolicy.current));
    const beginMovement = (kind: 'viewport' | 'gesture' | undefined) => {
      if (!kind) return;
      invalidateGeometry();
      movingRef.current = movementGate.current.gestureActive;
      if (kind === 'gesture') cancelAllPairs('map_moving');
    };
    const start = (event: maplibregl.MapMovementEvent) =>
      beginMovement(movementGate.current.start(event, map.isMoving()));
    const progress = (event: maplibregl.MapMovementEvent) =>
      beginMovement(movementGate.current.progress(event, map.isMoving()));
    const end = (event: maplibregl.MapMovementEvent) => {
      const atRest = movementGate.current.end(event, map.isMoving());
      movingRef.current = movementGate.current.gestureActive;
      if (!atRest) return;
      invalidateGeometry();
      arrangeAtRest();
    };
    const resize = () => {
      // Even unchanged label sizes can move every row in the narrow grid.
      invalidateGeometry();
      if (!movementGate.current.geometryPending && !map.isMoving()) { arrangeAtRest(); return; }
      const current = layoutRef.current, container = map.getContainer();
      setLayoutIfChanged({ ...current, width: container.clientWidth, height: container.clientHeight, narrow: current.narrow });
      syncPositions();
    };
    map.on('render', syncPositions); map.on('resize', resize); map.on('movestart', start); map.on('move', progress); map.on('moveend', end);
    arrangeAtRest();
    return () => {
      layoutGate.current.invalidateGeometry();
      cancelAllPairs('map_effect_cleanup');
      observer.disconnect(); resizeObserver.current = null;
      map.off('render', syncPositions); map.off('resize', resize); map.off('movestart', start); map.off('move', progress); map.off('moveend', end);
    };
  }, [map]);
  useLayoutEffect(() => {
    setLayoutIfChanged(arrange(map, targetsRef.current, labelSizes.current, previousPositions(), capacityPolicy.current));
  }, [map, geometry]);
  useLayoutEffect(() => {
    if (layout.narrow) for (const element of labelElements.current.values()) {
      element.style.transform = ''; element.style.visibility = 'visible'; element.setAttribute('aria-hidden', 'false');
    }
    syncPositions();
  }, [layout.groups, layout.narrow]);
  useLayoutEffect(() => { rebuildSignalGeometry(); }, [snapshot]);
  useEffect(() => {
    const wanted = new Set(targetsRef.current.filter(t => displayLocation(t)).map(t => t.id));
    for (const [id, marker] of markers.current) if (!wanted.has(id)) {
      cancelPair(id, 'marker_removed');
      const pending = pendingArrivals.current.get(id);
      if (pending) dropPending(id, pending, 'marker_removed');
      marker.remove(); markers.current.delete(id);
    }
    for (const t of targetsRef.current) {
      const location = displayLocation(t);
      if (!location) continue;
      if (!markers.current.has(t.id)) {
        const button = document.createElement('button'); button.type = 'button'; button.classList.add('probe-geodot');
        button.innerHTML = '<span class="probe-aura"></span><span class="probe-core"></span>';
        button.addEventListener('click', () => selectRef.current(t.id));
        markers.current.set(t.id, new maplibregl.Marker({ element: button }).setLngLat(location.coordinates).addTo(map));
      } else markers.current.get(t.id)!.setLngLat(location.coordinates);
    }
  }, [map, geometry]);
  useEffect(() => () => {
    cancelAllPairs('component_unmount');
    for (const marker of markers.current.values()) marker.remove();
    markers.current.clear();
  }, []);
  const targetsById = new Map(targets.map(target => [target.id, target]));
  const renderGroups = retainCurrentTargetGroups(layout.groups, new Set(targetsById.keys()));
  const visible = new Set(renderGroups.filter(g => g.visible).flatMap(g => g.ids));
  const enqueueArrival = (id: string, event: Arrival): 'queued' | 'replaced' | 'ignored' => {
    if (!registeredTargetIds.has(id)) {
      return 'ignored';
    }
    const previous = pendingArrivals.current.get(id);
    if (previous && previous.completedAt >= event.completedAt) {
      return 'ignored';
    }
    if (previous) {
      // A newer source timestamp replaces the payload while keeping its FIFO
      // position and original expiry; repeated updates cannot pin the queue.
      const replacement = { ...event, acceptedAt: previous.acceptedAt, acceptedMono: previous.acceptedMono };
      pendingArrivals.current.set(id, replacement);
      armExpiry(replacement.acceptedMono + ARRIVAL_MAX_QUEUE_AGE_MS);
      return 'replaced';
    }
    pendingArrivals.current.set(id, event);
    armExpiry(event.acceptedMono + ARRIVAL_MAX_QUEUE_AGE_MS);
    return 'queued';
  };
  useLayoutEffect(() => {
    for (const [id, event] of pendingArrivals.current) if (!registeredTargetIds.has(id)) dropPending(id, event, 'target_unregistered');
    for (const id of activeAnimations.current.keys()) if (!registeredTargetIds.has(id)) cancelPair(id, 'target_unregistered');
    for (const [group, id] of locationServiceCursor.current) if (!registeredTargetIds.has(id)) locationServiceCursor.current.delete(group);
  }, [registeredTargetIds]);
  drainRef.current = frame => {
    if (!motionEnabled || reduced || movingRef.current || document.hidden || currentFamily.current !== family) {
      const reason = !motionEnabled ? 'motion_disabled' : reduced ? 'reduced_motion' : movingRef.current ? 'map_moving' : document.hidden ? 'document_hidden' : 'family_changed';
      cancelAllPairs(reason);
      return;
    }
    // Admission, geometry and expiry have independent owners. A blocked geometry
    // epoch cannot sample hidden/paused time or release a stale callback.
    if (!layoutGate.current.canDrain(snapshot)) { pacing.current.pauseFrames(); return; }
    const clock = Date.now(), mono = performance.now();
    pacing.current.observeFrame(frame);
    const eligibilityFailure = (id: string, event: Arrival) => {
      const target = targetsById.get(id), observation = target && reading(target, family);
      if (!registeredTargetIds.has(id)) return 'target_unregistered';
      if (!target) return 'target_missing';
      if (!visible.has(id) || layout.narrow && !geometryBounds.current.has(id)) return 'callout_not_visible';
      if (event.family !== family) return 'family_changed';
      if (target.revision !== event.revision) return 'revision_changed';
      if (observation?.method !== event.method) return 'method_changed';
      if (freshCompletedAt(observation.observed_at, clock) !== event.completedAt) return 'source_changed_or_stale';
      if (probeActivity(target, observation, clock) !== 'unknown') return 'activity_changed';
      if (!markers.current.has(id)) return 'marker_missing';
      if (!rowElements.current.get(id)?.isConnected) return 'callout_unavailable';
      return undefined;
    };
    for (const [id, event] of activeAnimations.current) {
      const reason = eligibilityFailure(id, event);
      if (reason) cancelPair(id, reason);
    }
    for (const [id, event] of pendingArrivals.current) {
      if (mono - event.acceptedMono >= ARRIVAL_MAX_QUEUE_AGE_MS) dropPending(id, event, 'queue_expired', mono);
      else {
        const reason = eligibilityFailure(id, event);
        if (reason) dropPending(id, event, reason, mono);
      }
    }
    const queued = [...pendingArrivals.current].filter(([id]) => !activeAnimations.current.has(id))
      .sort(([a, x], [b, y]) => x.acceptedMono - y.acceptedMono || a.localeCompare(b));
    const locations = new Map<string, Array<{ id: string; event: Arrival }>>();
    const runnable: Array<{ id: string; event: Arrival; groupId: string; shape: ArrivalGeometry; row: HTMLButtonElement }> = [];
    for (const [id, event] of queued) {
      const target = targetsById.get(id), location = target && displayLocation(target);
      if (!target || !location) continue;
      const items = locations.get(location.id) ?? [];
      items.push({ id, event }); locations.set(location.id, items);
    }
    for (const [groupId, items] of locations) {
      // FIFO across distinct monotonic ages; rotate equal-age siblings after
      // the last real launch so repeated large cohorts cannot starve a tail.
      const oldest = items.filter(item => item.event.acceptedMono === items[0].event.acceptedMono);
      const cursor = locationServiceCursor.current.get(groupId);
      const { id, event } = (cursor && oldest.find(item => item.id.localeCompare(cursor) > 0)) || oldest[0];
      if (activeSignalGroups.current.has(groupId)) continue;
      const shape = signalGeometry.current.get(id), row = rowElements.current.get(id);
      if (!shape || !row?.isConnected) continue;
      runnable.push({ id, event, groupId, shape, row });
    }
    runnable.sort((a, b) => a.event.acceptedMono - b.event.acceptedMono || a.id.localeCompare(b.id));
    const expiry = runnable.length ? runnable.reduce((minimum, item) => Math.min(minimum, item.event.acceptedMono + ARRIVAL_MAX_QUEUE_AGE_MS), Number.POSITIVE_INFINITY) : mono;
    const plan = pacing.current.plan(mono, frame, runnable.length, expiry);
    if (plan.ready) {
      const { id, event, groupId, shape, row } = runnable[0]; // Oldest runnable location; busy groups cannot block it.
      // Queue preparation can cross a deadline before the independent timer can
      // run. Recheck at the launch boundary before consuming the original event.
      const launchMono = performance.now();
      if (launchMono - event.acceptedMono >= ARRIVAL_MAX_QUEUE_AGE_MS) {
        dropPending(id, event, 'queue_expired', launchMono);
      } else {
        pendingArrivals.current.delete(id);
        activeAnimations.current.set(id, event);
        labelSlots.current.add(id);
        activeLocationGroups.current.set(id, groupId); activeSignalGroups.current.set(groupId, id);
        const constructionStart = performance.now();
        const started = startArrivalSignal(id, groupId, shape, row);
        if (started) locationServiceCursor.current.set(groupId, id);
        const construction = Math.max(0, performance.now() - constructionStart);
        pacing.current.launched(frame, constructionStart, construction);
        // Re-arm after construction so an O(registry) expiry scan cannot delay
        // the checked event before its signal begins.
        armExpiry();
        if (!started) {
          releaseSignal(id, groupId, undefined, false);
        }
      }
    }
    const queueDepth = pendingArrivals.current.size, activeCount = activeAnimations.current.size;
    if (queueDepth || activeCount) requestDrainRef.current();
    else pacing.current.idle();
  };
  useEffect(() => {
    for (const t of targets) {
      const marker = markers.current.get(t.id); if (!marker) continue;
      const el = marker.getElement(), o = reading(t, family), status = statusOf(o, now);
      el.style.setProperty('--probe-tone', distanceColor(t, o, origin, now, theme === 'light', accessAllowanceMS));
      const activity = probeActivity(t, o, now);
      updateMarkerAppearance(el, selected === t.id, status, `${t.region} · ${t.provider} · ${statusLabel(status)} · ${activity} activity · approximate location`);
    }
  }, [targets, selected, family, now, theme, origin, accessAllowanceMS]);
  useEffect(() => {
    if (geometryRevision !== geometryRevisionRef.current || !layoutGate.current.isCurrent(snapshot)) return;
    const epoch = layoutGate.current.epoch;
    const familyChanged = currentFamily.current !== family;
    currentFamily.current = family;
    if (familyChanged) cancelAllPairs('family_changed');
    // Prune only withdrawn registry identities, not temporary UI filters.
    for (const key of seenArrivals.current.keys()) if (!registeredTargetIds.has(key.split('|')[0])) seenArrivals.current.delete(key);

    // Saved local readings establish a baseline; only newly completed local batches animate.
    const eventClock = Date.now();
    const admissionMono = performance.now();
    const liveCandidates = new Map<string, { id: string; event: Arrival }>();
    for (const [key, acceptedAt] of arrivals) {
      const [targetId, eventFamily, eventMethod, eventRevision] = key.split('|');
      if (!registeredTargetIds.has(targetId)) continue;
      if (seenArrivals.current.get(key) === acceptedAt) continue;
      seenArrivals.current.set(key, acceptedAt);
      const age = eventClock - acceptedAt;
      if (familyChanged || eventFamily !== family || age < 0 || age >= ARRIVAL_WINDOW_MS) {
        continue;
      }
      const target = targetsRef.current.find(item => item.id === targetId);
      if (!target || !visible.has(targetId) || layout.narrow && !geometryBounds.current.has(targetId) || !target.geography.coordinates) {
        continue;
      }
      const observation = reading(target, family);
      if (!observation || target.revision !== Number(eventRevision) || observation.method !== eventMethod || freshCompletedAt(observation.observed_at, eventClock) === undefined || probeActivity(target, observation, eventClock) !== 'unknown' || !markers.current.has(targetId)) {
        continue;
      }
      const completedAt = Date.parse(observation.observed_at!);
      const event: Arrival = { family, method: eventMethod, completedAt, acceptedAt, acceptedMono: admissionMono - Math.max(0, eventClock - acceptedAt), revision: target.revision };
      const previous = liveCandidates.get(target.id);
      if (!previous || event.completedAt > previous.event.completedAt || event.completedAt === previous.event.completedAt && event.acceptedAt > previous.event.acceptedAt) {
        liveCandidates.set(target.id, { id: target.id, event });
      }
    }

    // Refresh already-queued identities first. enqueueArrival preserves each
    // item's original FIFO age and expiry when replacing its source.
    const replacements = [...liveCandidates.values()].filter(candidate => pendingArrivals.current.has(candidate.id))
      .sort((a, b) => {
        const left = pendingArrivals.current.get(a.id)!, right = pendingArrivals.current.get(b.id)!;
        return left.acceptedAt - right.acceptedAt || a.id.localeCompare(b.id);
      });
    for (const candidate of replacements) enqueueArrival(candidate.id, candidate.event);

    // Admit new identities in chronological acceptedAt cohorts, rotating the
    // sorted target order after the last successful admission. Dropped overflow
    // remains seen; only a later genuine fresh event can enter on a future turn.
    const cohorts = new Map<number, Array<{ id: string; event: Arrival }>>();
    for (const candidate of liveCandidates.values()) {
      if (pendingArrivals.current.has(candidate.id)) continue;
      const cohort = cohorts.get(candidate.event.acceptedAt) ?? [];
      cohort.push(candidate); cohorts.set(candidate.event.acceptedAt, cohort);
    }
    const chronologicalCohorts = [...cohorts].sort(([left], [right]) => left - right);
    for (const [, cohort] of chronologicalCohorts) {
      const sorted = cohort.sort((a, b) => a.id.localeCompare(b.id));
      const cursor = liveAdmissionCursor.current;
      const split = cursor === null ? 0 : sorted.findIndex(candidate => candidate.id.localeCompare(cursor) > 0);
      const startAt = split < 0 ? 0 : split;
      const rotated = [...sorted.slice(startAt), ...sorted.slice(0, startAt)];
      for (const candidate of rotated) {
        const accepted = enqueueArrival(candidate.id, candidate.event);
        if (accepted !== 'queued') continue;
        liveAdmissionCursor.current = candidate.id;
      }
    }
    const animate = motionEnabled && !reduced && !movingRef.current && !document.hidden;
    if (!animate || familyChanged) {
      const reason = familyChanged ? 'family_changed' : !motionEnabled ? 'motion_disabled' : reduced ? 'reduced_motion' : movingRef.current ? 'map_moving' : 'document_hidden';
      cancelAllPairs(reason);
    }
    layoutGate.current.admit(epoch, snapshot);
    requestDrainRef.current();
  }, [snapshot]);
  useEffect(() => {
    const clearMotion = () => {
      if (!document.hidden) return;
      for (const [key, acceptedAt] of arrivals) seenArrivals.current.set(key, acceptedAt);
      cancelAllPairs('document_hidden');
    };
    document.addEventListener('visibilitychange', clearMotion);
    return () => document.removeEventListener('visibilitychange', clearMotion);
  }, [arrivals]);
  useLayoutEffect(() => {
    if (motionEnabled && !reduced) return;
    cancelAllPairs(reduced ? 'reduced_motion' : 'motion_disabled');
  }, [motionEnabled, reduced]);
  return <div ref={listContainer} onScroll={() => { if (signalSVG.current) signalSVG.current.style.top = `${listContainer.current?.scrollTop ?? 0}px`; invalidateGeometry(); }} className={`probe-atlas ${layout.narrow ? 'narrow' : ''} ${layout.docked ? 'docked' : ''}`} data-layout={layout.docked ? 'docked' : layout.narrow ? 'list' : 'floating'} tabIndex={layout.narrow ? 0 : undefined} aria-label="Approximate endpoint regions and current measurements on the map">
    {!layout.narrow && <svg className="probe-leaders" width={layout.width} height={layout.height} aria-hidden="true">{renderGroups.map(g => <path key={g.id} ref={pathRef(g.id)} d={leader(g)} />)}</svg>}
    {renderGroups.map(g => {
      return <div className="probe-callout" key={g.id} ref={labelRef(g.id)}>
        <div className="probe-location" title={g.label}>{g.label}</div>
        <div className="probe-telemetry" ref={telemetryRef(g.id)}>
          {g.ids.map(id => { const t = targetsById.get(id)!, o = reading(t, family), activity = probeActivity(t, o, now), state = mapLabelState(t, o, now), { loss } = state, status = statusOf(o, now), color = distanceColor(t, o, origin, now, theme === 'light', accessAllowanceMS);
            return <button key={id} ref={rowRef(id)} type="button" data-target-id={id} className={`probe-row ${id === selected ? 'selected' : ''} ${status} ${activity}`} style={{ '--probe-tone': color } as CSSProperties}
              onMouseEnter={event => showHelp(id, event.currentTarget, true)} onMouseLeave={leaveHelp}
              onMouseMove={event => {
                if (!helpDismissed.current && helpTarget?.id !== id) showHelp(id, event.currentTarget, true);
              }}
              onFocus={event => showHelp(id, event.currentTarget)} onBlur={leaveHelp}
              onClick={event => { onSelect(id); showHelp(id, event.currentTarget); }}
              aria-pressed={id === selected} aria-label={state.description} aria-describedby={helpTarget?.id === id ? helpId : undefined}>
              {state.cue && <span className={`probe-status-pattern ${state.cue}`} aria-hidden="true" />}
              <span className="probe-reading"><span className="probe-provider">{compactProvider(t.provider)}</span><span className="probe-method">{o ? shortMethod[o.method] : '\u2014'}</span><strong>
                <span className="probe-measurement"><span className="probe-rtt"><span className="probe-rtt-value">{compact(o?.median_ms)}</span></span>{loss.visible && <span className={`probe-loss-bar ${loss.freshness}`} style={{ '--probe-loss-scale': loss.scale, '--probe-loss-color': loss.color } as CSSProperties} aria-hidden="true"><span className="probe-loss-fill" /></span>}</span>
              </strong></span>
            </button>;
          })}
        </div>
      </div>;
    })}
    {helpTarget && targetsById.has(helpTarget.id) && <ReadingTooltip id={helpId} anchor={helpTarget.anchor}
      lines={mapLabelState(targetsById.get(helpTarget.id)!, reading(targetsById.get(helpTarget.id)!, family), now).reasons}
      onDismiss={dismissHelp} onEnter={keepHelp} onLeave={leaveHelp} />}
    <svg ref={signalSVG} className="probe-signals" aria-hidden="true" focusable="false" />
  </div>;
}
